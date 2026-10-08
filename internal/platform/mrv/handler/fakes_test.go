package handler_test

import (
	"context"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeScopes giả party ScopeResolver: phạm vi theo org_ref của token.
type fakeScopes struct {
	mu     sync.Mutex
	scopes map[uuid.UUID]authz.Scope
}

func newFakeScopes() *fakeScopes { return &fakeScopes{scopes: map[uuid.UUID]authz.Scope{}} }

func (f *fakeScopes) set(org uuid.UUID, s authz.Scope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes[org] = s
}

func (f *fakeScopes) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scopes[p.OrgRef], nil
}

var _ partyport.ScopeResolver = (*fakeScopes)(nil)

// fakeConsent giả party ConsentChecker theo (tenant, chủ thể, mục đích).
type fakeConsent struct {
	mu      sync.Mutex
	granted map[string]bool
	calls   int
}

func newFakeConsent() *fakeConsent { return &fakeConsent{granted: map[string]bool{}} }

func (f *fakeConsent) grant(tid, subject uuid.UUID, purpose string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.granted[tid.String()+"|"+subject.String()+"|"+purpose] = true
}

func (f *fakeConsent) HasConsent(ctx context.Context, subject uuid.UUID, purpose string) (bool, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.granted[tid.String()+"|"+subject.String()+"|"+purpose], nil
}

var _ partyport.ConsentChecker = (*fakeConsent)(nil)

// fakeParams giả masterdata Params: tham số "đã duyệt" theo khóa kèm phiên bản.
type fakeParams struct {
	mu   sync.Mutex
	vals map[string]params.ParamValue
}

func newFakeParams() *fakeParams { return &fakeParams{vals: map[string]params.ParamValue{}} }

func (f *fakeParams) set(key, v string, version int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = params.ParamValue{Key: key, Value: decimal.RequireFromString(v), Version: version}
}

// setText đặt tham số CHUỖI (vd danh sách mã) "đã duyệt".
func (f *fakeParams) setText(key, text string, version int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = params.ParamValue{Key: key, Text: text, Version: version}
}

func (f *fakeParams) At(_ context.Context, key string, _ time.Time) (params.ParamValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return v, nil
}

var _ params.Params = (*fakeParams)(nil)
