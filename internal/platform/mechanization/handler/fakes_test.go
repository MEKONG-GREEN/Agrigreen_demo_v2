package handler_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// fakeScope tính phạm vi theo OrgRef của token (party đã có kiểm thử riêng cho cây phân cấp).
type fakeScope struct {
	mu    sync.Mutex
	byOrg map[uuid.UUID]authz.Scope
}

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, nil
	}
	if p.Has(port.RoleCGHSync) || p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.byOrg[p.OrgRef]; ok {
		return s, nil
	}
	return authz.Scope{}, nil
}

// fakeOrgs là sổ HTX giả; đổi được xã của HTX để thử "HTX chuyển tỉnh".
type fakeOrgs struct {
	mu   sync.Mutex
	orgs map[uuid.UUID]partyport.Org
}

func (f *fakeOrgs) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

func (f *fakeOrgs) put(o partyport.Org) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orgs[o.Ref] = o
}

func (f *fakeOrgs) move(ref uuid.UUID, commune string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.orgs[ref]
	o.AdminUnitCode = commune
	f.orgs[ref] = o
}

// fakeUnits: An Giang (89) có xã 30001, 30002; Thành phố Cần Thơ (92) có xã 31001.
type fakeUnits struct {
	units map[string]partyport.AdminUnit
}

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]partyport.AdminUnit{}}
	add := func(code, name string, lvl partyport.Level, parent string) {
		f.units[code] = partyport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
	}
	add("89", "An Giang", partyport.LevelProvince, "")
	add("30001", "Xã Mỹ Hòa", partyport.LevelCommune, "89")
	add("30002", "Xã Bình Thủy", partyport.LevelCommune, "89")
	add("92", "Thành phố Cần Thơ", partyport.LevelProvince, "")
	add("31001", "Xã Thới Lai", partyport.LevelCommune, "92")
	return f
}

func (f *fakeUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	u, ok := f.units[code]
	if !ok {
		return partyport.AdminUnit{}, partyport.ErrUnitNotFound
	}
	return u, nil
}

func (f *fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	out := []string{code}
	for c, u := range f.units {
		if u.ParentCode == code {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

// fakeCatalogs: khâu thí điểm lam_dat, thu_hoach; sau_thu_hoach KHÔNG thí điểm.
type fakeCatalogs struct {
	items map[string][]mdport.CatalogItem
}

func newFakeCatalogs() *fakeCatalogs {
	it := func(cat, code, name string, attrs map[string]string) mdport.CatalogItem {
		return mdport.CatalogItem{Catalog: cat, Code: code, Name: name, Attrs: attrs, Active: true}
	}
	return &fakeCatalogs{items: map[string][]mdport.CatalogItem{
		mdport.CatalogFarmStage: {
			it("farm_stage", "lam_dat", "Làm đất", map[string]string{"pilot": "true"}),
			it("farm_stage", "thu_hoach", "Thu hoạch", map[string]string{"pilot": "true"}),
			it("farm_stage", "sau_thu_hoach", "Sau thu hoạch", map[string]string{"pilot": "false"}),
		},
		mdport.CatalogMachineType: {
			it("machine_type", "MLD", "Máy làm đất", map[string]string{"farm_stage": "lam_dat"}),
			it("machine_type", "MGD", "Máy gặt đập liên hợp", map[string]string{"farm_stage": "thu_hoach"}),
			it("machine_type", "MSL", "Máy sấy lúa", map[string]string{"farm_stage": "sau_thu_hoach"}),
		},
		mdport.CatalogSeason: {
			it("season", "DX", "Đông Xuân", map[string]string{"start_month": "11", "end_month": "3"}),
			it("season", "HT", "Hè Thu", map[string]string{"start_month": "4", "end_month": "8"}),
		},
	}}
}

func (f *fakeCatalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	for _, it := range f.items[catalog] {
		if it.Code == code {
			return it, nil
		}
	}
	return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
}

func (f *fakeCatalogs) List(_ context.Context, catalog string) ([]mdport.CatalogItem, error) {
	return append([]mdport.CatalogItem(nil), f.items[catalog]...), nil
}

// fakeParams: phiên bản ĐÃ DUYỆT theo ngày hiệu lực — At trả bản mới nhất có hiệu lực tại at (mục 16).
type fakeParams struct {
	mu   sync.Mutex
	vals map[string][]params.ParamValue
}

func (f *fakeParams) approve(key, value, from string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, _ := time.ParseInLocation(time.DateOnly, from, time.FixedZone("ICT", 7*3600))
	f.vals[key] = append(f.vals[key], params.ParamValue{
		Key: key, Value: decimal.RequireFromString(value), Version: len(f.vals[key]) + 1, EffectiveFrom: t.UTC(), SourceDoc: "QĐ thử " + from,
	})
	sort.Slice(f.vals[key], func(i, j int) bool { return f.vals[key][i].EffectiveFrom.Before(f.vals[key][j].EffectiveFrom) })
}

func (f *fakeParams) At(_ context.Context, key string, at time.Time) (params.ParamValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *params.ParamValue
	for i, v := range f.vals[key] {
		if !v.EffectiveFrom.After(at) {
			best = &f.vals[key][i]
		}
	}
	if best == nil {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return *best, nil
}
