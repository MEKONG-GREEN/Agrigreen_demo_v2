package handler_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// fakeScope là ScopeResolver giả: phạm vi theo org_ref của token (thay pf/party).
type fakeScope struct {
	mu     sync.Mutex
	scopes map[uuid.UUID]authz.Scope
}

func newFakeScope() *fakeScope { return &fakeScope{scopes: map[uuid.UUID]authz.Scope{}} }

func (f *fakeScope) set(org uuid.UUID, s authz.Scope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes[org] = s
}

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scopes[p.OrgRef], nil
}

// fakeUnits: tỉnh 89 (xã 30001, 30002), tỉnh 92 (xã 31001).
type fakeUnits struct {
	units map[string]partyport.AdminUnit
}

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]partyport.AdminUnit{}}
	add := func(code string, lvl partyport.Level, parent string) {
		f.units[code] = partyport.AdminUnit{Code: code, Name: code, Level: lvl, ParentCode: parent}
	}
	add("89", partyport.LevelProvince, "")
	add("30001", partyport.LevelCommune, "89")
	add("30002", partyport.LevelCommune, "89")
	add("92", partyport.LevelProvince, "")
	add("31001", partyport.LevelCommune, "92")
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

// fakeCatalogs: vài mục danh mục pf/masterdata.
type fakeCatalogs struct{ items map[string]mdport.CatalogItem }

func newFakeCatalogs() *fakeCatalogs {
	f := &fakeCatalogs{items: map[string]mdport.CatalogItem{}}
	add := func(cat, code string, active bool) {
		f.items[cat+":"+code] = mdport.CatalogItem{Catalog: cat, Code: code, Name: code, Active: active}
	}
	add("rice_variety", "OM18", true)
	add("farm_stage", "sowing", true)
	add("pest", "ray_nau", true)
	add("rice_variety", "IR50404", false) // đã ngừng dùng
	return f
}

func (f *fakeCatalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	it, ok := f.items[catalog+":"+code]
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
