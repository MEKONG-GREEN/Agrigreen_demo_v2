package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestKH_ReportSumYearToDateAndFrozenWeeks(t *testing.T) {
	parse := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	periods := []port.KHTCPeriod{}
	for _, tc := range []struct{ key, to, value string }{{"2026-W40", "2026-10-06", "90"}, {"2026-W41", "2026-10-13", "120"}} {
		day := parse(tc.to)
		src := port.SourcePeriodRef{NativeKey: tc.key, From: day.AddDate(0, 0, -6), To: day, Closed: true, Cells: map[string]string{"tickets_collected": tc.value}}
		figure := port.TargetFigure{FormCode: "PRICE-TICKETS", KeyClosed: true, SumUnconfirmed: map[string]string{"tickets_collected": "2"}, Sources: []port.SourcePeriodRef{src}}
		department := port.DepartmentFigures{DepartmentCode: "thong_tin_truyen_thong", Closed: true, Forms: []port.TargetFigure{figure}}
		periods = append(periods, port.KHTCPeriod{TargetKey: tc.key, TargetFrom: day.AddDate(0, 0, -6), TargetTo: day, Departments: []port.DepartmentFigures{department}})
	}
	key, err := domain.ParseSourceKey("sum:thong_tin_truyen_thong:price-tickets:tickets_collected")
	if err != nil {
		t.Fatal(err)
	}
	frozen := decimal.NewFromInt(100)
	got := domain.Evaluate(key, periods, map[string]domain.Frozen{"2026-W40": {Sum: &frozen}}, parse("2026-10-07"), parse("2026-10-13"))
	if got.Sum == nil || !got.Sum.Equal(decimal.NewFromInt(120)) || got.YearToDate == nil || !got.YearToDate.Equal(decimal.NewFromInt(220)) {
		t.Fatalf("current week120, yearly220 (frozen100+120): %+v", got)
	}
	if got.Unconfirmed == nil || !got.Unconfirmed.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("period flow unconfirmed: %+v", got)
	}
}
