package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PushInput là một lô máy gửi lên. BatchKey = Idempotency-Key của request.
type PushInput struct {
	DeviceHeader string
	DeviceID     string // device_id trong thân — phải trùng header
	BatchKey     string
	Records      []domain.RawRecord
}

// ResultItem là kết quả của một bản ghi; ID là chuỗi máy gửi (kể cả khi sai dạng)
// để máy khớp lại với hàng đợi.
type ResultItem struct {
	ID         string
	Status     string
	ConflictID *uuid.UUID
	Code       string
	Errors     []problem.FieldError
}

// Push nhận một lô bản ghi chiều lên. Mỗi bản ghi có kết quả riêng; một bản ghi
// lỗi không làm hỏng cả lô. Gọi lặp cùng Idempotency-Key do middleware phát lại
// nguyên phản hồi; bản ghi gửi lại trong lô khác được nhận diện theo idem_key
// (sổ inbound) và trả duplicate mà không gọi lại Sink.
func (s *Service) Push(ctx context.Context, in PushInput) ([]ResultItem, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	dev, err := s.checkDevice(ctx, p, in.DeviceHeader)
	if err != nil {
		return nil, err
	}
	if in.BatchKey == "" {
		return nil, problem.IdempotencyRequired
	}
	if d, err := uuid.Parse(in.DeviceID); err != nil || d != dev.ID {
		return nil, problem.Validation.WithField("device_id", "phải trùng header "+port.HeaderDeviceID)
	}
	if len(in.Records) > domain.MaxBatchRecords {
		return nil, problem.Validation.WithField("records", fmt.Sprintf("tối đa %d bản ghi mỗi lô", domain.MaxBatchRecords))
	}
	scope, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()

	keys := make([]string, 0, len(in.Records))
	for _, r := range in.Records {
		keys = append(keys, r.IdemKey)
	}
	var written map[string]bool
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		written, e = store.WrittenKeys(ctx, tx, keys)
		return e
	}); err != nil {
		return nil, err
	}

	results := make([]ResultItem, len(in.Records))
	rows := make([]store.Inbound, len(in.Records))
	var conflicts []domain.Conflict
	var sla *time.Duration
	for i, raw := range in.Records {
		rec, errs := domain.ParseRecord(raw, dev.ID, now)
		res := port.Result{ID: rec.ID}
		switch {
		case len(errs) > 0:
			res.Status, res.Code, res.Errors = port.StatusRejected, problem.Validation.Code, errs
		case s.sinks[rec.Entity] == nil:
			res.Status, res.Code = port.StatusRejected, port.CodeUnknownEntity
			res.Errors = []problem.FieldError{{Field: "entity", Reason: "không có module nhận entity này"}}
		case written[rec.IdemKey]:
			res.Status = port.StatusDuplicate
		default:
			res = domain.NormalizeResult(s.accept(ctx, scope, rec), rec.ID, ids.New)
		}
		if res.Status == port.StatusAccepted || res.Status == port.StatusDuplicate || res.Status == port.StatusConflict {
			written[rec.IdemKey] = true
		}
		if res.Status == port.StatusConflict {
			if sla == nil {
				d := s.conflictSLA(ctx, now)
				sla = &d
			}
			conflicts = append(conflicts, domain.Conflict{
				TenantID: p.TenantID, ID: *res.ConflictID, Entity: rec.Entity, FirstRecordID: rec.ID, FirstIdemKey: rec.IdemKey,
				DeviceID: dev.ID, ReportedBy: p.ActorRef, OwnerOrgRef: orgPtr(p), OpenedAt: now, DueAt: now.Add(*sla),
			})
		}
		results[i] = ResultItem{ID: raw.ID, Status: res.Status, ConflictID: res.ConflictID, Code: res.Code, Errors: res.Errors}
		rows[i] = inboundRow(p, dev.ID, in.BatchKey, i, raw, rec, res, now)
	}

	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		for _, r := range rows {
			if err := store.InsertInbound(ctx, tx, r); err != nil {
				return ledger.Classify(err)
			}
		}
		for _, c := range conflicts {
			inserted, err := store.InsertConflict(ctx, tx, c)
			if err != nil {
				return err
			}
			if !inserted {
				continue
			}
			owner := c.OwnerOrgRef
			if err := outbox.Enqueue(ctx, tx, outbox.Event{
				ID: ids.New(), TenantID: c.TenantID, Type: port.EventConflictOpened, SchemaVersion: 1,
				OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: "pf/sync",
				Payload: port.ConflictOpenedV1{ConflictID: c.ID, Entity: c.Entity, RecordID: c.FirstRecordID, DeviceID: c.DeviceID,
					OwnerOrgRef: owner, OpenedAt: now.Format(time.RFC3339), DueAt: c.DueAt.Format(time.RFC3339)},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		// Lô đã được ghi sổ ở lần chạy trước bị ngắt giữa chừng (middleware cho chạy lại).
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return results, nil
}

// accept gọi Sink; lỗi hoảng (panic) của module nhận quy về sync.sink_failed để
// không làm hỏng cả lô.
func (s *Service) accept(ctx context.Context, scope authz.Scope, rec port.Record) (res port.Result) {
	defer func() {
		if r := recover(); r != nil {
			s.Log.Error("sink hoảng khi nhận bản ghi", "event", "sync.sink_panic", "entity", rec.Entity,
				"record_id", rec.ID, "trace_id", telemetry.TraceID(ctx))
			res = port.Result{ID: rec.ID, Status: port.StatusRejected, Code: port.CodeSinkFailed}
		}
	}()
	return s.sinks[rec.Entity].Accept(ctx, scope, rec)
}

// conflictSLA đọc tham số sync.conflict_sla_hours tại thời điểm hiện tại; chưa
// có bản đã phê duyệt → 72 giờ (C-8 đang chờ chốt).
func (s *Service) conflictSLA(ctx context.Context, now time.Time) time.Duration {
	if s.Params == nil {
		return port.DefaultConflictSLA
	}
	v, err := s.Params.At(ctx, port.ParamConflictSLAHours, now)
	if err != nil {
		if !errors.Is(err, mdport.ErrParamNotFound) {
			s.Log.Warn("không đọc được tham số SLA xung đột, dùng mặc định", "event", "sync.param_unavailable",
				"param", port.ParamConflictSLAHours, "trace_id", telemetry.TraceID(ctx))
		}
		return port.DefaultConflictSLA
	}
	secs := v.Value.Mul(decimal.NewFromInt(3600)).IntPart()
	if secs <= 0 {
		return port.DefaultConflictSLA
	}
	return time.Duration(secs) * time.Second
}

func inboundRow(p authz.Principal, device uuid.UUID, batch string, i int, raw domain.RawRecord, rec port.Record,
	res port.Result, now time.Time) store.Inbound {
	row := store.Inbound{
		ID: ids.New(), TenantID: p.TenantID, DeviceID: device, BatchKey: batch, Index: i,
		RecordIdemKey: raw.IdemKey, Entity: raw.Entity, Op: raw.Op, Status: res.Status, ConflictID: res.ConflictID,
		RecordSupersedesID: rec.SupersedesID, PayloadSHA256: domain.PayloadSHA256(raw.Payload), EventTime: now, ActorRef: p.ActorRef,
	}
	if rec.ID != uuid.Nil {
		id := rec.ID
		row.RecordID = &id
	}
	if !rec.EventTime.IsZero() {
		row.EventTime = rec.EventTime
	}
	if res.Code != "" {
		code := res.Code
		row.Code = &code
	}
	return row
}
