package app

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var (
	_ port.CoopMarkers      = (*Registry)(nil)
	_ port.CooperativeSites = (*Registry)(nil)
)

// MaxMarkers là số điểm HTX tối đa một lần đọc khung nhìn.
const MaxMarkers = 1000

// MarkersInBBox thực hiện geo/port.CoopMarkers: điểm vị trí hồ sơ (pf_party.cooperative.location)
// của HTX đang hoạt động trong khung nhìn — marker/dự phòng của lớp ranh giới HTX (GIS
// US-BND-02). Tenant lấy từ context; không kiểm phạm vi (pf/geo tự lọc).
func (r *Registry) MarkersInBBox(ctx context.Context, minLon, minLat, maxLon, maxLat float64, limit int) ([]port.CoopMarker, error) {
	for _, v := range []float64{minLon, minLat, maxLon, maxLat} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, problem.Validation.WithField("bbox", "toạ độ không hợp lệ")
		}
	}
	if limit <= 0 || limit > MaxMarkers {
		limit = MaxMarkers
	}
	var out []port.CoopMarker
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.CoopMarkersInBBox(ctx, tx, minLon, minLat, maxLon, maxLat, limit)
		return e
	})
	return out, err
}

// ActiveCooperatives thực hiện port.CooperativeSites (nguồn cung của mô phỏng chọn Hub).
func (r *Registry) ActiveCooperatives(ctx context.Context) ([]port.CooperativeSite, error) {
	var out []port.CooperativeSite
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.ActiveCooperativeSites(ctx, tx)
		return e
	})
	return out, err
}

// DeactivationPreview là phạm vi ảnh hưởng hiển thị TRƯỚC khi xác nhận vô hiệu hóa HTX
// (HTX US-HTXSTATUS-01 AC-1). OpenSeasons/PendingLogs nil khi chưa nối pf/cropcycle.
type DeactivationPreview struct {
	CooperativeRef   uuid.UUID
	Active           bool
	ActiveHouseholds int
	ActiveChildren   int
	OpenSeasons      *int64
	PendingLogs      *int64
}

// PreviewDeactivation đếm nông hộ đang là thành viên, tổ chức con đang hoạt động, vụ
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
		if out.ActiveHouseholds, err = store.CountActiveHouseholds(ctx, tx, ref); err != nil {
			return err
		}
		out.ActiveChildren, err = store.CountActiveChildren(ctx, tx, ref)
		return err
	})
	if err != nil {
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
