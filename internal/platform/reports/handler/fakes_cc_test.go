package handler_test

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ccRow là một dòng giả của pf/cropcycle kèm xã và tập nông hộ (để tính số hộ phân biệt ở dòng tổng).
type ccRow struct {
	commune string
	hh      []string
	row     cropcyclesrc.ProductionRow
}

// fakeCC là cổng báo cáo sản lượng giả của pf/cropcycle (HTX US-DASH-02): lọc theo phạm vi
// như hiện thực thật, ghi lại truy vấn; tổng tính lại trên tập đã lọc (số hộ phân biệt).
type fakeCC struct {
	mu   sync.Mutex
	last cropcyclesrc.ProductionQuery
	rows []ccRow
}

func fig(seasons, open, completed int64, area, harvested, expKg, actKg string, steps, done int64) cropcyclesrc.ProductionFigures {
	f := cropcyclesrc.ProductionFigures{Seasons: seasons, OpenSeasons: open, CompletedSeasons: completed, AreaHa: d(area), HarvestedAreaHa: d(harvested),
		ExpectedKg: mass.MustKg(expKg), ActualKg: mass.MustKg(actKg), SOPSteps: steps, SOPStepsDone: done}
	derive(&f)
	return f
}

func derive(f *cropcyclesrc.ProductionFigures) {
	if f.HarvestedAreaHa.IsPositive() {
		y := f.ActualKg.Decimal().Div(decimal.NewFromInt(1000)).Div(f.HarvestedAreaHa).Round(4)
		f.ActualYieldTPerHa = &y
	}
	if f.SOPSteps > 0 {
		p := decimal.NewFromInt(f.SOPStepsDone * 100).Div(decimal.NewFromInt(f.SOPSteps)).Round(1)
		f.SOPProgressPct = &p
	}
}

func newFakeCC(coop, coop2 uuid.UUID) *fakeCC {
	row := func(ref uuid.UUID, variety string, f cropcyclesrc.ProductionFigures, hh int64) cropcyclesrc.ProductionRow {
		f.Households = hh
		return cropcyclesrc.ProductionRow{CooperativeRef: ref, SeasonCode: "HT", SeasonYear: 2026, VarietyCode: variety, ProductionFigures: f}
	}
	return &fakeCC{rows: []ccRow{
		{commune: cBinhDuc, hh: []string{"h1", "h2"}, row: row(coop, "OM5451", fig(2, 1, 1, "3.0000", "1.5000", "20250.000", "9750.125", 10, 4), 2)},
		{commune: cBinhDuc, hh: []string{"h1"}, row: row(coop, "ST25", fig(1, 1, 0, "1.5000", "0", "9000.000", "0", 5, 0), 1)},
		{commune: cCaiKhe, hh: []string{"h9"}, row: row(coop2, "ST25", fig(1, 0, 1, "2.2500", "2.2500", "13500.000", "14000.500", 5, 5), 1)},
	}}
}

func (f *fakeCC) lastQuery() cropcyclesrc.ProductionQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func (f *fakeCC) Production(_ context.Context, q cropcyclesrc.ProductionQuery) (cropcyclesrc.ProductionData, error) {
	f.mu.Lock()
	f.last = q
	f.mu.Unlock()
	out := cropcyclesrc.ProductionData{Rows: []cropcyclesrc.ProductionRow{}}
	var tot cropcyclesrc.ProductionFigures
	tot.ExpectedKg, tot.ActualKg = mass.MustKg("0"), mass.MustKg("0")
	hh := map[string]bool{}
	for _, r := range f.rows {
		switch {
		case q.CoopOnly:
			if r.row.CooperativeRef != q.CooperativeRef {
				continue
			}
		case !q.Scope.All && !q.Scope.Allows(r.commune):
			continue
		case q.CooperativeRef != uuid.Nil && r.row.CooperativeRef != q.CooperativeRef:
			continue
		}
		if (q.SeasonCode != "" && r.row.SeasonCode != q.SeasonCode) || (q.SeasonYear != 0 && r.row.SeasonYear != q.SeasonYear) ||
			(q.VarietyCode != "" && r.row.VarietyCode != q.VarietyCode) {
			continue
		}
		out.Rows = append(out.Rows, r.row)
		x := r.row.ProductionFigures
		tot.Seasons += x.Seasons
		tot.OpenSeasons += x.OpenSeasons
		tot.CompletedSeasons += x.CompletedSeasons
		tot.AreaHa = tot.AreaHa.Add(x.AreaHa)
		tot.HarvestedAreaHa = tot.HarvestedAreaHa.Add(x.HarvestedAreaHa)
		tot.ExpectedKg = tot.ExpectedKg.Add(x.ExpectedKg)
		tot.ActualKg = tot.ActualKg.Add(x.ActualKg)
		tot.SOPSteps += x.SOPSteps
		tot.SOPStepsDone += x.SOPStepsDone
		for _, h := range r.hh {
			hh[h] = true
		}
	}
	tot.Households = int64(len(hh))
	derive(&tot)
	out.Totals = tot
	return out, nil
}
