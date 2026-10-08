package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DeactivationPreview là phạm vi ảnh hưởng hiển thị TRƯỚC khi xác nhận vô hiệu hóa HTX
// (HTX US-HTXSTATUS-01 AC-1). OpenSeasons/PendingLogs nil khi chưa nối nguồn vụ mùa.
type DeactivationPreview struct {
	CooperativeRef   uuid.UUID
	Active           bool
	ActiveHouseholds int
	ActiveChildren   int
	OpenSeasons      *int64
	PendingLogs      *int64
}

// PreviewDeactivation đếm nông hộ đang là thành viên (pf/cooperative), tổ chức con đang hoạt động, vụ
// chưa xong và nhật ký chờ duyệt của HTX — chỉ đọc, cùng vai và phạm vi với Deactivate.
func (r *Registry) PreviewDeactivation(ctx context.Context, ref uuid.UUID) (DeactivationPreview, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return DeactivationPreview{}, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return DeactivationPreview{}, err
	}
	out := DeactivationPreview{CooperativeRef: ref}
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.OrgByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if cur == nil || cur.Kind != port.OrgCooperative || !visible(sc, cur.AdminUnitCode) {
			return ErrOrgNotFound
		}
		out.Active = cur.Active
		out.ActiveChildren, err = store.CountActiveChildren(ctx, tx, ref)
		return err
	})
	if err != nil {
		return DeactivationPreview{}, err
	}
	if out.ActiveHouseholds, err = r.activeHouseholds(ctx, p.TenantID, ref); err != nil {
		return DeactivationPreview{}, err
	}
	if r.Activity != nil {
		open, pending, err := r.Activity.CoopActivity(tenant.WithID(ctx, p.TenantID), ref)
		if err != nil {
			return DeactivationPreview{}, ErrDependency.WithDetail("không đọc được vụ mùa / nhật ký của HTX").WithCause(err)
		}
		out.OpenSeasons, out.PendingLogs = &open, &pending
	}
	return out, nil
}
