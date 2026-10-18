package domain_test

import (
	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}
func id() uuid.UUID                 { return uuid.New() }
func qty(s string) *decimal.Decimal { n := decimal.RequireFromString(s); return &n }
func dataFixture() (domain.Period, port.KHTCPlan, []domain.Submitted, uuid.UUID) {
	item, parent, master := id(), id(), id()
	period := domain.Period{ID: id(), Type: "month", Year: 2026, Name: "Tháng 10", From: day("2026-09-30"), To: day("2026-10-27"), Cutoff: day("2026-10-27"), Departments: []string{"trong_trot"}}
	plan := port.KHTCPlan{VersionID: id(), Year: 2026, Entries: []port.KHTCPlanEntry{
		{Ref: parent, ProgramRef: &master, Kind: "program", Name: "CT III", Tab: "programs", Roman: "III"},
		{Ref: item, ParentRef: &parent, ProgramRef: &master, Kind: "item", Tab: "programs", Name: "Tập huấn", Department: "trong_trot", PlannedQty: qty("100"), DataSource: "form", Communes: []string{"30001"}},
	}}
	sheet := func(periodID uuid.UUID, to, done string, attempt int64) domain.Submitted {
		return domain.Submitted{PeriodID: periodID, Department: "trong_trot", Type: "week", Year: 2026, To: day(to), SubmissionID: id(), SheetID: id(), Attempt: attempt, Hash: done,
			Sheet: form.Sheet{Cells: map[string]form.CellInput{item.String(): {Done: done}}, Texts: map[string]string{}, Adjustments: []form.Adjustment{}}}
	}
	first := sheet(id(), "2026-09-29", "20", 1)
	week := id()
	old := sheet(week, "2026-10-06", "900", 1)
	latest := sheet(week, "2026-10-06", "30", 2)
	latest.Sheet.Adjustments = []form.Adjustment{{EntryRef: item.String(), Amount: "-5", PeriodYear: 2026}, {EntryRef: item.String(), Amount: "1000", PeriodYear: 2025}}
	second := sheet(id(), "2026-10-13", "40", 1)
	narrative := sheet(period.ID, "2026-10-27", "9999", 1)
	narrative.Type = "month"
	narrative.Sheet.Texts["cong_tac_khac"] = "Đã thực hiện"
	return period, plan, []domain.Submitted{first, old, latest, second, narrative}, item
}
func TestKH_FN_TI_02_06_MonthSumsWeeks(t *testing.T) {
	period, plan, sheets, item := dataFixture()
	snapshot := &domain.SourceRef{ID: id(), PlanVersionID: &plan.VersionID, Closed: true}
	got, err := domain.Compose(period, plan, sheets, snapshot, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	var row domain.ProgramRow
	for _, r := range got.Rows {
		if r.Ref == item {
			row = r
		}
	}
	if row.InPeriod != "70" || row.YearToDate != "85" || row.Adjustment != "-5" || row.Percent == nil || *row.Percent != "85" {
		t.Fatalf("month excludes adjustments; yearly total includes only same-year adjustments: %+v", row)
	}
	if len(got.Provisional) != 0 || len(got.Submissions) != 4 || got.FormTexts["trong_trot"]["cong_tac_khac"] != "Đã thực hiện" {
		t.Fatalf("refs/texts/provisional: %+v", got)
	}
	reversed := []domain.Submitted{}
	for i := len(sheets) - 1; i >= 0; i-- {
		reversed = append(reversed, sheets[i])
	}
	again, err := domain.Compose(period, plan, reversed, snapshot, nil, day("2026-10-29"))
	if err != nil || again.InputHash != got.InputHash {
		t.Fatalf("input order/time changes immutable source identity: %s %s %v", got.InputHash, again.InputHash, err)
	}
}
func TestKH_FN_TI_02_09_ProgressFilters(t *testing.T) {
	period, plan, sheets, _ := dataFixture()
	got, err := domain.Compose(period, plan, sheets, nil, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	parent := got.Rows[0].Ref.String()
	if rows := domain.SelectRows(got.Rows, domain.Filter{Program: parent, Commune: "30001", Department: "trong_trot"}); len(rows) != 2 {
		t.Fatalf("filtered item retains its parent: %+v", rows)
	}
	if rows := domain.SelectRows(got.Rows, domain.Filter{Commune: "30002"}); len(rows) != 0 {
		t.Fatalf("unrelated headings must not remain: %+v", rows)
	}
	if len(got.Rows) != 2 {
		t.Fatal("filter mutated report")
	}
}
func TestKH_ReportAdministrativeOverridesAndLockedNumbers(t *testing.T) {
	draft := domain.Draft{Data: domain.Data{Template: "mau_1"}, Admin: map[string]domain.AdminField{"signer": {System: "Tên hệ thống"}}, Texts: map[string]string{"D": "Không có."}}
	name := "Tên đã sửa"
	actor := id()
	when := day("2026-12-01")
	edited, err := domain.Edit(draft, domain.Edits{Admin: map[string]*string{"signer": &name}, Texts: map[string]string{"B": "Khó khăn"}}, actor, when)
	if err != nil {
		t.Fatal(err)
	}
	name = "mutated input"
	if edited.Admin["signer"].Value() != "Tên đã sửa" || draft.Admin["signer"].Value() != "Tên hệ thống" {
		t.Fatal("input/history was aliased")
	}
	updated := domain.Refresh(edited, edited.Data, map[string]string{"signer": "Tên mới"})
	if field := updated.Admin["signer"]; field.Value() != "Tên đã sửa" || !field.Changed || field.Actor == nil || *field.Actor != actor {
		t.Fatalf("refresh overwrote manual value/audit: %+v", field)
	}
	reset, err := domain.Edit(updated, domain.Edits{Admin: map[string]*string{"signer": nil}}, actor, when)
	if err != nil || reset.Admin["signer"].Value() != "Tên mới" {
		t.Fatal("reset must use current system value")
	}
	for _, in := range []domain.Edits{{Admin: map[string]*string{"planned_qty": &name}}, {Texts: map[string]string{"A.I.1": "123"}}} {
		if _, err := domain.Edit(draft, in, actor, when); err == nil {
			t.Fatal("numeric/system section accepted")
		}
	}
}
func TestKH_ReportStoppedParentAndMissingForms(t *testing.T) {
	period, plan, sheets, _ := dataFixture()
	stopped := day("2026-09-01")
	plan.Entries[0].Tracking = "stopped"
	plan.Entries[0].StoppedFrom = &stopped
	got, err := domain.Compose(period, plan, sheets, nil, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 0 || len(got.Provisional) == 0 {
		t.Fatalf("stopped parent/child or missing source: %+v", got)
	}
}
func TestKH_ReportPercentBoundaries(t *testing.T) {
	for _, tc := range []struct{ actual, planned, percent, level string }{{"11.07", "10", "110.7", "achieved"}, {"3", "10", "30", "progressing"}, {"2.99", "10", "29.9", "slow"}} {
		percent := domain.Percent(decimal.RequireFromString(tc.actual), qty(tc.planned))
		if percent == nil || *percent != tc.percent || domain.Level(percent) != tc.level {
			t.Fatalf("%+v: %v", tc, percent)
		}
	}
	if domain.Percent(decimal.NewFromInt(1), qty("0")) != nil {
		t.Fatal("zero plan must not divide")
	}
}

func TestKH_ReportAttainmentBeforeRounding(t *testing.T) {
	if domain.Attainment(decimal.RequireFromString("99.96"), qty("100")) != "progressing" {
		t.Fatal("rounding to 100 must not mark an unfinished item achieved")
	}
	if domain.Attainment(decimal.RequireFromString("29.96"), qty("100")) != "slow" {
		t.Fatal("rounding to 30 must not hide items below 30 percent")
	}
}

func TestKH_ReportPriorAdjustmentOnlyAffectsYearToDate(t *testing.T) {
	period, plan, sheets, item := dataFixture()
	sheets[0].Sheet.Adjustments = []form.Adjustment{{EntryRef: item.String(), Amount: "-2", PeriodYear: 2026}}
	got, err := domain.Compose(period, plan, sheets, nil, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range got.Rows {
		if row.Ref == item {
			if row.InPeriod != "70" || row.YearToDate != "83" || row.Adjustment != "-5" {
				t.Fatalf("prior-month adjustment must only alter yearly total: %+v", row)
			}
			return
		}
	}
	t.Fatal("missing item")
}
func TestKH_ReportYearWith53WeeksAndThirteenPrograms(t *testing.T) {
	period := domain.Period{ID: id(), Type: "year", Year: 2030, Name: "Năm 2030", From: day("2029-12-26"), To: day("2030-12-31"), Cutoff: day("2030-12-31"), Departments: []string{"trong_trot"}}
	plan := port.KHTCPlan{VersionID: id(), Year: 2030}
	item := id()
	for i := 0; i < 13; i++ {
		parent := id()
		plan.Entries = append(plan.Entries, port.KHTCPlanEntry{Ref: parent, Kind: "program", Name: "Chương trình", Tab: "programs"})
		if i == 12 {
			plan.Entries = append(plan.Entries, port.KHTCPlanEntry{Ref: item, ParentRef: &parent, Kind: "item", Department: "trong_trot", DataSource: "form", PlannedQty: qty("100"), Tab: "programs"})
		}
	}
	sheets := []domain.Submitted{}
	for i := 0; i < 53; i++ {
		end := day("2030-01-01").AddDate(0, 0, 7*i)
		sheets = append(sheets, domain.Submitted{PeriodID: id(), Department: "trong_trot", Type: "week", Year: 2030, To: end, SubmissionID: id(), Attempt: 1, SheetID: id(), Sheet: form.Sheet{Cells: map[string]form.CellInput{item.String(): {Done: "1.5"}}}})
	}
	// Numeric cells from the annual narrative sheet do not double count the weekly series.
	sheets = append(sheets, domain.Submitted{PeriodID: period.ID, Department: "trong_trot", Type: "year", Year: 2030, To: period.To, SubmissionID: id(), Attempt: 1, SheetID: id(), Sheet: form.Sheet{Cells: map[string]form.CellInput{item.String(): {Done: "9999"}}}})
	got, err := domain.Compose(period, plan, sheets, &domain.SourceRef{ID: id(), Closed: true}, nil, day("2031-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	programs := 0
	for _, row := range got.Rows {
		if row.Kind == "program" {
			programs++
		}
		if row.Ref == item && (row.InPeriod != "79.5" || row.YearToDate != "79.5") {
			t.Fatalf("53 weekly increments: %+v", row)
		}
	}
	if programs != 13 {
		t.Fatalf("dynamic programs=%d", programs)
	}
}

func TestKH_ReportLevelFilterAndLatestStatus(t *testing.T) {
	period, plan, sheets, item := dataFixture()
	sheets[0].Sheet.Cells[item.String()] = form.CellInput{Done: "20", Status: "Đang sinh trưởng"}
	got, err := domain.Compose(period, plan, sheets, nil, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	if rows := domain.SelectRows(got.Rows, domain.Filter{Level: "progressing"}); len(rows) != 2 || rows[1].Status != "Đang sinh trưởng" {
		t.Fatalf("status/filter %+v", rows)
	}
	if rows := domain.SelectRows(got.Rows, domain.Filter{Level: "achieved"}); len(rows) != 0 {
		t.Fatal(rows)
	}
	if err := (domain.Filter{Level: "unknown"}).Validate(); err == nil {
		t.Fatal("invalid level accepted")
	}
}

func TestKH_UR_KT_02_YieldIsOutputOverArea(t *testing.T) {
	metrics := []domain.SourceMetric{{Key: "season:trong_trot:t04:dt_th", Department: "trong_trot", Form: "t04", Value: source.Value{Cumulative: qty("100"), Closed: true}}, {Key: "season:trong_trot:t04:san_luong", Department: "trong_trot", Form: "t04", Value: source.Value{Cumulative: qty("740"), Closed: true}}}
	got := domain.WithYields(metrics)
	if len(got) != 3 || !got[2].Value.Cumulative.Equal(*qty("7.4")) || got[2].Unit != "tấn/ha" || !got[2].Stock || !got[2].Value.Closed {
		t.Fatalf("weighted yield %+v", got)
	}
	metrics[0].Value.Cumulative = qty("0")
	metrics[1].Value.Closed = false
	got = domain.WithYields(metrics)
	if got[2].Value.Cumulative != nil || got[2].Value.Closed {
		t.Fatal("zero area/missing source must not fabricate a yield")
	}
}
