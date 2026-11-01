package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"strings"
	"testing"
)

func TestDashboardMatchesMau5(t *testing.T) {
	p, plan, sheets, _ := dataFixture()
	p.Type = "program"
	p.From = day("2026-01-01")
	p.Cutoff, p.To = day("2026-10-13"), day("2026-10-13")
	// No synthetic draft values; Compose uses latest submitted weekly sheets.
	data, err := domain.Compose(p, plan, sheets, &domain.SourceRef{ID: id(), Closed: true}, nil, day("2026-10-13"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := domain.SummarizeDashboard(data, "trong_trot", "", day("2026-10-13"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Items != 1 || got.Achieved != 0 || got.Slow != 0 || len(got.Programs) != 1 || got.Programs[0].Percent == nil || *got.Programs[0].Percent != "85" {
		t.Fatalf("same captured M5 rows: %+v", got)
	}
	if *got.Rows[0].Percent != *got.Programs[0].Percent {
		t.Fatalf("M5 percent differs: %+v", got)
	}
	filtered, err := domain.SummarizeDashboard(data, "chan_nuoi_thuy_san", "", day("2026-10-13"))
	if err != nil || filtered.Items != 0 {
		t.Fatalf("department filter %+v %v", filtered, err)
	}
}
func TestDashboardMissingThresholdAndExactMean(t *testing.T) {
	root, a, b, c := id(), id(), id(), id()
	data := domain.Data{Period: domain.Period{Year: 2026}, Plan: port.KHTCPlan{Entries: []port.KHTCPlanEntry{{Ref: root, Kind: "program", FieldCode: "lua"}, {Ref: a, ParentRef: &root, PlannedQty: qty("3")}, {Ref: b, ParentRef: &root, PlannedQty: qty("7")}, {Ref: c, ParentRef: &root, PlannedQty: qty("1")}}}, Rows: []domain.ProgramRow{{Ref: a, ParentRef: &root, Kind: "item", Tab: "programs", YearToDate: "1"}, {Ref: b, ParentRef: &root, Kind: "item", Tab: "programs", YearToDate: "1"}, {Ref: c, ParentRef: &root, Kind: "item", Tab: "programs", YearToDate: "1000", Missing: true}}}
	got, err := domain.SummarizeDashboard(data, "", "lua", day("2026-09-30"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Items != 3 || got.Missing != 1 || got.Achieved != 0 || got.Slow != 0 || *got.Programs[0].Percent != "23.81" {
		t.Fatalf("exact mean / missing: %+v", got)
	}
	got, err = domain.SummarizeDashboard(data, "", "lua", day("2026-10-01"))
	if err != nil || got.Slow != 1 {
		t.Fatalf("3/4 threshold: %+v %v", got, err)
	}
	for _, hostile := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		data.Rows[0].YearToDate = hostile
		if _, err = domain.SummarizeDashboard(data, "", "", day("2026-10-01")); err == nil {
			t.Fatal("unbounded corrupt value accepted")
		}
	}

}
