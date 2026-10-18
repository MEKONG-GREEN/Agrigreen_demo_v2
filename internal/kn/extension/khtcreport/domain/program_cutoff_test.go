package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"testing"
)

// BRD FN-TI-02.09: the appendix uses submitted figures through cutoff,
// including full source weeks only. Keep the requested period identity and its
// current narrative submission even when the quantity window ends earlier.
func TestKH_M5_ProgramUsesCutoffForWeeklyFigures(t *testing.T) {
	p, plan, sheets, item := dataFixture()
	p.Type = "program"
	p.From = day("2025-12-31")
	p.Cutoff = day("2026-10-08")
	sheets[len(sheets)-1].Type = "program"
	got, err := domain.Compose(p, plan, sheets, &domain.SourceRef{ID: id(), PlanVersionID: &plan.VersionID, Closed: true}, nil, day("2026-10-14"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range got.Rows {
		if row.Ref == item {
			if row.InPeriod != "50" || row.YearToDate != "45" || row.Adjustment != "-5" {
				t.Errorf("includes a week after cutoff: %+v", row)
			}
		}
	}
	if !got.Period.To.Equal(p.To) || len(got.Provisional) != 0 || got.FormTexts["trong_trot"]["cong_tac_khac"] != "Đã thực hiện" {
		t.Errorf("lost report identity/narrative: %+v", got)
	}
	for _, sub := range got.Submissions {
		if sub.Type == "week" && sub.To.After(p.Cutoff) {
			t.Errorf("captured future week: %+v", sub)
		}
	}
}
