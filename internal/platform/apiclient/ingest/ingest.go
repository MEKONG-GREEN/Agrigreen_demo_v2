// Package ingest là nạp theo lô của cổng tích hợp (W5.4; R-INT-03, R-INT-04, ADR-0033 §3, kiến trúc v4.1 §41.3):
//
//	POST /v1/batches/{contract} → kiểm phong bì đồng bộ (rẻ) → sổ ingest_batch → 202 + mã lô
//	→ hàng đợi RIÊNG của đối tác (+ DLQ riêng) → inbox (một lần về hiệu quả) → kiểm hợp đồng (mặc định từ chối)
//	→ port của module chủ (RecordSink) → kết quả từng bản ghi (ingest_record_result) → kết cục lô.
//
// Bản ghi lỗi không làm hỏng cả lô (rejected + mã problem). Lỗi tạm của port chủ (không phải problem 4xx) làm
// thông điệp giao lại; quá DeliveryLimit lần → DLQ của đối tác, lô ghi kết cục dead_lettered.
//
// Mọi bảng thuộc tenant ĐỐI TÁC (client_tenant_id của khách hàng API — lấy từ bản ghi khóa), chạy dưới vai
// r_apiclient (db.ForModule ở gốc module). Hợp đồng: contracts/integration (ingest/*.json, phong bì
// envelope/ingest_batch.v1.json). Hợp đồng proposed/draft chỉ phục vụ khi bật cờ dev/test (Gate.AllowProposed)
// VÀ không phải production — production từ chối (đóng khi lỗi).
//
// Port của module chủ là interface do BÊN TIÊU THỤ khai (RecordSink, ở đây) và được nối ở gốc lắp ráp của cổng
// (cmd/integration-gateway/assembly). Ba hợp đồng song phương (ADR-0034, C-12) chưa có port chủ: Staging ghi vào
// sổ đệm pf_apiclient.ingest_staging; W5.6 nối port thật (pf_coop/pf_cropcycle). Van RabbitMQ của MKG
// (valve_collection) KHÔNG đổi ở gói này (W5.10).
package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RuleVersion ghim vào rule_version của các sổ nạp.
const RuleVersion = "integration.ingest.v1"

// MessageType là event_type của phong bì thông điệp trên hàng đợi đối tác (không phát qua outbox — không phải
// sự kiện công bố).
const MessageType = "pf.apiclient.ingest.batch_received"

// ConsumerName là tên consumer của inbox (shared.inbox) cho bộ xử lý lô — một lô xử lý đúng một lần về hiệu quả
// dù được phát lại (gọi lặp POST, hàng đợi giao lặp).
const ConsumerName = "pf.apiclient.ingest"

// QueuePrefix: hàng đợi của đối tác là QueuePrefix + client_id, DLQ thêm ".dlq".
const QueuePrefix = "pf.apiclient.ingest."

// DefaultMaxQueued: số lô chờ tối đa mỗi đối tác trước khi trả 429 (R-INT-04 vách ngăn).
const DefaultMaxQueued = 50

// DefaultRetryAfter: Retry-After khi hàng đợi đối tác đầy.
const DefaultRetryAfter = 30 * time.Second

// Mã lỗi (docs/errors.md mục "Cổng tích hợp — nạp theo lô").
var (
	ErrContractUnknown      = problem.New(http.StatusNotFound, "integration.contract_unknown", "Không có hợp đồng nạp với tên và phiên bản này")
	ErrContractNotServable  = problem.New(http.StatusForbidden, "integration.contract_not_servable", "Hợp đồng nạp không phục vụ ở môi trường này")
	ErrBatchInvalid         = problem.New(http.StatusBadRequest, "integration.batch_invalid", "Lô không đúng phong bì ingest_batch.v1")
	ErrBatchTooLarge        = problem.New(http.StatusRequestEntityTooLarge, "integration.batch_too_large", "Lô vượt max_batch của hợp đồng")
	ErrQueueSaturated       = problem.New(http.StatusTooManyRequests, "integration.queue_saturated", "Hàng đợi nạp của đối tác đã đầy — gửi lại sau Retry-After")
	ErrQueueUnavailable     = problem.New(http.StatusServiceUnavailable, "integration.queue_unavailable", "Hàng đợi nạp chưa sẵn sàng — gửi lại cùng Idempotency-Key")
	ErrBatchNotFound        = problem.New(http.StatusNotFound, "integration.batch_not_found", "Không tìm thấy lô nạp")
	ErrRecordInvalid        = problem.New(http.StatusUnprocessableEntity, "integration.record_invalid", "Bản ghi không đúng hợp đồng nạp")
	ErrEntityKeyMismatch    = problem.New(http.StatusUnprocessableEntity, "integration.entity_key_mismatch", "entity_key của phần tử không khớp các trường entity_key của bản ghi")
	ErrBusinessKeyConflict  = problem.New(http.StatusConflict, "integration.business_key_conflict", "Khóa nghiệp vụ đã nhận với nội dung khác")
	ErrNoOwnerPort          = problem.New(http.StatusForbidden, "integration.no_owner_port", "Hợp đồng chưa nối port của module chủ — mặc định từ chối")
	ErrUnavailableOwnerPort = problem.New(http.StatusServiceUnavailable, "integration.owner_unavailable", "Port của module chủ tạm lỗi")
)

// Known: có hợp đồng nạp hợp lệ (bất kỳ phiên bản, bất kỳ trạng thái) tên name — phân biệt 404 (không có) với
// 403 (có nhưng thiếu scope/không phục vụ).
func Known(name string) bool {
	all, err := integration.All()
	if err != nil {
		return false
	}
	for _, c := range all {
		if c.Name == name && c.Kind == integration.KindIngest {
			return true
		}
	}
	return false
}

// Gate là kiểm hợp đồng nạp, mặc định từ chối.
type Gate struct {
	// Production: residency.Production — proposed/draft không bao giờ phục vụ.
	Production bool
	// AllowProposed: cờ dev/test (AG_GATEWAY_INGEST_ALLOW_PROPOSED=1) cho hợp đồng proposed/draft ngoài production.
	AllowProposed bool
	// Owned: hợp đồng có port module chủ đã nối (nil → không hợp đồng nào).
	Owned func(contract string) bool
}

// Resolve trả hợp đồng nạp phục vụ được, hoặc lỗi: không có / không phải nạp → ErrContractUnknown; không phục vụ
// (retired, proposed/draft không cờ hoặc ở production, approved còn pending, chưa nối port chủ) →
// ErrContractNotServable.
func (g Gate) Resolve(name string, version int) (*integration.Contract, error) {
	c, err := integration.Get(name, version)
	if err != nil || c.Kind != integration.KindIngest || c.Ingest == nil {
		return nil, ErrContractUnknown
	}
	if !g.servable(c) {
		return nil, ErrContractNotServable
	}
	if g.Owned == nil || !g.Owned(c.Name) {
		return nil, ErrNoOwnerPort
	}
	return c, nil
}

func (g Gate) servable(c *integration.Contract) bool {
	switch c.Status {
	case integration.StatusApproved:
		return c.Servable(g.Production)
	case integration.StatusProposed, integration.StatusDraft:
		return g.AllowProposed && !g.Production && c.Servable(false)
	}
	return false
}

// Item là một phần tử của phong bì.
type Item struct {
	EntityKey string          `json:"entity_key"`
	Record    json.RawMessage `json:"record"`
}

// Envelope là phong bì ingest_batch.v1 đã đọc.
type Envelope struct {
	Contract        string     `json:"contract"`
	ContractVersion int        `json:"contract_version"`
	BatchRef        uuid.UUID  `json:"batch_ref"`
	SentAt          time.Time  `json:"sent_at"`
	AsOf            *time.Time `json:"as_of"`
	Items           []Item     `json:"items"`
}

// ParseEnvelope kiểm thân POST theo phong bì ingest_batch.v1 (mặc định từ chối khóa lạ), khớp hợp đồng của path
// và Idempotency-Key = batch_ref. Không kiểm từng bản ghi (việc của bộ xử lý inbox).
func ParseEnvelope(raw []byte, pathContract, idemKey string) (Envelope, error) {
	env, err := integration.EnvelopeFor(integration.KindIngest)
	if err != nil {
		return Envelope{}, err
	}
	if err := env.Validate(raw); err != nil {
		return Envelope{}, ErrBatchInvalid.WithDetail(trim(strings.TrimPrefix(err.Error(), "publication: van từ chối payload: ")))
	}
	var e Envelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&e); err != nil {
		return Envelope{}, ErrBatchInvalid.WithDetail("phong bì không đọc được")
	}
	switch {
	case e.Contract != pathContract:
		return Envelope{}, ErrBatchInvalid.WithField("contract", "phải trùng hợp đồng của đường dẫn")
	case e.BatchRef == uuid.Nil || !strings.EqualFold(e.BatchRef.String(), strings.TrimSpace(idemKey)):
		return Envelope{}, ErrBatchInvalid.WithField("batch_ref", "phải bằng header Idempotency-Key")
	case len(e.Items) == 0:
		return Envelope{}, ErrBatchInvalid.WithField("items", "ít nhất một phần tử")
	}
	return e, nil
}

// CheckSize: số phần tử không vượt max_batch của hợp đồng.
func CheckSize(c *integration.Contract, n int) error {
	if c.Ingest == nil || c.Ingest.MaxBatch < 1 || n > c.Ingest.MaxBatch {
		limit := 0
		if c.Ingest != nil {
			limit = c.Ingest.MaxBatch
		}
		return ErrBatchTooLarge.WithDetail(fmt.Sprintf("%d phần tử, tối đa %d", n, limit))
	}
	return nil
}

// Saturation quyết định 429 theo độ sâu hàng đợi của đối tác: depth ≥ limit → bão hòa. limit ≤ 0 →
// DefaultMaxQueued.
func Saturation(depth, limit int) bool {
	if limit <= 0 {
		limit = DefaultMaxQueued
	}
	return depth >= limit
}

// BusinessKey dựng khóa nghiệp vụ (ingest.business_key) của bản ghi; rỗng business_key → entity_key.
func BusinessKey(c *integration.Contract, record json.RawMessage) (string, error) {
	fields := c.EntityKey
	if c.Ingest != nil && len(c.Ingest.BusinessKey) > 0 {
		fields = c.Ingest.BusinessKey
	}
	dec := json.NewDecoder(bytes.NewReader(record))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		switch v := m[f].(type) {
		case string:
			parts = append(parts, v)
		case json.Number:
			parts = append(parts, v.String())
		default:
			return "", fmt.Errorf("business_key %s thiếu hoặc không phải string/integer", f)
		}
	}
	return strings.Join(parts, integration.EntityKeySep), nil
}

// Hash là SHA-256 hex.
func Hash(b ...[]byte) string {
	h := sha256.New()
	for _, x := range b {
		h.Write(x)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func trim(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:300])
	}
	return s
}

// Record là một bản ghi đã qua hợp đồng, gửi tới port của module chủ.
type Record struct {
	Contract        string
	ContractVersion int
	PartnerTenant   uuid.UUID // tenant đối tác (bên gửi)
	ClientID        uuid.UUID
	BatchID         uuid.UUID
	Index           int
	EntityKey       string
	BusinessKey     string
	Payload         json.RawMessage
	ReceivedAt      time.Time
}

// RecordSink là port của MODULE CHỦ một hợp đồng nạp, do bộ xử lý lô (bên tiêu thụ) khai và gốc lắp ráp nối.
// Ingest phải idempotent theo (Contract, ContractVersion, BusinessKey): lô có thể được xử lý lại sau lỗi tạm.
// Trả nil → accepted; *problem.Problem 4xx → rejected với mã đó; lỗi khác (kể cả problem 5xx) → lỗi tạm, thông
// điệp giao lại (quá giới hạn → DLQ).
type RecordSink interface {
	Ingest(ctx context.Context, r Record) error
}

// SinkFunc chuyển hàm thành RecordSink.
type SinkFunc func(ctx context.Context, r Record) error

// Ingest gọi f.
func (f SinkFunc) Ingest(ctx context.Context, r Record) error { return f(ctx, r) }

// rejection trả problem 4xx nếu err là từ chối bản ghi (không phải lỗi tạm).
func rejection(err error) (*problem.Problem, bool) {
	var p *problem.Problem
	if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
		return p, true
	}
	return nil, false
}
