package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// RegisterDevice đăng ký thiết bị cho người gọi. actor_ref và org_ref lấy từ
// token (R-16), không từ thân. Đăng ký lại cùng mã bởi cùng người → trả bản cũ
// (created = false) và cập nhật phiên bản app; mã đã bị thu hồi → sync.device_revoked
// (máy phải xóa dữ liệu và sinh mã mới); mã của người khác → sync.device_taken.
func (s *Service) RegisterDevice(ctx context.Context, in domain.Registration) (d domain.Device, created bool, err error) {
	p, err := principal(ctx)
	if err != nil {
		return d, false, err
	}
	id, errs := domain.ParseRegistration(in)
	if len(errs) > 0 {
		return d, false, validation(errs)
	}
	now := s.Clock.Now()
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.DeviceByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		switch {
		case prev == nil:
			d = domain.Device{TenantID: p.TenantID, ID: id, ActorRef: p.ActorRef, OrgRef: orgPtr(p),
				Platform: in.Platform, AppVersion: in.AppVersion, RegisteredAt: now}
			created = true
			return store.InsertDevice(ctx, tx, d)
		case prev.ActorRef != p.ActorRef:
			return ErrDeviceTaken
		case prev.Revoked():
			return ErrDeviceRevoked
		}
		d = *prev
		d.Platform, d.AppVersion = in.Platform, in.AppVersion
		return store.RefreshDevice(ctx, tx, id, in.Platform, in.AppVersion, now)
	})
	return d, created, err
}

// RevokeDevice thu hồi thiết bị (Admin API, vai device_admin). Từ lời gọi kế tiếp,
// mọi pull/push/tải tệp của thiết bị trả 403 sync.device_revoked.
func (s *Service) RevokeDevice(ctx context.Context, id uuid.UUID, reason string) (domain.Device, error) {
	p, err := authz.Require(ctx, port.RoleDeviceAdmin)
	if err != nil {
		return domain.Device{}, err
	}
	reason = strings.TrimSpace(reason)
	if !domain.ValidReason(reason) {
		return domain.Device{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.Clock.Now()
	var d domain.Device
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.DeviceByID(ctx, tx, id, true)
		switch {
		case err != nil:
			return err
		case prev == nil:
			return ErrDeviceNotFound
		case prev.Revoked():
			return ErrDeviceAlreadyRevoked
		}
		if err := store.RevokeDevice(ctx, tx, id, p.ActorRef, reason, now); err != nil {
			return err
		}
		d = *prev
		by := p.ActorRef
		d.RevokedAt, d.RevokedBy, d.RevokeReason = &now, &by, &reason
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventDeviceRevoked, SchemaVersion: 1,
			OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: "pf/sync",
			Payload: port.DeviceRevokedV1{DeviceID: id, ActorRef: prev.ActorRef, RevokedBy: p.ActorRef, Reason: reason,
				EventTime: now.Format(time.RFC3339)},
		})
	})
	return d, err
}

// DevicePage là một trang thiết bị.
type DevicePage struct {
	Items []domain.Device
	Next  *uuid.UUID
}

// ListDevices liệt kê thiết bị của tenant (Admin API), lọc theo người đăng ký.
func (s *Service) ListDevices(ctx context.Context, actor *uuid.UUID, after uuid.UUID, limit int) (DevicePage, error) {
	p, err := authz.Require(ctx, port.RoleDeviceAdmin)
	if err != nil {
		return DevicePage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []domain.Device
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListDevices(ctx, tx, actor, after, limit+1)
		return e
	})
	if err != nil {
		return DevicePage{}, err
	}
	var pg DevicePage
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].ID
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}
