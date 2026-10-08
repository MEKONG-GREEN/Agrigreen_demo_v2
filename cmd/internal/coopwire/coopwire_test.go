package coopwire

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port/porttest"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// legacy là port cũ giả, dựng ngược từ seed của bộ kiểm hợp đồng (kiểu cũ ← kiểu mới): mặt tiền trên
// coopwire phải trả đúng như bản giả trong bộ nhớ (bảng so sánh cổng cũ ↔ mới, docs/design/cooperative.md mục 14).
type legacy struct{ m porttest.Memory }

func (l legacy) CooperativesByUnit(ctx context.Context, units []string) ([]partyport.UnitCooperatives, error) {
	rows, err := l.m.ByUnit(ctx, units)
	out := make([]partyport.UnitCooperatives, len(rows))
	for i, r := range rows {
		out[i] = partyport.UnitCooperatives(r)
	}
	return out, err
}

func (l legacy) ActiveCooperatives(ctx context.Context) ([]partyport.CooperativeSite, error) {
	rows, err := l.m.Active(ctx)
	out := make([]partyport.CooperativeSite, len(rows))
	for i, r := range rows {
		out[i] = partyport.CooperativeSite(r)
	}
	return out, err
}

func (l legacy) CoopCentroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]geoport.CoopCentroid, error) {
	m, err := l.m.Centroids(ctx, coops)
	out := map[uuid.UUID]geoport.CoopCentroid{}
	for k, v := range m {
		out[k] = geoport.CoopCentroid(v)
	}
	return out, err
}

func (l legacy) Membership(ctx context.Context, ref uuid.UUID) (partyport.HouseholdMembership, error) {
	m, err := l.m.Membership(ctx, ref)
	if errors.Is(err, coopport.ErrHouseholdNotFound) {
		return partyport.HouseholdMembership{}, partyport.ErrHouseholdNotFound
	}
	return partyport.HouseholdMembership(m), err
}

func (l legacy) HouseholdsInCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	return l.m.InCommunes(ctx, communes)
}

func (l legacy) Get(ctx context.Context, ref uuid.UUID) (geoport.Plot, error) {
	p, err := l.m.Get(ctx, ref)
	if errors.Is(err, coopport.ErrPlotNotFound) {
		return geoport.Plot{}, geoport.ErrPlotNotFound
	}
	return geoport.Plot(p), err
}

func (l legacy) DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) {
	if _, err := l.Get(ctx, ref); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromInt(12), nil
}

func (l legacy) PlotFacts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]geoport.Plot, error) {
	m, err := l.m.Facts(ctx, refs)
	out := map[uuid.UUID]geoport.Plot{}
	for k, v := range m {
		out[k] = geoport.Plot(v)
	}
	return out, err
}

func (l legacy) DigitizedByCommune(ctx context.Context, communes []string) ([]partyport.PlotStat, error) {
	rows, err := l.m.DigitizedByCommune(ctx, communes)
	out := make([]partyport.PlotStat, len(rows))
	for i, r := range rows {
		out[i] = partyport.PlotStat(r)
	}
	return out, err
}

func (l legacy) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]partyport.PlotStat, error) {
	m, err := l.m.DigitizedByCooperative(ctx, coops)
	out := map[uuid.UUID]partyport.PlotStat{}
	for k, v := range m {
		out[k] = partyport.PlotStat(v)
	}
	return out, err
}

func (l legacy) ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]ccport.CoopActivity, error) {
	m, err := l.m.ActivityByCooperative(ctx, coops)
	out := map[uuid.UUID]ccport.CoopActivity{}
	for k, v := range m {
		out[k] = ccport.CoopActivity(v)
	}
	return out, err
}

func (l legacy) SeasonAreas(ctx context.Context, coop uuid.UUID, code string, year int) ([]ccport.CoopSeasonArea, error) {
	rows, err := l.m.Areas(ctx, coop, code, year)
	out := make([]ccport.CoopSeasonArea, len(rows))
	for i, r := range rows {
		out[i] = ccport.CoopSeasonArea(r)
	}
	return out, err
}

func (l legacy) SeasonSupply(ctx context.Context, year int) ([]ccport.CoopSeasonSupply, error) {
	rows, err := l.m.Supply(ctx, year)
	out := make([]ccport.CoopSeasonSupply, len(rows))
	for i, r := range rows {
		out[i] = ccport.CoopSeasonSupply(r)
	}
	return out, err
}

func (l legacy) Production(ctx context.Context, q cropcyclesrc.ProductionQuery) (cropcyclesrc.ProductionData, error) {
	d, err := l.m.Production(ctx, coopport.ProductionQuery(q))
	out := cropcyclesrc.ProductionData{Totals: cropcyclesrc.ProductionFigures(d.Totals)}
	for _, r := range d.Rows {
		out.Rows = append(out.Rows, cropcyclesrc.ProductionRow{CooperativeRef: r.CooperativeRef, SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear,
			VarietyCode: r.VarietyCode, ProductionFigures: cropcyclesrc.ProductionFigures(r.ProductionFigures)})
	}
	return out, err
}

func wired(s porttest.Seed) *cooperative.Module {
	l := legacy{porttest.Memory{S: s}}
	return cooperative.New(Sources(Legacy{CoopStats: l, CoopSites: l, Centroids: l, Households: l, PlotHH: l, Plots: l, PlotFacts: l,
		PlotStats: l, SeasonStats: l, Supply: l, Reports: l}))
}

// Mặt tiền pf/cooperative trên port cũ (store tạm W3.2) qua cùng bộ kiểm hợp đồng với bản giả.
func TestFacadeOverLegacyPortsContract(t *testing.T) {
	porttest.RunContract(t, tenant.WithID, func(_ *testing.T, s porttest.Seed) porttest.Ports {
		m := wired(s)
		return porttest.Ports{Cooperatives: m.Cooperatives(), Households: m.Households(), Plots: m.Plots(), Seasons: m.Seasons(),
			Diary: m.Diary(), Resolve: m.Resolve()}
	})
}

// Lỗi "không có" của port cũ được đổi sang lỗi của cooperativeport (bên tiêu thụ chỉ so lỗi mới).
func TestErrorMapping(t *testing.T) {
	s := porttest.SampleSeed()
	m := wired(s)
	ctx := context.Background()
	if d, err := m.Plots().DistanceM(ctx, s.Plots[0].Ref, decimal.Zero, decimal.Zero); err != nil || !d.Equal(decimal.NewFromInt(12)) {
		t.Fatalf("DistanceM = %v, %v", d, err)
	}
	if _, err := m.Plots().Get(ctx, uuid.New()); !errors.Is(err, coopport.ErrPlotNotFound) || errors.Is(err, geoport.ErrPlotNotFound) {
		t.Fatalf("Get err = %v", err)
	}
	if _, err := m.Households().Membership(ctx, uuid.New()); !errors.Is(err, coopport.ErrHouseholdNotFound) {
		t.Fatalf("Membership err = %v", err)
	}
}

// Nhóm thiếu port cũ → nil trong Sources (mặt tiền trả ErrUnavailable); trường thiếu trong nhóm có
// port khác → bộ chuyển trả ErrUnavailable. HarvestDates, Diary chưa có nguồn cũ (W3.3).
func TestPartialLegacy(t *testing.T) {
	s := porttest.SampleSeed()
	l := legacy{porttest.Memory{S: s}}
	src := Sources(Legacy{Plots: l})
	if src.Cooperatives != nil || src.Households != nil || src.Seasons != nil || src.Diary != nil || src.Plots == nil {
		t.Fatalf("Sources = %+v", src)
	}
	m := cooperative.New(src)
	ctx := context.Background()
	if _, err := m.Plots().Facts(ctx, []uuid.UUID{s.Plots[0].Ref}); !errors.Is(err, coopport.ErrUnavailable) {
		t.Fatalf("Facts không có PlotFacts: %v", err)
	}
	if _, err := m.Seasons().Supply(ctx, 2026); !errors.Is(err, coopport.ErrUnavailable) {
		t.Fatalf("Supply: %v", err)
	}
	full := wired(s)
	if _, err := full.Seasons().HarvestDates(ctx, []uuid.UUID{s.Plots[0].Ref}); !errors.Is(err, coopport.ErrUnavailable) {
		t.Fatalf("HarvestDates: %v", err)
	}
	if _, err := full.Diary().Get(ctx, uuid.New()); !errors.Is(err, coopport.ErrUnavailable) {
		t.Fatalf("Diary: %v", err)
	}
	if hh, err := full.Households().InCommunes(ctx, nil); err != nil || !slices.Contains(hh, s.Members[2].Ref) {
		t.Fatalf("InCommunes(nil) = %v, %v", hh, err)
	}
}
