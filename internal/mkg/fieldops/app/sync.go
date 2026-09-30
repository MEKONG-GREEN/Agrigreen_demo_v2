package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Đồng bộ ngoại tuyến tối giản của app hiện trường MKG — CÙNG HÌNH DẠNG với hợp
// đồng docs/dev/sync-contract.md nhưng hiện thực trong vùng mkg (không dùng
// internal/platform/sync, R-03). Chiều lên: entity roll_count (create/correct).
// Chiều xuống: entity collection_order.

// Giới hạn của một lô đẩy.
const (
	MaxPushRecords  = 500
	maxPayloadBytes = 256 << 10
	EntityRollCount = "roll_count"
	EntityOrder     = "collection_order"
)

// SyncRecord là một bản ghi máy gửi lên.
type SyncRecord struct {
	ID               string          `json:"id"`
	IdemKey          string          `json:"idem_key"`
	Entity           string          `json:"entity"`
	Op               string          `json:"op"`
	EventTime        string          `json:"event_time"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason *string         `json:"correction_reason"`
	Payload          json.RawMessage `json:"payload"`
}

// SyncBatch là thân POST /v1/fieldops/sync/push.
type SyncBatch struct {
	DeviceID string       `json:"device_id"`
	Records  []SyncRecord `json:"records"`
}

// RollCountPayload là payload entity roll_count.
type RollCountPayload struct {
	OrderID            string  `json:"order_id"`
	PlotRef            string  `json:"plot_ref"`
	Stage              string  `json:"stage"`
	Rolls              *int    `json:"rolls"`
	LooseKg            *string `json:"loose_kg"`
	DiscrepancyReason  string  `json:"discrepancy_reason"`
	HTXConfirmationRef *string `json:"htx_confirmation_ref"`
	// GPS (G3-2, tùy chọn, không bao giờ chặn lưu): {lat, lon|lng, accuracy_m} và trạng thái
	// máy báo (ok | weak | denied | unavailable | timeout | no_device).
	GPS       json.RawMessage `json:"gps"`
	GPSStatus string          `json:"gps_status"`
	// VesselCode (G4, tùy chọn): số hiệu ghe/sà lan khi xếp ghe (boat_load) — proto FM-05. App hiện trường
	// MKG (apps/mkg-field-app) gửi khi người thu gom nhập ô số hiệu ghe ở công đoạn xếp ghe (G4).
	VesselCode *string `json:"vessel_code"`
}

// FieldIssue là lỗi theo trường của một bản ghi.
type FieldIssue struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// SyncResult là kết quả một bản ghi.
type SyncResult struct {
	ID     string       `json:"id"`
	Status string       `json:"status"` // accepted | duplicate | rejected
	Code   string       `json:"code,omitempty"`
	Errors []FieldIssue `json:"errors,omitempty"`
}

func rejected(id, code string, issues ...FieldIssue) SyncResult {
	return SyncResult{ID: id, Status: "rejected", Code: code, Errors: issues}
}

// Push nhận một lô bản ghi từ app hiện trường MKG. Mỗi bản ghi xử lý trong giao
// dịch riêng: một bản ghi lỗi không làm hỏng cả lô. Idempotent theo idem_key
// (gửi lại → duplicate) và id (id đã dùng cho bản ghi khác → rejected).
func (s *Service) Push(ctx context.Context, headerDevice string, b SyncBatch) ([]SyncResult, error) {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return nil, err
	}
	if headerDevice == "" || b.DeviceID != headerDevice {
		return nil, ErrDeviceMismatch
	}
	if len(b.Records) > MaxPushRecords {
		return nil, problem.Validation.WithField("records", "tối đa 500 bản ghi mỗi lô")
	}
	out := make([]SyncResult, 0, len(b.Records))
	for _, r := range b.Records {
		out = append(out, s.pushOne(ctx, p, b.DeviceID, r))
	}
	return out, nil
}

func (s *Service) pushOne(ctx context.Context, p authz.Principal, device string, r SyncRecord) SyncResult {
	bad := func(f, m string) SyncResult { return rejected(r.ID, "validation_failed", FieldIssue{f, m}) }
	id, err := uuid.Parse(r.ID)
	if err != nil || id.Version() != 7 {
		return bad("id", "UUIDv7")
	}
	if !strings.HasPrefix(r.IdemKey, "dev:"+device+":") || len(r.IdemKey) > 200 {
		return bad("idem_key", "dạng dev:<device_id>:<seq> của chính thiết bị gửi")
	}
	if r.Entity != EntityRollCount {
		return rejected(r.ID, "fieldops.unknown_entity")
	}
	evt, err := time.Parse(time.RFC3339Nano, r.EventTime)
	if err != nil {
		return bad("event_time", "RFC 3339 có múi giờ")
	}
	if len(r.Payload) == 0 || len(r.Payload) > maxPayloadBytes || r.Payload[0] != '{' {
		return bad("payload", "đối tượng JSON ≤ 256 KiB")
	}
	var pl RollCountPayload
	if err := json.Unmarshal(r.Payload, &pl); err != nil {
		return bad("payload", "không đọc được")
	}
	in := CountInput{ID: &id, EventTime: evt, DeviceID: &device, IdemKey: r.IdemKey}
	switch r.Op {
	case "create":
		if r.SupersedesID != nil || r.CorrectionReason != nil {
			return bad("op", "create không có supersedes_id/correction_reason")
		}
		if in.OrderID, err = uuid.Parse(pl.OrderID); err != nil {
			return bad("payload.order_id", "UUID")
		}
		if in.PlotRef, err = uuid.Parse(pl.PlotRef); err != nil {
			return bad("payload.plot_ref", "UUID")
		}
		in.Stage = pl.Stage
	case "correct":
		if r.SupersedesID == nil || r.CorrectionReason == nil {
			return bad("op", "correct bắt buộc supersedes_id và correction_reason")
		}
		sup, err := uuid.Parse(*r.SupersedesID)
		if err != nil {
			return bad("supersedes_id", "UUID")
		}
		in.Supersedes, in.CorrectionReason = &sup, *r.CorrectionReason
	default:
		return bad("op", "create | correct")
	}
	if pl.Rolls == nil {
		return bad("payload.rolls", "bắt buộc số cuộn (proto FM-09)")
	}
	in.Count = domain.Count{Rolls: *pl.Rolls, DiscrepancyReason: pl.DiscrepancyReason}
	in.Location = domain.ParseLocation(pl.GPSStatus, pl.GPS)
	in.VesselCode = pl.VesselCode
	if pl.LooseKg != nil {
		k, err := mass.ParseKg(*pl.LooseKg)
		if err != nil {
			return bad("payload.loose_kg", "kg dạng chuỗi thập phân")
		}
		in.Count.LooseKg = &k
	}
	if pl.HTXConfirmationRef != nil {
		c, err := uuid.Parse(*pl.HTXConfirmationRef)
		if err != nil {
			return bad("payload.htx_confirmation_ref", "UUID")
		}
		in.HTXConfirmationRef = &c
	}
	_, replayed, err := s.count(ctx, p, in)
	if err == nil {
		if replayed {
			return SyncResult{ID: r.ID, Status: "duplicate"}
		}
		return SyncResult{ID: r.ID, Status: "accepted"}
	}
	var pb *problem.Problem
	if errors.As(err, &pb) && pb.Status < 500 {
		res := rejected(r.ID, pb.Code)
		for _, f := range pb.Errors {
			res.Errors = append(res.Errors, FieldIssue{Field: f.Field, Reason: f.Reason})
		}
		return res
	}
	// Lỗi hệ thống: máy giữ và tự gửi lại (như sync.sink_failed của nền tảng).
	return rejected(r.ID, "fieldops.sink_failed")
}

// Change là một thay đổi chiều xuống.
type Change struct {
	Entity  string          `json:"entity"`
	ID      uuid.UUID       `json:"id"`
	Version int64           `json:"version"`
	Op      string          `json:"op"` // upsert | tombstone
	Data    json.RawMessage `json:"data,omitempty"`
}

// PullPage là một trang pull.
type PullPage struct {
	Changes    []Change  `json:"changes"`
	NextCursor string    `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
	ServerTime time.Time `json:"server_time"`
	Reset      bool      `json:"reset"`
}

// OrderSyncData là data của entity collection_order.
type OrderSyncData struct {
	OrderID     uuid.UUID   `json:"order_id"`
	Status      string      `json:"status"`
	Revision    int         `json:"revision"`
	HTXRef      uuid.UUID   `json:"htx_ref"`
	CommuneCode string      `json:"commune_code"`
	WindowFrom  string      `json:"window_from"`
	WindowTo    string      `json:"window_to"`
	PlotRefs    []uuid.UUID `json:"plot_refs"`
	CrewRef     *uuid.UUID  `json:"crew_ref"`
}

func (s *Service) cursorMAC(p authz.Principal, device string, seq int64) []byte {
	m := hmac.New(sha256.New, s.CursorKey)
	m.Write([]byte("fieldops.pull.v1|" + p.TenantID.String() + "|" + p.ActorRef.String() + "|" + device + "|" + strconv.FormatInt(seq, 10)))
	return m.Sum(nil)[:16]
}

func (s *Service) encodeCursor(p authz.Principal, device string, seq int64) string {
	b := make([]byte, 8, 24)
	binary.BigEndian.PutUint64(b, uint64(seq))
	return base64.RawURLEncoding.EncodeToString(append(b, s.cursorMAC(p, device, seq)...))
}

func (s *Service) decodeCursor(p authz.Principal, device, c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(b) != 24 {
		return 0, ErrCursorInvalid
	}
	seq := int64(binary.BigEndian.Uint64(b[:8]))
	if seq < 0 || !hmac.Equal(b[8:], s.cursorMAC(p, device, seq)) {
		return 0, ErrCursorInvalid
	}
	return seq, nil
}

// Pull trả thay đổi lệnh gom sau con trỏ. Lệnh đã duyệt/đang làm → upsert; lệnh
// chưa duyệt, đã đóng hoặc hủy → tombstone (ngừng hiển thị trên máy).
//
// Lọc theo tổ (nghiệm thu G3, sổ tổ gom): field_supervisor thấy mọi lệnh; field_collector
// (không có vai giám sát) chỉ nhận upsert cho lệnh ĐÃ GIAO (assigned) cho tổ mà tài khoản là
// thành viên (crew_current.member_refs) — lệnh khác (chưa giao, giao tổ khác) → tombstone.
// Đổi tổ / đổi thành viên đẩy sync_seq của lệnh nên máy cũ nhận tombstone, máy mới nhận upsert.
func (s *Service) Pull(ctx context.Context, device, cursor string, limit int) (PullPage, error) {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return PullPage{}, err
	}
	if device == "" {
		return PullPage{}, ErrDeviceMismatch
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	seq, err := s.decodeCursor(p, device, cursor)
	if err != nil {
		return PullPage{}, err
	}
	var orders []store.Order
	supervisor := p.Has(RoleSupervisor)
	var mine map[uuid.UUID]bool
	if err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if !supervisor {
			if mine, e = store.CrewsOfMember(ctx, tx, p.ActorRef); e != nil {
				return e
			}
		}
		orders, e = store.OrdersSince(ctx, tx, seq, limit+1)
		return e
	}); err != nil {
		return PullPage{}, err
	}
	pg := PullPage{Changes: []Change{}, ServerTime: s.Clock.Now()}
	if len(orders) > limit {
		orders, pg.HasMore = orders[:limit], true
	}
	for _, o := range orders {
		c := Change{Entity: EntityOrder, ID: o.OrderID, Version: o.SyncSeq, Op: "tombstone"}
		visible := domain.Countable(o.Status)
		if visible && !supervisor {
			visible = o.Status == domain.OrderAssigned && o.CrewRef != nil && mine[*o.CrewRef]
		}
		if visible {
			c.Op = "upsert"
			c.Data, _ = json.Marshal(OrderSyncData{
				OrderID: o.OrderID, Status: o.Status, Revision: o.Revision, HTXRef: o.HTXRef, CommuneCode: o.CommuneCode,
				WindowFrom: o.WindowFrom.Format(dateLayout), WindowTo: o.WindowTo.Format(dateLayout), PlotRefs: o.PlotRefs, CrewRef: o.CrewRef,
			})
		}
		pg.Changes = append(pg.Changes, c)
		seq = o.SyncSeq
	}
	pg.NextCursor = s.encodeCursor(p, device, seq)
	return pg, nil
}
