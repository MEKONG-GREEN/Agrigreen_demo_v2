package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Sink nhận minh chứng thu ngoại tuyến qua pf/sync (entity "evidence").
type Sink struct{ Svc *Service }

var _ syncport.Sink = Sink{}

// Entities thực hiện syncport.Sink.
func (Sink) Entities() []string { return []string{port.SyncEntity} }

func rejected(rec syncport.Record, code string, errs []problem.FieldError) syncport.Result {
	return syncport.Result{ID: rec.ID, Status: syncport.StatusRejected, Code: code, Errors: errs}
}

// Accept thực hiện syncport.Sink: một bản ghi, một giao dịch; idempotent theo
// IdemKey (đã ghi → duplicate). Dữ liệu sai → rejected kèm lỗi theo trường; nông
// hộ chưa đồng ý → rejected mã mrv.consent_missing.
func (k Sink) Accept(ctx context.Context, scope authz.Scope, rec syncport.Record) syncport.Result {
	if rec.Entity != port.SyncEntity {
		return rejected(rec, problem.BadRequest.Code, []problem.FieldError{{Field: "entity", Reason: "pf/mrv chỉ nhận entity evidence"}})
	}
	var w CapturePayload
	if err := json.Unmarshal(rec.Payload, &w); err != nil {
		return rejected(rec, problem.Validation.Code, []problem.FieldError{{Field: "payload", Reason: "phải là JSON đối tượng minh chứng"}})
	}
	// Các trường phong bì lấy từ bản ghi đồng bộ, không từ payload.
	w.ID, w.EventTime, w.SupersedesID, w.CorrectionReason, w.DeviceID = nil, "", nil, nil, nil
	in, errs := w.Parse()
	in.ID, in.EventTime, in.SupersedesID, in.CorrectionReason, in.IdemKey = &rec.ID, rec.EventTime, rec.SupersedesID, rec.CorrectionReason, rec.IdemKey
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		in.DeviceID = &d
	}
	switch rec.Op {
	case syncport.OpCreate:
		if rec.SupersedesID != nil {
			errs = append(errs, problem.FieldError{Field: "supersedes_id", Reason: "op create không có supersedes_id — dùng op correct"})
		}
	case syncport.OpCorrect:
		if rec.SupersedesID == nil || rec.CorrectionReason == nil {
			errs = append(errs, problem.FieldError{Field: "correction_reason", Reason: "op correct bắt buộc supersedes_id và correction_reason"})
		}
	default:
		errs = append(errs, problem.FieldError{Field: "op", Reason: "chỉ có create hoặc correct"})
	}
	if len(errs) > 0 {
		return rejected(rec, problem.Validation.Code, errs)
	}
	e, replayed, err := k.Svc.Capture(ctx, scope, in)
	if err != nil {
		var p *problem.Problem
		if errors.As(err, &p) && p.Status < 500 {
			return rejected(rec, p.Code, p.Errors)
		}
		// Lỗi hệ thống: máy giữ bản ghi và gửi lại ở lượt sau (idempotent theo idem_key).
		return rejected(rec, problem.Internal.Code, nil)
	}
	if replayed {
		return syncport.Result{ID: e.ID, Status: syncport.StatusDuplicate}
	}
	return syncport.Result{ID: e.ID, Status: syncport.StatusAccepted}
}
