package app

import (
	"context"
	"fmt"
	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestW1168TypedSourceExponentBoundBeforeRendering(t *testing.T) {
	for _, exponent := range []int32{-100000, 100000} {
		n := decimal.New(1, exponent)
		if err := boundedMetrics([]domain.SourceMetric{{Value: source.Value{Sum: &n}}}); err == nil {
			t.Fatal("hostile typed source accepted", exponent)
		}
	}
}

type adhocYearPlans struct {
	plans map[int]port.KHTCPlan
	seen  []int
}

func (p *adhocYearPlans) PlanAt(_ context.Context, y int, _ time.Time) (port.KHTCPlan, error) {
	p.seen = append(p.seen, y)
	v, ok := p.plans[y]
	if !ok {
		return port.KHTCPlan{}, port.ErrKHTCPlanNotFound
	}
	return v, nil
}
func TestW1168CrossYearRangePinsEveryPlanAndPreservesUnits(t *testing.T) {
	from := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	row := uuid.New()
	plans := &adhocYearPlans{plans: map[int]port.KHTCPlan{}}
	sheets := []domain.Submitted{}
	for i, y := range []int{2024, 2025, 2026} {
		unit := []string{"ha", "lớp", "người"}[i]
		at := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
		if y == 2026 {
			at = to
		}
		plans.plans[y] = port.KHTCPlan{Year: y, VersionID: uuid.New(), Version: 1, Entries: []port.KHTCPlanEntry{{Ref: row, Tab: "programs", Kind: "item", Name: "Cùng tên qua năm", Department: "trong_trot", UnitCode: unit, DataSource: "form"}}}
		// Equal row identity across plans must never add across different years/units.
		sheets = append(sheets, domain.Submitted{PeriodID: uuid.New(), SubmissionID: uuid.New(), Year: y, Department: "trong_trot", Type: "week", To: at, Attempt: 1, Decision: "accepted", Hash: fmt.Sprint(y), Sheet: form.Sheet{Cells: map[string]form.CellInput{row.String(): {Done: fmt.Sprint(i + 1)}}}})
	}
	// A previous week outside the requested range is excluded, including its value.
	sheets = append(sheets, domain.Submitted{PeriodID: uuid.New(), SubmissionID: uuid.New(), Year: 2024, Department: "trong_trot", Type: "week", To: from.Add(-7 * 24 * time.Hour), Attempt: 1, Sheet: form.Sheet{Cells: map[string]form.CellInput{row.String(): {Done: "999"}}}})
	service := Service{Plans: plans, Clock: clock.NewFixed(to)}
	p := ps.Period{ID: uuid.New(), Year: 2026, Type: "adhoc", From: from, To: to, Cutoff: to, Departments: []string{"trong_trot"}}
	values, e := service.priorYearInputs(context.Background(), p, sheets)
	if e != nil {
		t.Fatal(e)
	}
	if len(values) != 2 || len(plans.seen) != 2 || plans.seen[0] != 2024 || plans.seen[1] != 2025 {
		t.Fatalf("dropped a prior plan year: %v/%v", values, plans.seen)
	}
	for i, v := range values {
		y := 2024 + i
		if v.Value.Sum == nil || v.Value.Sum.String() != fmt.Sprint(i+1) || v.Unit != []string{"ha", "lớp"}[i] || v.Key != fmt.Sprintf("ke_hoach_nam:%d:%s", y, row) {
			t.Fatalf("mixed year/unit: %+v", v)
		}
		want := fmt.Sprintf("plan:%d:%s", y, plans.plans[y].VersionID)
		if len(v.Trace) != 1 || v.Trace[0].Calendar != want || v.Trace[0].FiguresHash == nil {
			t.Fatalf("unversioned provenance: %+v", v.Trace)
		}
	}
	current, e := domain.Compose(domain.Period{ID: p.ID, Type: "adhoc", Year: 2026, From: from, To: to, Cutoff: to, Departments: p.Departments}, plans.plans[2026], sheets, nil, values, to)
	if e != nil || len(current.Rows) != 1 || current.Rows[0].InPeriod != "3" || current.Rows[0].Unit != "người" {
		t.Fatalf("current year mixed: %+v %v", current.Rows, e)
	}
}
