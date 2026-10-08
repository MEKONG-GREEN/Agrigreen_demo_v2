package handler_test

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
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

// fakeSeasons thay cooperative/port.Seasons (W3.3: vụ ở pf/cooperative) — chỉ Codes cho collection_volume.
type fakeSeasons struct {
	coopport.Seasons
	mu    sync.Mutex
	codes map[uuid.UUID]coopport.SeasonKey
}

// open ghi một vụ DX 2026 giả của thửa và trả season_ref (thay mở vụ qua API vụ mùa).
func (f *fakeSeasons) open(uuid.UUID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := ids.New()
	f.codes[ref] = coopport.SeasonKey{SeasonCode: "DX", SeasonYear: 2026}
	return ref.String()
}

func (f *fakeSeasons) Codes(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.SeasonKey, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]coopport.SeasonKey{}
	for _, r := range refs {
		if k, ok := f.codes[r]; ok {
			out[r] = k
		}
	}
	return out, nil
}

// Facts thay cooperativeport.Plots.Facts (tenant bắt buộc trong ctx).
func (f *fakePlots) Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.Plot, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]coopport.Plot{}
	for _, r := range refs {
		if p, ok := f.plots[r]; ok {
			out[r] = p
		}
	}
	return out, nil
}
