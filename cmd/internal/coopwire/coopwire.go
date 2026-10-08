// Package coopwire là bộ chuyển lắp ráp dùng chung (cmd/api-platform, cmd/worker, cmd/api-public)
// của W3.2: dựng nguồn của mặt tiền pf/cooperative (cooperative.Sources — store tạm) từ port cũ
// của pf/party, pf/geo, pf/cropcycle. Chỉ đổi dạng dữ liệu và lỗi; không có logic nghiệp vụ.
//
// Vì sao ở gốc lắp ráp mà không trong module: nếu pf/cooperative import port cũ, nó nằm trong chu
// trình pf (party ↔ geo ↔ … ↔ mechanization) ngay khi bên tiêu thụ đổi sang cooperativeport —
// agmodgraph báo cạnh chu trình mới (docs/design/cooperative.md mục 3, ghi chú W3.2). W3.3 thay
// gói này bằng store SQL của pf/cooperative rồi xóa nó.
package coopwire

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
)

// Legacy là port cũ; trường nil = nguồn tương ứng chưa nối (cổng mới trả coopport.ErrUnavailable).
type Legacy struct {
	CoopStats   partyport.CooperativeStats
	CoopSites   partyport.CooperativeSites
	Centroids   geoport.CoopCentroids
	Households  partyport.Households
	PlotHH      geoport.PlotHouseholds
	Plots       geoport.Plots
	PlotFacts   geoport.PlotFacts
	PlotStats   partyport.PlotStats
	SeasonStats ccport.Stats
	Supply      ccport.SupplyStats
	Reports     cropcyclesrc.ReportData
}

// Sources dựng nguồn của pf/cooperative. Nhóm nào không có port cũ nào thì để nil.
func Sources(l Legacy) cooperative.Sources {
	var s cooperative.Sources
	if l.CoopStats != nil || l.CoopSites != nil || l.Centroids != nil {
		s.Cooperatives = Cooperatives{Stats: l.CoopStats, Sites: l.CoopSites, Areas: l.Centroids}
	}
	if l.Households != nil || l.PlotHH != nil {
		s.Households = Households{Members: l.Households, PlotHH: l.PlotHH}
	}
	if l.Plots != nil || l.PlotFacts != nil || l.PlotStats != nil {
		s.Plots = Plots{Plots: l.Plots, PlotFacts: l.PlotFacts, Stats: l.PlotStats}
	}
	if l.SeasonStats != nil || l.Supply != nil || l.Reports != nil {
		s.Seasons = Seasons{Stats: l.SeasonStats, SupplyStats: l.Supply, Reports: l.Reports}
	}
	return s // Diary: chưa có nguồn cũ (W3.3)
}

func unavailable(what string) error { return errors.Join(coopport.ErrUnavailable, errors.New(what)) }

// ---------------------------------------------------------------------------
// Cooperatives ← partyport.CooperativeStats + CooperativeSites, geoport.CoopCentroids
// ---------------------------------------------------------------------------

// Cooperatives thực hiện coopport.Cooperatives.
type Cooperatives struct {
	Stats partyport.CooperativeStats
	Sites partyport.CooperativeSites
	Areas geoport.CoopCentroids // tâm vùng HTX (pf_geo.cooperative_area)
}

// ByUnit thực hiện coopport.Cooperatives.
func (c Cooperatives) ByUnit(ctx context.Context, units []string) ([]coopport.UnitCooperatives, error) {
	if c.Stats == nil {
		return nil, unavailable("pf/party CooperativeStats")
	}
	rows, err := c.Stats.CooperativesByUnit(ctx, units)
	if err != nil {
		return nil, err
	}
	out := make([]coopport.UnitCooperatives, len(rows))
	for i, r := range rows {
		out[i] = coopport.UnitCooperatives(r)
	}
	return out, nil
}

// Active thực hiện coopport.Cooperatives.
func (c Cooperatives) Active(ctx context.Context) ([]coopport.CooperativeSite, error) {
	if c.Sites == nil {
		return nil, unavailable("pf/party CooperativeSites")
	}
	rows, err := c.Sites.ActiveCooperatives(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]coopport.CooperativeSite, len(rows))
	for i, r := range rows {
		out[i] = coopport.CooperativeSite(r)
	}
	return out, nil
}

// Centroids thực hiện coopport.Cooperatives.
func (c Cooperatives) Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]coopport.CoopCentroid, error) {
	if c.Areas == nil {
		return nil, unavailable("pf/geo CoopCentroids")
	}
	m, err := c.Areas.CoopCentroids(ctx, coops)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]coopport.CoopCentroid, len(m))
	for k, v := range m {
		out[k] = coopport.CoopCentroid(v)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Households ← partyport.Households, geoport.PlotHouseholds
// ---------------------------------------------------------------------------

// Households thực hiện coopport.Households.
type Households struct {
	Members partyport.Households
	PlotHH  geoport.PlotHouseholds
}

// Membership thực hiện coopport.Households.
func (h Households) Membership(ctx context.Context, ref uuid.UUID) (coopport.HouseholdMembership, error) {
	if h.Members == nil {
		return coopport.HouseholdMembership{}, unavailable("pf/party Households")
	}
	m, err := h.Members.Membership(ctx, ref)
	if errors.Is(err, partyport.ErrHouseholdNotFound) {
		return coopport.HouseholdMembership{}, coopport.ErrHouseholdNotFound
	}
	if err != nil {
		return coopport.HouseholdMembership{}, err
	}
	return coopport.HouseholdMembership(m), nil
}

// InCommunes thực hiện coopport.Households.
func (h Households) InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	if h.PlotHH == nil {
		return nil, unavailable("pf/geo PlotHouseholds")
	}
	return h.PlotHH.HouseholdsInCommunes(ctx, communes)
}

// ---------------------------------------------------------------------------
// Plots ← geoport.Plots + PlotFacts + PlotStats
// ---------------------------------------------------------------------------

// Plots thực hiện coopport.Plots.
type Plots struct {
	Plots     geoport.Plots
	PlotFacts geoport.PlotFacts
	Stats     partyport.PlotStats
}

// Get thực hiện coopport.Plots.
func (p Plots) Get(ctx context.Context, ref uuid.UUID) (coopport.Plot, error) {
	if p.Plots == nil {
		return coopport.Plot{}, unavailable("pf/geo Plots")
	}
	pl, err := p.Plots.Get(ctx, ref)
	if errors.Is(err, geoport.ErrPlotNotFound) {
		return coopport.Plot{}, coopport.ErrPlotNotFound
	}
	if err != nil {
		return coopport.Plot{}, err
	}
	return coopport.Plot(pl), nil
}

// DistanceM thực hiện coopport.Plots.
func (p Plots) DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) {
	if p.Plots == nil {
		return decimal.Zero, unavailable("pf/geo Plots")
	}
	d, err := p.Plots.DistanceM(ctx, ref, lon, lat)
	if errors.Is(err, geoport.ErrPlotNotFound) {
		return decimal.Zero, coopport.ErrPlotNotFound
	}
	return d, err
}

// Facts thực hiện coopport.Plots.
func (p Plots) Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.Plot, error) {
	if p.PlotFacts == nil {
		return nil, unavailable("pf/geo PlotFacts")
	}
	m, err := p.PlotFacts.PlotFacts(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]coopport.Plot, len(m))
	for k, v := range m {
		out[k] = coopport.Plot(v)
	}
	return out, nil
}

// DigitizedByCommune thực hiện coopport.Plots.
func (p Plots) DigitizedByCommune(ctx context.Context, communes []string) ([]coopport.PlotStat, error) {
	if p.Stats == nil {
		return nil, unavailable("pf/geo PlotStats")
	}
	rows, err := p.Stats.DigitizedByCommune(ctx, communes)
	if err != nil {
		return nil, err
	}
	out := make([]coopport.PlotStat, len(rows))
	for i, r := range rows {
		out[i] = coopport.PlotStat(r)
	}
	return out, nil
}

// DigitizedByCooperative thực hiện coopport.Plots.
func (p Plots) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]coopport.PlotStat, error) {
	if p.Stats == nil {
		return nil, unavailable("pf/geo PlotStats")
	}
	m, err := p.Stats.DigitizedByCooperative(ctx, coops)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]coopport.PlotStat, len(m))
	for k, v := range m {
		out[k] = coopport.PlotStat(v)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Seasons ← ccport.Stats + SupplyStats + ReportData
// ---------------------------------------------------------------------------

// Seasons thực hiện coopport.Seasons.
type Seasons struct {
	Stats       ccport.Stats
	SupplyStats ccport.SupplyStats
	Reports     cropcyclesrc.ReportData
}

// ActivityByCooperative thực hiện coopport.Seasons.
func (s Seasons) ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]coopport.CoopActivity, error) {
	if s.Stats == nil {
		return nil, unavailable("pf/cropcycle Stats")
	}
	m, err := s.Stats.ActivityByCooperative(ctx, coops)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]coopport.CoopActivity, len(m))
	for k, v := range m {
		out[k] = coopport.CoopActivity(v)
	}
	return out, nil
}

// Areas thực hiện coopport.Seasons.
func (s Seasons) Areas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]coopport.CoopSeasonArea, error) {
	if s.Stats == nil {
		return nil, unavailable("pf/cropcycle Stats")
	}
	rows, err := s.Stats.SeasonAreas(ctx, coop, seasonCode, year)
	if err != nil {
		return nil, err
	}
	out := make([]coopport.CoopSeasonArea, len(rows))
	for i, r := range rows {
		out[i] = coopport.CoopSeasonArea(r)
	}
	return out, nil
}

// Supply thực hiện coopport.Seasons.
func (s Seasons) Supply(ctx context.Context, year int) ([]coopport.CoopSeasonSupply, error) {
	if s.SupplyStats == nil {
		return nil, unavailable("pf/cropcycle SupplyStats")
	}
	rows, err := s.SupplyStats.SeasonSupply(ctx, year)
	if err != nil {
		return nil, err
	}
	out := make([]coopport.CoopSeasonSupply, len(rows))
	for i, r := range rows {
		out[i] = coopport.CoopSeasonSupply(r)
	}
	return out, nil
}

// Production thực hiện coopport.Seasons.
func (s Seasons) Production(ctx context.Context, q coopport.ProductionQuery) (coopport.ProductionData, error) {
	if s.Reports == nil {
		return coopport.ProductionData{}, unavailable("pf/cropcycle ReportData")
	}
	d, err := s.Reports.Production(ctx, cropcyclesrc.ProductionQuery(q))
	if err != nil {
		return coopport.ProductionData{}, err
	}
	out := coopport.ProductionData{Totals: coopport.ProductionFigures(d.Totals), Rows: make([]coopport.ProductionRow, len(d.Rows))}
	for i, r := range d.Rows {
		out.Rows[i] = coopport.ProductionRow{CooperativeRef: r.CooperativeRef, SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear,
			VarietyCode: r.VarietyCode, ProductionFigures: coopport.ProductionFigures(r.ProductionFigures)}
	}
	return out, nil
}

// HarvestDates thực hiện coopport.Seasons: port cũ không có cổng ngày thu hoạch ngoài bề mặt đối tác
// (cropcycle /v1/partner/season-harvest-dates, có cấp quyền) — W3.3 đọc thẳng store pf_coop.
func (s Seasons) HarvestDates(context.Context, []uuid.UUID) ([]coopport.SeasonHarvestDate, error) {
	return nil, unavailable("pf/cropcycle: chưa có cổng ngày thu hoạch (W3.3)")
}
