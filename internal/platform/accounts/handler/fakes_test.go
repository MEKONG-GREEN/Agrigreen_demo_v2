package handler_test

import (
	"context"
	"sync"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeParty thực hiện party/port.Orgs và ScopeResolver theo đúng quy tắc ADR-0009
// (pf/party thật có kiểm thử riêng). Mã đơn vị theo danh mục chính thức 2025:
// tỉnh 91 An Giang (xã/phường 30292, 30301), tỉnh 92 Cần Thơ (31120).
type fakeParty struct {
	mu   sync.Mutex
	orgs map[uuid.UUID]map[uuid.UUID]partyport.Org // tenant → ref → org
}

var communesOf = map[string][]string{"91": {"30292", "30301"}, "92": {"31120"}}

func newFakeParty() *fakeParty { return &fakeParty{orgs: map[uuid.UUID]map[uuid.UUID]partyport.Org{}} }

func (f *fakeParty) add(tid uuid.UUID, kind partyport.OrgKind, unit string) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.orgs[tid] == nil {
		f.orgs[tid] = map[uuid.UUID]partyport.Org{}
	}
	ref := uuid.New()
	f.orgs[tid][ref] = partyport.Org{Ref: ref, Kind: kind, Code: string(kind) + "-" + ref.String()[:6], Name: "Tổ chức thử", AdminUnitCode: unit, Active: true}
	return ref
}

func (f *fakeParty) setActive(tid, ref uuid.UUID, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.orgs[tid][ref]
	o.Active = on
	f.orgs[tid][ref] = o
}

func (f *fakeParty) Get(ctx context.Context, ref uuid.UUID) (partyport.Org, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return partyport.Org{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[tid][ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return o, nil
}

func (f *fakeParty) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, nil
	}
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	if p.OrgRef == uuid.Nil {
		return authz.Scope{}, nil
	}
	o, err := f.Get(ctx, p.OrgRef)
	if err != nil {
		return authz.Scope{}, nil
	}
	switch o.Kind {
	case partyport.OrgTTKNNational, partyport.OrgDCRD, partyport.OrgMKG:
		return authz.Scope{All: true}, nil
	case partyport.OrgTTKNProvince:
		return authz.Scope{Units: append([]string{o.AdminUnitCode}, communesOf[o.AdminUnitCode]...)}, nil
	}
	return authz.Scope{Units: []string{o.AdminUnitCode}}, nil
}

var (
	_ partyport.Orgs          = (*fakeParty)(nil)
	_ partyport.ScopeResolver = (*fakeParty)(nil)
)
