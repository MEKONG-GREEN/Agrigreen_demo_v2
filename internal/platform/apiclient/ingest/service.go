package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer của phong bì thông điệp lô.
const Producer = "pf/apiclient"

// Owner là port module chủ đã nối cho một hợp đồng nạp.
type Owner struct {
	Target string // tên port đích (ghi vào kết quả, vd "pf/apiclient.ingest_staging")
	Sink   RecordSink
}

// Service là nạp theo lô.
type Service struct {
	// DB: pool đã bọc vai r_apiclient (db.ForModule ở gốc module).
	DB    tenant.Beginner
	Queue Queue
	Gate  Gate
	// Owners: hợp đồng → port module chủ. Hợp đồng không có ở đây → mặc định từ chối (ErrNoOwnerPort).
	Owners map[string]Owner
	// MaxQueued: số lô chờ tối đa mỗi đối tác (≤ 0 → DefaultMaxQueued). RetryAfter của 429 (≤ 0 → DefaultRetryAfter).
	MaxQueued  int
	RetryAfter time.Duration
	// OperatorTenant: tenant đơn vị vận hành — liệt kê đối tác khi mở consumer (Rabbit).
	OperatorTenant uuid.UUID
	Clock          clock.Clock
	Log            *slog.Logger
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

// Wait là Retry-After của 429 khi hàng đợi đối tác đầy.
func (s *Service) Wait() time.Duration {
	if s.RetryAfter <= 0 {
		return DefaultRetryAfter
	}
	return s.RetryAfter
}

// gate trả Gate với Owned theo Owners.
func (s *Service) gate() Gate {
	g := s.Gate
	g.Owned = func(c string) bool { _, ok := s.Owners[c]; return ok }
	return g
}

// Contract kiểm hợp đồng của path ở phiên bản v (mặc định từ chối).
func (s *Service) Contract(name string, version int) (*integration.Contract, error) {
	return s.gate().Resolve(name, version)
}

// Submission là một POST /v1/batches/{contract} đã xác thực.
type Submission struct {
	ClientID       uuid.UUID
	ClientTenant   uuid.UUID // tenant đối tác — từ bản ghi khóa
	OperatorTenant uuid.UUID
	Contract       string // tên hợp đồng của path
	IdemKey        string
	Body           []byte
	ReceivedAt     time.Time
	TraceID        string
}

// Batch là trạng thái một lô (phản hồi 202 và GET /v1/batches/{id}).
type Batch struct {
	ID              uuid.UUID
	Contract        string
	ContractVersion int
	BatchRef        uuid.UUID
	ClientID        uuid.UUID
	Status          string // queued | completed | dead_lettered
	Items           int
	Accepted        int
	Rejected        int
	ProblemCode     string
	ReceivedAt      time.Time
	CompletedAt     *time.Time
	Results         []Result
}

// Trạng thái lô.
const (
	StatusQueued       = "queued"
	StatusCompleted    = "completed"
	StatusDeadLettered = "dead_lettered"
)

// ContractVersionOf đọc contract_version của thân (để kiểm hợp đồng trước khi kiểm phong bì đầy đủ).
func ContractVersionOf(raw []byte) (int, error) {
	var head struct {
		ContractVersion json.Number `json:"contract_version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&head); err != nil {
		return 0, ErrBatchInvalid.WithDetail("thân phải là JSON object")
	}
	v, err := strconv.Atoi(head.ContractVersion.String())
	if err != nil || v < 1 {
		return 0, ErrBatchInvalid.WithField("contract_version", "số nguyên ≥ 1")
	}
	return v, nil
}

// Submit nhận một lô: kiểm hợp đồng (mặc định từ chối) + phong bì + kích thước, ghi sổ ingest_batch (idempotent
// theo (khách hàng, batch_ref = Idempotency-Key)), rồi phát vào hàng đợi của đối tác. Gọi lặp cùng thân → cùng
// lô (replayed = true), phát lại nếu lô chưa có kết cục (an toàn: inbox khử trùng). Hàng đợi đầy → 429
// ErrQueueSaturated (lô mới không được ghi). Phát lỗi sau khi ghi → 503 ErrQueueUnavailable; gọi lặp cùng khóa
// sẽ phát lại.
func (s *Service) Submit(ctx context.Context, sub Submission) (Batch, bool, error) {
	if s.Queue == nil {
		return Batch{}, false, ErrQueueUnavailable.WithCause(errNoQueue)
	}
	version, err := ContractVersionOf(sub.Body)
	if err != nil {
		return Batch{}, false, err
	}
	c, err := s.Contract(sub.Contract, version)
	if err != nil {
		return Batch{}, false, err
	}
	env, err := ParseEnvelope(sub.Body, sub.Contract, sub.IdemKey)
	if err != nil {
		return Batch{}, false, err
	}
	if err := CheckSize(c, len(env.Items)); err != nil {
		return Batch{}, false, err
	}
	hash := Hash([]byte(c.ID()+"\n"), sub.Body)
	items, err := json.Marshal(env.Items)
	if err != nil {
		return Batch{}, false, err
	}
	row := batchRow{ID: ids.New(), TenantID: sub.ClientTenant, ClientID: sub.ClientID, OperatorTenant: sub.OperatorTenant, Contract: c.Name,
		ContractVersion: c.Version, BatchRef: env.BatchRef, RequestHash: hash, ItemCount: len(env.Items), Items: items, SentAt: env.SentAt,
		AsOf: env.AsOf, TraceID: sub.TraceID, ReceivedAt: sub.ReceivedAt}

	var (
		b        Batch
		replayed bool
		done     bool
	)
	lookup := func(tx pgx.Tx) (bool, error) {
		prev, err := batchByRef(ctx, tx, sub.ClientID, env.BatchRef)
		if err != nil || prev == nil {
			return false, err
		}
		if prev.RequestHash != hash {
			return false, problem.IdempotencyConflict
		}
		o, err := outcomeOf(ctx, tx, prev.ID)
		if err != nil {
			return false, err
		}
		b, replayed, done = view(*prev, o, nil), true, o != nil
		return true, nil
	}
	err = tenant.WithTenantTx(ctx, s.DB, sub.ClientTenant, func(tx pgx.Tx) error { _, err := lookup(tx); return err })
	if err != nil {
		return Batch{}, false, err
	}
	if !replayed {
		// Vách ngăn R-INT-04: hàng đợi của CHÍNH đối tác đầy → 429, không ghi lô.
		depth, err := s.Queue.Depth(ctx, sub.ClientID)
		if err != nil {
			return Batch{}, false, ErrQueueUnavailable.WithCause(err)
		}
		if Saturation(depth, s.MaxQueued) {
			return Batch{}, false, ErrQueueSaturated
		}
		err = tenant.WithTenantTx(ctx, s.DB, sub.ClientTenant, func(tx pgx.Tx) error {
			if err := insertBatch(ctx, tx, row); err != nil {
				return err
			}
			b = view(row, nil, nil)
			return nil
		})
		if errors.Is(err, ledger.ErrDuplicate) { // cùng khóa gửi đồng thời: đọc lại
			err = tenant.WithTenantTx(ctx, s.DB, sub.ClientTenant, func(tx pgx.Tx) error { _, err := lookup(tx); return err })
		}
		if err != nil {
			return Batch{}, false, err
		}
	}
	if done {
		return b, replayed, nil
	}
	if err := s.Queue.Publish(ctx, sub.ClientID, s.message(b, sub.ClientTenant, sub.TraceID)); err != nil {
		if errors.Is(err, ErrQueueSaturated) {
			return Batch{}, false, ErrQueueSaturated
		}
		s.log().ErrorContext(ctx, "không phát được lô vào hàng đợi đối tác — gọi lặp cùng Idempotency-Key sẽ phát lại",
			"event", "ingest.publish_failed", "batch_id", b.ID, "client_id", sub.ClientID, "outcome", "error", "error", err.Error())
		return Batch{}, false, ErrQueueUnavailable.WithCause(err)
	}
	return b, replayed, nil
}

type messagePayload struct {
	ClientID uuid.UUID `json:"client_id"`
	Contract string    `json:"contract"`
}

func (s *Service) message(b Batch, tenantID uuid.UUID, trace string) outbox.Envelope {
	p, _ := json.Marshal(messagePayload{ClientID: b.ClientID, Contract: b.Contract})
	now := s.now()
	return outbox.Envelope{EventID: b.ID, EventType: MessageType, SchemaVersion: 1, TenantID: tenantID, OccurredAt: b.ReceivedAt,
		RecordedAt: now, TraceID: trace, Producer: Producer, Payload: p}
}

func view(r batchRow, o *outcomeRow, results []Result) Batch {
	b := Batch{ID: r.ID, Contract: r.Contract, ContractVersion: r.ContractVersion, BatchRef: r.BatchRef, ClientID: r.ClientID,
		Status: StatusQueued, Items: r.ItemCount, ReceivedAt: r.ReceivedAt, Results: results}
	if o != nil {
		b.Status, b.Accepted, b.Rejected, b.ProblemCode = o.Outcome, o.Accepted, o.Rejected, o.ProblemCode
		at := o.At
		b.CompletedAt = &at
	}
	return b
}

// Get đọc lô của chính khách hàng (tenant đối tác + client_id) kèm kết quả từng bản ghi. Lô của khách hàng
// khác → ErrBatchNotFound (RLS chặn tenant khác; client_id chặn khách hàng khác cùng tenant).
func (s *Service) Get(ctx context.Context, clientTenant, clientID, id uuid.UUID) (Batch, error) {
	var b Batch
	err := tenant.WithTenantTx(ctx, s.DB, clientTenant, func(tx pgx.Tx) error {
		r, err := batchByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if r == nil || r.ClientID != clientID {
			return ErrBatchNotFound
		}
		o, err := outcomeOf(ctx, tx, id)
		if err != nil {
			return err
		}
		var res []Result
		if o != nil {
			if res, err = resultsOf(ctx, tx, id); err != nil {
				return err
			}
		}
		b = view(*r, o, res)
		return nil
	})
	return b, err
}

// Handle là inbox.Handler của bộ xử lý lô (giao dịch tenant đối tác, vai r_apiclient): kiểm hợp đồng lần nữa
// (hợp đồng có thể đã thu hồi), từng bản ghi qua hợp đồng → port module chủ → ghi kết quả; cuối cùng kết cục
// completed. Lỗi tạm của port chủ → trả lỗi (giao dịch rollback, thông điệp giao lại).
func (s *Service) Handle(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	r, err := batchByID(ctx, tx, env.EventID)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("ingest: không có lô " + env.EventID.String() + " trong tenant của thông điệp")
	}
	if o, err := outcomeOf(ctx, tx, r.ID); err != nil || o != nil {
		return err
	}
	var items []Item
	if err := json.Unmarshal(r.Items, &items); err != nil {
		return err
	}
	now := s.now()
	c, gerr := s.Contract(r.Contract, r.ContractVersion)
	owner := s.Owners[r.Contract]
	var accepted, rejected int
	for i, it := range items {
		res := Result{Index: i, EntityKey: it.EntityKey, Outcome: OutcomeAccepted}
		target := "gate"
		reject := func(p *problem.Problem, detail string) {
			res.Outcome, res.ProblemCode, res.Detail = OutcomeRejected, p.Code, detail
		}
		switch {
		case gerr != nil:
			reject(problem.From(gerr), "")
		default:
			if err := c.ValidateRecord(it.Record); err != nil {
				reject(ErrRecordInvalid, recordDetail(err))
				break
			}
			key, err := c.EntityKeyOf(it.Record)
			if err != nil || key != it.EntityKey {
				reject(ErrEntityKeyMismatch, "")
				break
			}
			bkey, err := BusinessKey(c, it.Record)
			if err != nil {
				reject(ErrRecordInvalid, trim(err.Error()))
				break
			}
			target = owner.Target
			serr := owner.Sink.Ingest(ctx, Record{Contract: c.Name, ContractVersion: c.Version, PartnerTenant: r.TenantID, ClientID: r.ClientID,
				BatchID: r.ID, Index: i, EntityKey: key, BusinessKey: bkey, Payload: it.Record, ReceivedAt: r.ReceivedAt})
			if serr != nil {
				p, ok := rejection(serr)
				if !ok {
					return serr // lỗi tạm: cả thông điệp giao lại
				}
				reject(p, trim(p.Detail))
			}
		}
		if res.Outcome == OutcomeAccepted {
			accepted++
		} else {
			rejected++
		}
		if err := insertResult(ctx, tx, ids.New(), r.TenantID, r.ClientID, r.ID, target, res, now); err != nil {
			return err
		}
	}
	return insertOutcome(ctx, tx, ids.New(), r.TenantID, r.ClientID, r.ID,
		outcomeRow{Outcome: StatusCompleted, Accepted: accepted, Rejected: rejected, At: now})
}

func recordDetail(err error) string {
	msg := err.Error()
	for _, p := range []string{"publication: van từ chối payload: "} {
		if len(msg) > len(p) && msg[:len(p)] == p {
			msg = msg[len(p):]
		}
	}
	return trim(msg)
}

// Failure là inbox.FailureRecorder: lần giao cuối thất bại (thông điệp sang DLQ của đối tác) → kết cục
// dead_lettered trong giao dịch riêng. Chỉ lưu mã chuẩn hóa, không thông điệp lỗi (có thể chứa dữ liệu).
func (s *Service) Failure(ctx context.Context, f inbox.Failure) error {
	if !f.DeadLettered {
		return nil
	}
	var p messagePayload
	if err := json.Unmarshal(f.Envelope.Payload, &p); err != nil || p.ClientID == uuid.Nil {
		return errors.New("ingest: phong bì thông điệp lô thiếu client_id")
	}
	code := ErrUnavailableOwnerPort.Code
	if pr, ok := rejection(f.Err); ok {
		code = pr.Code
	}
	err := tenant.WithTenantTx(ctx, s.DB, f.Envelope.TenantID, func(tx pgx.Tx) error {
		return insertOutcome(ctx, tx, ids.New(), f.Envelope.TenantID, p.ClientID, f.Envelope.EventID,
			outcomeRow{Outcome: StatusDeadLettered, ProblemCode: code, DeliveryCount: int(f.DeliveryCount) + 1, At: s.now()})
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return nil
	}
	if err == nil {
		s.log().WarnContext(ctx, "lô nạp vào DLQ của đối tác", "event", "ingest.dead_lettered", "batch_id", f.Envelope.EventID,
			"client_id", p.ClientID, "outcome", "dead_letter")
	}
	return err
}

// Partners liệt kê khách hàng API active của đơn vị vận hành (mở consumer hàng đợi khi khởi động).
func (s *Service) Partners(ctx context.Context) ([]uuid.UUID, error) {
	if s.OperatorTenant == uuid.Nil {
		return nil, nil
	}
	var out []uuid.UUID
	err := tenant.WithTenantTx(ctx, s.DB, s.OperatorTenant, func(tx pgx.Tx) error {
		var err error
		out, err = activePartners(ctx, tx)
		return err
	})
	return out, err
}

// Worker trả phần xử lý cho Queue.Run / Memory.Drain.
func (s *Service) Worker() Worker {
	return Worker{DB: s.DB, Handler: s.Handle, Failure: s.Failure, Partners: s.Partners, Log: s.log()}
}

// Run tiêu thụ hàng đợi các đối tác tới khi ctx hủy.
func (s *Service) Run(ctx context.Context) error {
	if s.Queue == nil {
		return errNoQueue
	}
	return s.Queue.Run(ctx, s.Worker())
}

func itoa(i int) string { return strconv.Itoa(i) }
