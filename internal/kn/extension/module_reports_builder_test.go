package extension

import (
	"context"
	knreportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

type reportBuilderFixture struct {
	last    knreportsport.ReportQuery
	targets map[string]*decimal.Decimal
	coops   [2]uuid.UUID
}

func (f *reportBuilderFixture) Sowing(_ context.Context, q knreportsport.ReportQuery) (knreportsport.SowingData, error) {
	f.last = q
	return knreportsport.SowingData{Rows: []knreportsport.SowingRow{
		{ProvinceCode: "89", CommuneCode: "8901", SeasonCode: "HT", VarietyCode: "A", SownAreaHa: decimal.RequireFromString("1.1"), Cooperatives: 1, Reports: 2},
		{ProvinceCode: "89", CommuneCode: "8901", SeasonCode: "HT", VarietyCode: "B", SownAreaHa: decimal.RequireFromString("2.2"), Cooperatives: 2, Reports: 3},
	}, Completeness: knreportsport.Completeness{Provisional: true, ExpectedCommunes: 2, MissingCommunes: []string{"8902"}}}, nil
}
func (f *reportBuilderFixture) GrowthPest(_ context.Context, q knreportsport.ReportQuery) (knreportsport.PestData, error) {
	f.last = q
	return knreportsport.PestData{Rows: []knreportsport.PestRow{{ProvinceCode: "89", CommuneCode: "8901", Severity: "nhe", AffectedAreaHa: decimal.RequireFromString("3.25"), Observations: 2, Urgent: 1}}}, nil
}
func (f *reportBuilderFixture) TasksSLA(_ context.Context, q knreportsport.ReportQuery) (knreportsport.TaskData, error) {
	f.last = q
	return knreportsport.TaskData{Rows: []knreportsport.TaskRow{{ProvinceCode: "89", CommuneCode: "8901", Opened: 10, Resolved: 5, OnTime: 4, Escalated: 2, Open: 5, Overdue: 1}}}, nil
}
func (f *reportBuilderFixture) PeriodSummary(_ context.Context, q knreportsport.ReportQuery) (knreportsport.SummaryData, error) {
	f.last = q
	return knreportsport.SummaryData{Rows: []knreportsport.SummaryRow{{ProvinceCode: "89", CommuneCode: "8901", SownAreaHa: decimal.NewFromInt(4), RegisteredHa: decimal.NewFromInt(5), TasksOverdue: 2}}, Completeness: knreportsport.Completeness{Provisional: true, OpenProvinces: []string{"89"}}}, nil
}
func (f *reportBuilderFixture) ProgramProgress(_ context.Context, q knreportsport.ReportQuery) (knreportsport.ProgramData, error) {
	f.last = q
	return knreportsport.ProgramData{Rows: []knreportsport.ProgramRow{
		{ProvinceCode: "89", CommuneCode: "8901", CooperativeRef: f.coops[0], RegisteredHa: decimal.NewFromInt(10), Approved: true},
		{ProvinceCode: "90", CommuneCode: "9001", CooperativeRef: f.coops[1], RegisteredHa: decimal.NewFromInt(20)},
	}, Targets: f.targets}, nil
}
func TestReportSourceBuildersFiguresAndMissingTargets(t *testing.T) {
	a, b := decimal.NewFromInt(100), decimal.NewFromInt(200)
	fixture := &reportBuilderFixture{coops: [2]uuid.UUID{uuid.New(), uuid.New()}, targets: map[string]*decimal.Decimal{"89": &a, "90": &b}}
	builders := ReportBuilders(ReportBuilderDeps{Data: fixture})
	if len(builders) != 5 {
		t.Fatal("five source builders required")
	}
	bySource := map[string]reportsport.SourceBuilder{}
	for _, builder := range builders {
		bySource[builder.Source] = builder.Build
	}
	query := reportsport.SourceBuildQuery{TenantID: uuid.New(), At: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Scope: authz.Scope{All: true}, Level: "national", Periods: []string{"2026-09"}, PeriodLabel: "Tháng 9/2026"}
	expect := func(row reportsport.SourceRow, key, want string) {
		t.Helper()
		value := row[key]
		if value.Dec == nil || value.Dec.String() != want {
			t.Fatalf("%s: %+v want %s", key, value, want)
		}
	}
	for _, tc := range []struct{ source, key, want string }{
		{knreportsport.SourceSowing, "sown_area_ha", "3.3"}, {knreportsport.SourceGrowthPest, "affected_area_ha", "3.25"}, {knreportsport.SourceTasksSLA, "opened", "10"}, {knreportsport.SourcePeriodSummary, "registered_ha", "5"}, {knreportsport.SourceProgramProgress, "registered_ha", "30"},
	} {
		query.Source = tc.source
		data, err := bySource[tc.source](context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		expect(data.Total, tc.key, tc.want)
		if fixture.last.TenantID != query.TenantID || !fixture.last.At.Equal(query.At) || !fixture.last.Scope.All || len(fixture.last.Periods) != 1 {
			t.Fatal("source query binding changed")
		}
		if tc.source == knreportsport.SourceSowing {
			if len(data.Rows) != 1 || !data.Provisional || len(data.Notes) == 0 {
				t.Fatal("grouping/completeness changed")
			}
			expect(data.Total, "reports", "5")
		}
		if tc.source == knreportsport.SourceProgramProgress {
			expect(data.Total, "target_ha", "300")
			expect(data.Total, "registered_pct", "10")
			expect(data.Total, "cooperatives", "2")
		}
	}
	fixture.targets["90"] = nil
	query.Source = knreportsport.SourceProgramProgress
	data, err := bySource[query.Source](context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if data.Total["target_ha"].Dec != nil || data.Total["registered_pct"].Dec != nil {
		t.Fatal("missing province target was treated as zero/partial total")
	}
	query.Source = knreportsport.SourceSowing
	query.Columns = []reportsport.SourceBuildColumn{{Key: "variety_code"}}
	data, err = bySource[query.Source](context.Background(), query)
	if err != nil || len(data.Rows) != 2 {
		t.Fatalf("visible variety dimension grouping: %d %v", len(data.Rows), err)
	}
}
