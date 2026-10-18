package domain

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"testing"
	"time"
)

func TestKH_M4_PriceDateDoesNotExtendQuantityPeriod(t *testing.T) {
	day := func(s string) time.Time {
		d, e := time.Parse(time.DateOnly, s)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	src := func(date, value string) port.SourcePeriodRef {
		return port.SourcePeriodRef{NativeKey: date, To: day(date), From: day(date), Closed: true, FormCode: port.PriceFormBoard, Cells: map[string]string{"MH00001/price_min": value, "MH00001/price_max": value}}
	}
	price := port.TargetFigure{DepartmentCode: "thong_tin_truyen_thong", FormCode: port.PriceFormBoard, TargetKey: "2026-W42", TargetFrom: day("2026-10-14"), TargetTo: day("2026-10-20"), Sources: []port.SourcePeriodRef{src("2026-10-14", "1400"), src("2026-10-15", "1500")}}
	quantities := func(key, from, to, qty string) port.KHTCPeriod {
		return port.KHTCPeriod{TargetKey: key, TargetFrom: day(from), TargetTo: day(to), Closed: true, Departments: []port.DepartmentFigures{{DepartmentCode: "thong_tin_truyen_thong", Closed: true, Forms: []port.TargetFigure{{FormCode: port.PriceFormTickets, KeyClosed: true, Sources: []port.SourcePeriodRef{{NativeKey: key, To: day(to), Closed: true, Cells: map[string]string{"tickets_collected": qty}}}}}}}}
	}
	periods := []port.KHTCPeriod{quantities("2026-W41", "2026-10-07", "2026-10-13", "7"), quantities("2026-W42", "2026-10-14", "2026-10-20", "99")}
	periods[0].PriceAsOf = []port.TargetFigure{price}
	from, to, cutoff := day("2026-10-07"), day("2026-10-13"), day("2026-10-14")
	pk, _ := ParseSourceKey("price:thong_tin_truyen_thong:price-board:mh00001")
	qk, _ := ParseSourceKey("sum:thong_tin_truyen_thong:price-tickets:tickets_collected")
	p, q := EvaluateAt(pk, periods, nil, from, to, cutoff), EvaluateAt(qk, periods, nil, from, to, cutoff)
	if len(p.PriceDays) != 1 || p.PriceDay != "2026-10-14" || p.Min.String() != "1400" || p.NoSource {
		t.Fatalf("price cutoff: %+v", p)
	}
	if q.Sum == nil || q.Sum.String() != "7" {
		t.Fatalf("price cutoff extended quantities: %+v", q)
	}
	rows := RowsAt(periods, from, to, cutoff)
	if len(rows) != 2 {
		t.Fatalf("capture leaked quantities or after-cutoff board: %+v", rows)
	}
	found := false
	for _, row := range rows {
		if row.FormCode == port.PriceFormBoard {
			found = true
			if row.NativeKey != "2026-10-14" || row.TargetKey != "2026-W42" {
				t.Fatalf("D8 provenance: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("price outside quantity period not captured")
	}
	periods[0].PriceAsOf = []port.TargetFigure{}
	if p = EvaluateAt(pk, periods, nil, from, to, cutoff); len(p.PriceDays) != 0 || !p.NoSource {
		t.Fatalf("empty approved source fell back to weekly data: %+v", p)
	}
}

func TestKH_M4_LegacyPriceProjectionDoesNotInventSourceChange(t *testing.T) {
	old := Row{TargetKey: "2026-W41", FormCode: port.PriceFormBoard, NativeKey: "2026-09-01", SourceModule: "pf/price", CalendarID: "thong_tin_truyen_thong", Fallback: true, FiguresHash: str("same")}
	live := old
	live.TargetKey = "2026-W35"
	live.Fallback = false
	if got := Changes([]Row{old}, []Row{live}); len(got) != 0 {
		t.Fatalf("same board reported changed: %+v", got)
	}
	live.FiguresHash = str("different")
	if got := Changes([]Row{old}, []Row{live}); len(got) != 1 || got[0].Kind != ChangeFigures {
		t.Fatalf("changed board missed: %+v", got)
	}
}
