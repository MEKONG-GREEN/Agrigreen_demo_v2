package handler_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// fakeUnits: tỉnh 89 (xã 30001, 30002), tỉnh 92 (xã 31001).
type fakeUnits struct {
	units map[string]partyport.AdminUnit
}

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]partyport.AdminUnit{}}
	add := func(code string, lvl partyport.Level, parent string) {
		f.units[code] = partyport.AdminUnit{Code: code, Name: "ĐV " + code, Level: lvl, ParentCode: parent}
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

// fakeParams: tham số đã duyệt, đổi được trong kiểm thử.
type fakeParams struct {
	mu   sync.Mutex
	v    map[string]decimal.Decimal
	text map[string]string // tham số chuỗi (vd extension.work_schedule_approver_levels)
}

// setText đặt tham số chuỗi; "" → xóa.
func (f *fakeParams) setText(key, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.text == nil {
		f.text = map[string]string{}
	}
	if val == "" {
		delete(f.text, key)
		return
	}
	f.text[key] = val
}

func (f *fakeParams) set(key, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if val == "" {
		delete(f.v, key)
		return
	}
	f.v[key] = decimal.RequireFromString(val)
}

func (f *fakeParams) At(_ context.Context, key string, at time.Time) (params.ParamValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.text[key]; ok {
		return params.ParamValue{Key: key, Text: t, Version: 1, EffectiveFrom: at}, nil
	}
	v, ok := f.v[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: v, Unit: "giờ", Version: 1, EffectiveFrom: at}, nil
}

// fakeCatalogs: một phần danh mục seed.
type fakeCatalogs struct{}

var catalogItems = map[string]bool{
	"rice_variety/OM5451": true, "rice_variety/ST25": true, "season/HT": true, "season/TD": true,
	"pest/ray_nau": true, "pest/dao_on": true,
}

func (fakeCatalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	if !catalogItems[catalog+"/"+code] {
		return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
	}
	return mdport.CatalogItem{Catalog: catalog, Code: code, Name: code, Active: true}, nil
}

func (fakeCatalogs) List(context.Context, string) ([]mdport.CatalogItem, error) { return nil, nil }

// orgTree là cây tổ chức giả + bộ tính phạm vi theo org_ref (thay pf/party).
type orgTree struct {
	orgs  map[uuid.UUID]partyport.Org
	scope map[uuid.UUID]authz.Scope

	national, p89, p92, t30001, t30002, t31001, c1, c2, c3 uuid.UUID
}

func newOrgTree() *orgTree {
	o := &orgTree{orgs: map[uuid.UUID]partyport.Org{}, scope: map[uuid.UUID]authz.Scope{}}
	add := func(kind partyport.OrgKind, unit string, parent uuid.UUID, sc authz.Scope) uuid.UUID {
		ref := ids.New()
		o.orgs[ref] = partyport.Org{Ref: ref, Kind: kind, Code: string(kind) + unit, Name: "Tổ chức " + unit, AdminUnitCode: unit, ParentRef: parent, Active: true}
		o.scope[ref] = sc
		return ref
	}
	o.national = add(partyport.OrgTTKNNational, "", uuid.Nil, authz.Scope{All: true})
	o.p89 = add(partyport.OrgTTKNProvince, "89", o.national, authz.Scope{Units: []string{"89", "30001", "30002"}})
	o.p92 = add(partyport.OrgTTKNProvince, "92", o.national, authz.Scope{Units: []string{"92", "31001"}})
	o.t30001 = add(partyport.OrgExtensionTeam, "30001", o.p89, authz.Scope{Units: []string{"30001"}})
	o.t30002 = add(partyport.OrgExtensionTeam, "30002", o.p89, authz.Scope{Units: []string{"30002"}})
	o.t31001 = add(partyport.OrgExtensionTeam, "31001", o.p92, authz.Scope{Units: []string{"31001"}})
	o.c1 = add(partyport.OrgCooperative, "30001", o.t30001, authz.Scope{Units: []string{"30001"}})
	o.c2 = add(partyport.OrgCooperative, "30002", o.t30002, authz.Scope{Units: []string{"30002"}})
	o.c3 = add(partyport.OrgCooperative, "31001", o.t31001, authz.Scope{Units: []string{"31001"}})
	return o
}

func (o *orgTree) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	org, ok := o.orgs[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return org, nil
}

func (o *orgTree) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, nil
	}
	return o.scope[p.OrgRef], nil
}

var (
	_ partyport.Orgs          = (*orgTree)(nil)
	_ partyport.ScopeResolver = (*orgTree)(nil)
	_ partyport.AdminUnits    = (*fakeUnits)(nil)
	_ params.Params           = (*fakeParams)(nil)
	_ mdport.Catalogs         = fakeCatalogs{}
)
