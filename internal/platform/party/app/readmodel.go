package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ port.CooperativeStats = (*Registry)(nil)

// CooperativesByUnit thực hiện port.CooperativeStats (G-02). Tenant lấy từ context.
func (r *Registry) CooperativesByUnit(ctx context.Context, units []string) ([]port.UnitCooperatives, error) {
	var out []port.UnitCooperatives
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.CooperativesByUnit(ctx, tx, units == nil, units)
		return e
	})
	return out, err
}

// narrowScope thu hẹp phạm vi đọc theo đơn vị lọc (mã tỉnh hoặc xã, G-03): chỉ
// giữ đơn vị vừa thuộc đơn vị lọc vừa thuộc phạm vi của người gọi.
func (r *Registry) narrowScope(ctx context.Context, sc authz.Scope, unit string) (authz.Scope, error) {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return sc, nil
	}
	if r.Units == nil {
		return sc, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	codes, err := r.Units.Descendants(ctx, unit, r.now())
	if errors.Is(err, port.ErrUnitNotFound) {
		return sc, problem.Validation.WithField("admin_unit", "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return sc, err
	}
	if sc.All {
		return authz.Scope{Units: codes}, nil
	}
	var keep []string
	for _, c := range codes {
		if slices.Contains(sc.Units, c) {
			keep = append(keep, c)
		}
	}
	return authz.Scope{Units: keep}, nil
}

// CoopExtra là dữ liệu đọc bổ sung của một HTX trong danh sách (G-03).
type CoopExtra struct {
	ProvinceCode     string
	CommuneName      string
	ProvinceName     string
	MemberHouseholds int64
	// DigitizedPlots: số thửa đã số hoá (pf/geo); nil khi chưa nối pf/geo hoặc tra lỗi.
	DigitizedPlots *int64
}

// CoopExtras tra tên xã/tỉnh (pf/masterdata), số hộ thành viên và số thửa đã số
// hoá (pf/geo) cho các HTX đã đọc. Tên thiếu không chặn danh sách.
func (r *Registry) CoopExtras(ctx context.Context, items []store.Cooperative) (map[uuid.UUID]CoopExtra, error) {
	out := make(map[uuid.UUID]CoopExtra, len(items))
	if len(items) == 0 {
		return out, nil
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.Ref)
	}
	var members map[uuid.UUID]int64
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		members, e = store.MemberCounts(ctx, tx, refs)
		return e
	}); err != nil {
		return nil, err
	}
	var plots map[uuid.UUID]int64
	if r.Plots != nil {
		if st, err := r.Plots.DigitizedByCooperative(ctx, refs); err == nil {
			plots = map[uuid.UUID]int64{}
			for k, v := range st {
				plots[k] = v.Plots
			}
		}
	}
	names := map[string]port.AdminUnit{}
	unit := func(code string) *port.AdminUnit {
		if code == "" || r.Units == nil {
			return nil
		}
		if u, ok := names[code]; ok {
			return &u
		}
		u, err := r.Units.Get(ctx, code, r.now())
		if err != nil {
			return nil
		}
		names[code] = u
		return &u
	}
	for _, it := range items {
		e := CoopExtra{MemberHouseholds: members[it.Ref]}
		if plots != nil {
			n := plots[it.Ref]
			e.DigitizedPlots = &n
		}
		if u := unit(it.AdminUnitCode); u != nil {
			switch u.Level {
			case port.LevelProvince:
				e.ProvinceCode, e.ProvinceName = u.Code, u.Name
			default:
				e.CommuneName, e.ProvinceCode = u.Name, u.ParentCode
				if pu := unit(u.ParentCode); pu != nil {
					e.ProvinceName = pu.Name
				}
			}
		}
		out[it.Ref] = e
	}
	return out, nil
}

// ConsentStates đọc trạng thái đồng ý hiện tại theo mục đích của các nông hộ (G-04).
func (r *Registry) ConsentStates(ctx context.Context, subjects []uuid.UUID) (map[uuid.UUID][]store.ConsentState, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	var out map[uuid.UUID][]store.ConsentState
	err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ConsentStates(ctx, tx, subjects)
		return e
	})
	return out, err
}

// AdminListQuery là tham số liệt kê tổ chức trên Admin API (G-08).
type AdminListQuery struct {
	Kind            string
	ParentRef       *uuid.UUID
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// AdminListOrgs liệt kê cây tổ chức cho quản trị (vai quản trị dữ liệu chủ):
// quản trị toàn quốc thấy mọi tổ chức; quản trị tỉnh thấy tổ chức trong tỉnh.
func (r *Registry) AdminListOrgs(ctx context.Context, q AdminListQuery) (OrgPage, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgPage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Kind != "" && !domain.KnownKind(port.OrgKind(q.Kind)) {
		return OrgPage{}, problem.Validation.WithField("kind", domain.ErrKind.Error())
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil || sc.Empty() {
		return OrgPage{Items: []port.Org{}}, err
	}
	var items []port.Org
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListOrgsAdmin(ctx, tx, store.AdminOrgFilter{
			OrgFilter: store.OrgFilter{All: sc.All, Units: sc.Units, Kind: q.Kind, IncludeInactive: q.IncludeInactive, After: q.After, Limit: q.Limit + 1},
			ParentRef: q.ParentRef,
		})
		return e
	})
	if err != nil {
		return OrgPage{}, err
	}
	pg := OrgPage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].Ref
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []port.Org{}
	}
	return pg, nil
}
