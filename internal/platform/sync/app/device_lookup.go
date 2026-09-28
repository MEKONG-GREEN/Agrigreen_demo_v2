package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DeviceLookup hiện thực port.Devices trên pool vai sync_app. Không phụ thuộc
// Source/Sink nên gốc lắp ráp dựng được trước module đồng bộ (tránh vòng phụ thuộc
// với module có Source cần kiểm thiết bị, như pf/notify).
type DeviceLookup struct{ DB tenant.Beginner }

var _ port.Devices = DeviceLookup{}

// Device thực hiện port.Devices.
func (l DeviceLookup) Device(ctx context.Context, tid, id uuid.UUID) (port.DeviceInfo, error) {
	var d *domain.Device
	err := tenant.WithTenantTx(ctx, l.DB, tid, func(tx pgx.Tx) error {
		var e error
		d, e = store.DeviceByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return port.DeviceInfo{}, err
	}
	if d == nil {
		return port.DeviceInfo{}, port.ErrDeviceNotFound
	}
	return port.DeviceInfo{ID: d.ID, ActorRef: d.ActorRef, OrgRef: d.OrgRef, Platform: d.Platform, Revoked: d.Revoked()}, nil
}
