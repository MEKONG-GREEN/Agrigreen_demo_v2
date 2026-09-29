package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ReportData thực hiện port.ReportData (HTX US-DASH-02, US-DASH-03) trên bảng chiếu
// yield_summary — cùng truy vấn và công thức với bảng điều hành (Dashboard).
type ReportData struct{ Svc *Service }

var _ port.ReportData = ReportData{}

// errBadReportQuery: truy vấn thiếu tenant, thiếu HTX khi CoopOnly hoặc mã lọc sai (lỗi của bên gọi).
var errBadReportQuery = errors.New("cropcycle: truy vấn báo cáo sản lượng không hợp lệ")

func reportFilter(q port.ProductionQuery) store.Filter {
	switch {
	case q.CoopOnly:
		return store.Filter{Mode: store.FilterCoop, Coop: q.CooperativeRef}
	case q.Scope.All:
		return store.Filter{Mode: store.FilterAll}
	case len(q.Scope.Units) > 0:
		return store.Filter{Mode: store.FilterUnits, Units: q.Scope.Units}
	}
	return store.Filter{Mode: store.FilterNone}
}

func figures(g store.DashGroup) (port.ProductionFigures, error) {
	d := derive(g)
	exp, err := mass.ParseKg(g.ExpectedKg.String())
	if err != nil {
		return port.ProductionFigures{}, err
	}
	act, err := mass.ParseKg(g.ActualKg.String())
	if err != nil {
		return port.ProductionFigures{}, err
	}
	return port.ProductionFigures{
		Seasons: g.Seasons, OpenSeasons: g.OpenSeasons, CompletedSeasons: g.CompletedSeasons, Households: g.Households,
		AreaHa: g.AreaHa, HarvestedAreaHa: g.HarvestedAreaHa, ExpectedKg: exp, ActualKg: act,
		ActualYieldTPerHa: d.ActualYieldTPerHa, SOPSteps: g.SOPSteps, SOPStepsDone: g.SOPStepsDone, SOPProgressPct: d.SOPProgressPct,
	}, nil
}

// Production tổng hợp sản lượng dự kiến – thực tế, diện tích, số hộ, tiến độ SOP
// theo HTX × vụ × giống trong phạm vi đã giải.
func (r ReportData) Production(ctx context.Context, q port.ProductionQuery) (port.ProductionData, error) {
	if q.TenantID == uuid.Nil || (q.CoopOnly && q.CooperativeRef == uuid.Nil) ||
		(q.SeasonCode != "" && !domain.ValidSeasonCode(q.SeasonCode)) || (q.VarietyCode != "" && !domain.ValidCatalogCode(q.VarietyCode)) {
		return port.ProductionData{}, errBadReportQuery
	}
	f := store.DashFilter{Scope: reportFilter(q), CooperativeRef: q.CooperativeRef, SeasonCode: q.SeasonCode, SeasonYear: q.SeasonYear,
		VarietyCode: q.VarietyCode}
	out := port.ProductionData{Rows: []port.ProductionRow{}}
	err := r.Svc.inTx(ctx, q.TenantID, func(tx pgx.Tx) error {
		groups, err := store.ProductionGroups(ctx, tx, f, q.ByCooperative, q.BySeason, q.ByVariety)
		if err != nil {
			return err
		}
		for _, g := range groups {
			fig, err := figures(g.DashGroup)
			if err != nil {
				return err
			}
			out.Rows = append(out.Rows, port.ProductionRow{CooperativeRef: g.CooperativeRef, SeasonCode: g.SeasonCode, SeasonYear: g.SeasonYear,
				VarietyCode: g.VarietyCode, ProductionFigures: fig})
		}
		tot, err := store.ProductionGroups(ctx, tx, f, false, false, false)
		if err != nil {
			return err
		}
		var t store.DashGroup
		if len(tot) > 0 {
			t = tot[0].DashGroup
		}
		out.Totals, err = figures(t)
		return err
	})
	return out, err
}
