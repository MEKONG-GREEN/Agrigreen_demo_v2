package handler_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// fakeScope thay party.ScopeResolver: phạm vi theo OrgRef của người gọi.
type fakeScope struct {
	mu     sync.Mutex
	scopes map[uuid.UUID]authz.Scope
}

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scopes[p.OrgRef], nil
}

// fakePlots thay cooperative/port.Plots (W3.2): thửa theo ref, khoảng cách GPS cấu hình được.
type fakePlots struct {
	mu    sync.Mutex
	plots map[uuid.UUID]coopport.Plot
	dist  map[uuid.UUID]decimal.Decimal
}

func (f *fakePlots) put(p coopport.Plot, distM int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plots[p.Ref] = p
	f.dist[p.Ref] = decimal.NewFromInt(distM)
}

func (f *fakePlots) Get(_ context.Context, ref uuid.UUID) (coopport.Plot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.plots[ref]
	if !ok {
		return coopport.Plot{}, coopport.ErrPlotNotFound
	}
	return p, nil
}

func (f *fakePlots) DistanceM(_ context.Context, ref uuid.UUID, _, _ decimal.Decimal) (decimal.Decimal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.dist[ref]
	if !ok {
		return decimal.Zero, coopport.ErrPlotNotFound
	}
	return d, nil
}

// DigitizedByCommune, DigitizedByCooperative: không dùng ở cropcycle.
func (f *fakePlots) DigitizedByCommune(context.Context, []string) ([]coopport.PlotStat, error) {
	return nil, nil
}

func (f *fakePlots) DigitizedByCooperative(context.Context, []uuid.UUID) (map[uuid.UUID]coopport.PlotStat, error) {
	return nil, nil
}

// fakeOrgs thay party/port.Orgs.
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

func (f *fakeOrgs) setActive(ref uuid.UUID, active bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.orgs[ref]
	o.Active = active
	f.orgs[ref] = o
}

// fakeCatalogs thay masterdata/port.Catalogs.
type fakeCatalogs struct{ items map[string]mdport.CatalogItem }

func newFakeCatalogs() *fakeCatalogs {
	f := &fakeCatalogs{items: map[string]mdport.CatalogItem{}}
	add := func(cat, code string, attrs map[string]string) {
		f.items[cat+"/"+code] = mdport.CatalogItem{Catalog: cat, Code: code, Name: code, Attrs: attrs, Active: true}
	}
	add(mdport.CatalogRiceVariety, "OM5451", map[string]string{"growth_days": "95", "expected_yield_t_per_ha": "6.75"})
	add(mdport.CatalogRiceVariety, "ST25", map[string]string{"growth_days": "105", "expected_yield_t_per_ha": "5.5"})
	add(mdport.CatalogSeason, "DX", nil)
	add(mdport.CatalogSeason, "HT", nil)
	add(mdport.CatalogMachineType, "MGS", map[string]string{"farm_stage": "gieo_sa"})
	add(mdport.CatalogMachineType, "MPT", map[string]string{"farm_stage": "cham_soc"})
	f.items[mdport.CatalogMachineType+"/OLD"] = mdport.CatalogItem{Catalog: mdport.CatalogMachineType, Code: "OLD", Active: false}
	return f
}

func (f *fakeCatalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	it, ok := f.items[catalog+"/"+code]
	if !ok {
		return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
	}
	return it, nil
}

func (f *fakeCatalogs) List(_ context.Context, catalog string) ([]mdport.CatalogItem, error) {
	var out []mdport.CatalogItem
	for _, it := range f.items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

// fakeParams trả tham số đã "phê duyệt" theo khóa.
type fakeParams struct {
	mu   sync.Mutex
	vals map[string]decimal.Decimal
}

func (f *fakeParams) set(key, v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = decimal.RequireFromString(v)
}

func (f *fakeParams) At(_ context.Context, key string, _ time.Time) (params.ParamValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: v, Version: 1}, nil
}

// fakeFarmers thay port.FarmerHouseholds (liên kết tài khoản ↔ nông hộ của pf/accounts).
type fakeFarmers struct {
	mu    sync.Mutex
	links map[uuid.UUID][]uuid.UUID
}

func (f *fakeFarmers) link(actor uuid.UUID, hh ...uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[actor] = hh
}

func (f *fakeFarmers) HouseholdsOf(_ context.Context, actor uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.links[actor], nil
}
