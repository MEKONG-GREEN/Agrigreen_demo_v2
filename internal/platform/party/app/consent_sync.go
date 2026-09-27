package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// EntityConsent là entity chiều lên của đồng ý theo mục đích (docs/dev/sync-contract.md).
const EntityConsent = "consent"

// ConsentSink nhận bản ghi đồng ý thu ngoại tuyến qua pf/sync. Dùng lại use case
// Record: cùng kiểm vai (consent_writer), cùng sổ, cùng outbox với POST /v1/consents.
type ConsentSink struct{ Svc *Service }

var _ syncport.Sink = ConsentSink{}

// ConsentSyncPayload là payload của entity consent. id, event_time, device_id
// và idem_key lấy từ phong bì bản ghi đồng bộ, không từ payload.
type ConsentSyncPayload struct {
	SubjectRef  string `json:"subject_ref"`
	Purpose     string `json:"purpose"`
	TextVersion string `json:"text_version"`
	Status      string `json:"status"`
	Channel     string `json:"channel"` // bỏ trống = field_app
}

// Entities thực hiện syncport.Sink.
func (ConsentSink) Entities() []string { return []string{EntityConsent} }

func consentRejected(id uuid.UUID, code string, errs []problem.FieldError) syncport.Result {
	return syncport.Result{ID: id, Status: syncport.StatusRejected, Code: code, Errors: errs}
}

// Accept thực hiện syncport.Sink. Idem_key của thiết bị ("dev:<máy>:<seq>") là
// khóa chống trùng của sổ đồng ý: gửi lại → duplicate. Rút lại đồng ý là một
// bản ghi MỚI với status = withdrawn (op create), không phải bản ghi bù — nên
// op correct bị từ chối.
func (k ConsentSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	if rec.Entity != EntityConsent {
		return consentRejected(rec.ID, problem.Validation.Code, []problem.FieldError{{Field: "entity", Reason: "pf/party chỉ nhận entity consent"}})
	}
	if rec.Op != syncport.OpCreate {
		return consentRejected(rec.ID, problem.Validation.Code, []problem.FieldError{{Field: "op", Reason: "consent chỉ nhận op = create; rút lại đồng ý là bản ghi mới status = withdrawn"}})
	}
	var body ConsentSyncPayload
	if err := json.Unmarshal(rec.Payload, &body); err != nil {
		return consentRejected(rec.ID, problem.Validation.Code, []problem.FieldError{{Field: "payload", Reason: "phải là JSON đối tượng đồng ý"}})
	}
	subject, err := ids.ParseRef(body.SubjectRef)
	if err != nil {
		return consentRejected(rec.ID, problem.Validation.Code, []problem.FieldError{{Field: "payload.subject_ref", Reason: "phải là UUID"}})
	}
	channel := domain.Channel(body.Channel)
	if channel == "" {
		channel = domain.FieldApp
	}
	id := rec.ID
	in := RecordInput{
		ID: &id, SubjectRef: subject, Purpose: body.Purpose, TextVersion: body.TextVersion,
		Status: domain.Status(body.Status), Channel: channel, EventTime: rec.EventTime, IdemKey: rec.IdemKey,
	}
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		in.DeviceID = &d
	}
	_, replayed, err := k.Svc.Record(ctx, in)
	if err != nil {
		var p *problem.Problem
		if errors.As(err, &p) && p.Status < 500 {
			return consentRejected(rec.ID, p.Code, p.Errors)
		}
		// Lỗi hệ thống: không lộ chi tiết; máy giữ bản ghi và gửi lại (idempotent theo idem_key).
		return consentRejected(rec.ID, problem.Internal.Code, nil)
	}
	if replayed {
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}
