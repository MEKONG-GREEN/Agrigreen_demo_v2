package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SyncSource là chiều xuống của van thu gom MKG ↔ HTX: htx_collection_schedule, htx_collection_statement
// (docs/dev/sync-contract.md). Vụ, nhật ký, SOP thuộc pf/cooperative từ W3.3.
type SyncSource struct{ Svc *Service }

// SyncSink là chiều lên htx_collection_response.
type SyncSink struct{ Svc *Service }

var (
	_ syncport.Source = SyncSource{}
	_ syncport.Sink   = SyncSink{}
)

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string {
	return []string{port.EntityCollectionSchedule, port.EntityCollectionStatement}
}

// syncAccess tính quyền đọc cho đồng bộ van: chỉ quản trị (toàn tenant) và quản lý HTX (HTX của mình) nhận
// lịch gom / bảng kê; vai khác không nhận entity (ok = false, không lỗi).
func (s *Service) syncAccess(ctx context.Context) (access, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return access{}, false, problem.Unauthenticated
	}
	switch {
	case p.Has(authz.RolePlatformAdmin):
		return access{p: p, kind: accAll, sc: authz.Scope{All: true}}, true, nil
	case p.Has(port.RoleCoopManager):
		return access{p: p, kind: accCoop}, true, nil
	}
	return access{}, false, nil
}

// Changes thực hiện syncport.Source.
func (src SyncSource) Changes(ctx context.Context, _ authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	a, ok, err := src.Svc.syncAccess(ctx)
	if err != nil || !ok {
		return []syncport.Change{}, after, err
	}
	return src.collectionChanges(ctx, a, entity, after, limit)
}

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string { return []string{port.EntityCollectionResponse} }

func rejected(id uuid.UUID, err error) syncport.Result {
	var pr *problem.Problem
	if !errors.As(err, &pr) {
		pr = problem.Internal // lỗi hệ thống: pf/sync coi internal_error là thử lại được
	}
	errs := pr.Errors
	if len(errs) == 0 {
		reason := pr.Detail
		if reason == "" {
			reason = pr.Title
		}
		errs = []problem.FieldError{{Field: "payload", Reason: reason}}
	}
	return syncport.Result{ID: id, Status: syncport.StatusRejected, Code: pr.Code, Errors: errs}
}

// Accept thực hiện syncport.Sink: idempotent theo IdemKey (sổ chống trùng).
func (snk SyncSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	if rec.IdemKey == "" {
		return rejected(rec.ID, problem.Validation.WithField("idem_key", "bắt buộc"))
	}
	if rec.ID.Version() != 7 {
		return rejected(rec.ID, problem.Validation.WithField("id", "phải là UUIDv7"))
	}
	if rec.EventTime.IsZero() {
		return rejected(rec.ID, problem.Validation.WithField("event_time", "bắt buộc (R-13)"))
	}
	var device *string
	if rec.DeviceID != uuid.Nil {
		d := rec.DeviceID.String()
		device = &d
	}
	if rec.Entity != port.EntityCollectionResponse {
		return rejected(rec.ID, problem.Validation.WithField("entity", "không thuộc module pf/cropcycle"))
	}
	replayed, err := snk.collectionResponse(ctx, rec, device)
	switch {
	case err != nil:
		return rejected(rec.ID, err)
	case replayed:
		return syncport.Result{ID: rec.ID, Status: syncport.StatusDuplicate}
	}
	return syncport.Result{ID: rec.ID, Status: syncport.StatusAccepted}
}
