package app

// Cổng đọc HTX cho module khác: thống kê theo đơn vị, HTX đang hoạt động (cooperativeport.Cooperatives — mặt
// tiền cooperative/app nối), marker HTX cho lớp bản đồ (partyport.CoopMarkers). Nút (mã, tên, đơn vị, trạng
// thái) đọc qua partyport.OrgDirectory, hồ sơ và xã viên ở pf_coop — ghép trong Go, không JOIN chéo schema.

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ partyport.CoopMarkers = (*Service)(nil)

// MaxMarkers là số điểm HTX tối đa một lần đọc khung nhìn.
const MaxMarkers = 1000

func (s *Service) dir() (partyport.OrgDirectory, error) {
	if s.Dir == nil {
		return nil, fmt.Errorf("%w (danh bạ tổ chức của pf/party)", port.ErrUnavailable)
	}
	return s.Dir, nil
}

func (s *Service) memberCounts(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]int64, error) {
	var out map[uuid.UUID]int64
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.MemberCounts(ctx, tx, coops)
		return e
	})
	return out, err
}

func orgRefs(orgs []partyport.Org) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.Ref)
	}
	return out
}

// CooperativesByUnit đếm HTX đang hoạt động và hộ thành viên đang mở theo mã đơn vị (G-02; cooperativeport.
// Cooperatives.ByUnit). units nil = mọi đơn vị; HTX không gắn đơn vị không được tính. Tenant lấy từ context.
func (s *Service) CooperativesByUnit(ctx context.Context, units []string) ([]port.UnitCooperatives, error) {
	if units != nil && len(units) == 0 {
		return []port.UnitCooperatives{}, nil
	}
	d, err := s.dir()
	if err != nil {
		return nil, err
	}
	orgs, err := d.OrgsOfKind(ctx, partyport.OrgCooperative, units, true)
	if err != nil {
		return nil, err
	}
	members, err := s.memberCounts(ctx, orgRefs(orgs))
	if err != nil {
		return nil, err
	}
	by := map[string]*port.UnitCooperatives{}
	for _, o := range orgs {
		if o.AdminUnitCode == "" {
			continue
		}
		u := by[o.AdminUnitCode]
		if u == nil {
			u = &port.UnitCooperatives{UnitCode: o.AdminUnitCode}
			by[o.AdminUnitCode] = u
		}
		u.Cooperatives++
		u.Households += members[o.Ref]
	}
	out := make([]port.UnitCooperatives, 0, len(by))
	for _, u := range by {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b port.UnitCooperatives) int {
		switch {
		case a.UnitCode < b.UnitCode:
			return -1
		case a.UnitCode > b.UnitCode:
			return 1
		}
		return 0
	})
	return out, nil
}

// ActiveCooperatives liệt kê HTX đang hoạt động kèm xã, vị trí hồ sơ và số hộ thành viên đang mở, sắp theo ref
// (cooperativeport.Cooperatives.Active — nguồn cung của mô phỏng chọn Hub). Tenant lấy từ context.
func (s *Service) ActiveCooperatives(ctx context.Context) ([]port.CooperativeSite, error) {
	d, err := s.dir()
	if err != nil {
		return nil, err
	}
	orgs, err := d.OrgsOfKind(ctx, partyport.OrgCooperative, nil, true)
	if err != nil {
		return nil, err
	}
	refs := orgRefs(orgs)
	var profiles map[uuid.UUID]store.Profile
	var members map[uuid.UUID]int64
	err = tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		if profiles, e = store.ProfilesByRefs(ctx, tx, refs); e != nil {
			return e
		}
		members, e = store.MemberCounts(ctx, tx, refs)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.CooperativeSite, 0, len(orgs))
	for _, o := range orgs {
		site := port.CooperativeSite{Ref: o.Ref, Code: o.Code, CommuneCode: o.AdminUnitCode, Members: members[o.Ref]}
		if p, ok := profiles[o.Ref]; ok {
			site.Lon, site.Lat = p.Lon, p.Lat
		}
		out = append(out, site)
	}
	return out, nil
}

// MarkersInBBox thực hiện partyport.CoopMarkers: điểm vị trí hồ sơ (pf_coop.cooperative.location) của HTX
// đang hoạt động trong khung nhìn — marker/dự phòng của lớp ranh giới HTX (GIS US-BND-02). Tenant lấy từ
// context; không kiểm phạm vi (bên gọi tự lọc).
func (s *Service) MarkersInBBox(ctx context.Context, minLon, minLat, maxLon, maxLat float64, limit int) ([]partyport.CoopMarker, error) {
	for _, v := range []float64{minLon, minLat, maxLon, maxLat} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, problem.Validation.WithField("bbox", "toạ độ không hợp lệ")
		}
	}
	if limit <= 0 || limit > MaxMarkers {
		limit = MaxMarkers
	}
	d, err := s.dir()
	if err != nil {
		return nil, err
	}
	var locs []store.Profile
	if err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		locs, e = store.LocationsInBBox(ctx, tx, minLon, minLat, maxLon, maxLat)
		return e
	}); err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, len(locs))
	for _, l := range locs {
		refs = append(refs, l.OrgRef)
	}
	orgs, err := d.OrgsByRefs(ctx, refs)
	if err != nil {
		return nil, err
	}
	var out []partyport.CoopMarker
	for _, l := range locs {
		o, ok := orgs[l.OrgRef]
		if !ok || !o.Active || o.Kind != partyport.OrgCooperative || l.Lat == nil || l.Lon == nil {
			continue
		}
		out = append(out, partyport.CoopMarker{Ref: o.Ref, Code: o.Code, Name: o.Name, CommuneCode: o.AdminUnitCode, Lon: *l.Lon, Lat: *l.Lat})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
