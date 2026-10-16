package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func cntsWeek(t *testing.T, key string) Span {
	t.Helper()
	s, err := SpanOfKey(CNTSRule, PeriodWeek, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func dayPeriod(d string, status string, units []string, parents map[string]string, data map[string]SourceUnit) SourcePeriod {
	x, _ := ParseDate(d)
	return SourcePeriod{ID: uuid.New(), Key: d, From: x, To: x, Status: status, Units: units, Parents: parents, Data: data}
}

func TestBM02_SumsAdjustmentsMissingDaysStale_W1144(t *testing.T) {
	weeks, err := MonthKSGMWeeks(2026, 10, nil)
	if err != nil || len(weeks) != 4 || weeks[1].Key != "2026-10-K2" {
		t.Fatalf("tuần KSGM: %+v %v", weeks, err)
	}
	par := map[string]string{"F1": "X1", "F2": "X2"}
	days := []SourcePeriod{
		dayPeriod("2026-10-08", "closed", []string{"F1", "F2"}, par, map[string]SourceUnit{
			"F1": {Values: map[string]string{"heo": "10", "trau_bo": "2", "gia_cam": "100", "de": "0"}},
			"F2": {Values: map[string]string{"heo": "0", "trau_bo": "0", "gia_cam": "0", "de": "0"}, NilReport: true}}),
		dayPeriod("2026-10-09", "open", []string{"F1", "F2"}, par, map[string]SourceUnit{
			"F1": {Values: map[string]string{"heo": "12.5", "trau_bo": "1", "gia_cam": "50", "de": "1"}}}),
		dayPeriod("2026-10-01", "closed", []string{"F1"}, par, map[string]SourceUnit{ // tuần K1 — ngoài tuần chọn
			"F1": {Values: map[string]string{"heo": "999"}}}),
	}
	wk := weeks[1:2]
	cur := AutoByFacilityWeek(weeks, days)
	adj := BM02Adjustment{ID: uuid.New(), Facility: "F1", Commune: "X1", WeekKey: "2026-10-K2", Deltas: Dec{"heo": dec("-2.5")},
		Reason: "cộng nhầm ca đêm", Base: cur["F1"]["2026-10-K2"].clone()}
	other := BM02Adjustment{ID: uuid.New(), Facility: "F1", WeekKey: "2026-10-K3", Deltas: Dec{"heo": dec("7")}, Base: Dec{}}
	r := BM02(wk, days, []BM02Adjustment{adj, other}, cur)
	if r.SourceCount != 2 || r.SourceClosed != 1 || r.AllClosed {
		t.Fatalf("kỳ nguồn: %+v", r)
	}
	if len(r.Rows) != 2 || r.Rows[0].Facility != "F1" || r.Rows[1].Facility != "F2" {
		t.Fatalf("dòng: %+v", r.Rows)
	}
	f1 := r.Rows[0]
	if !f1.Auto["heo"].Equal(dec("22.5")) || !f1.Total()["heo"].Equal(dec("20")) || !f1.Adjust["heo"].Equal(dec("-2.5")) {
		t.Fatalf("F1: auto %v total %v", f1.Auto, f1.Total())
	}
	if f1.DaysExpected != 2 || f1.DaysReported != 2 || f1.MissingDays() != 0 || f1.Stale {
		t.Fatalf("F1 ngày: %+v", f1)
	}
	f2 := r.Rows[1]
	if f2.DaysExpected != 2 || f2.DaysReported != 1 || f2.MissingDays() != 1 {
		t.Fatalf("F2 thiếu ngày: %+v", f2)
	}
	if !r.Total()["heo"].Equal(dec("20")) || r.UnitsTotal != 2 || r.UnitsReported != 2 {
		t.Fatalf("tổng: %v", r.Total())
	}
	// BM-01 đổi sau điều chỉnh → "cần xem lại".
	days[1].Data["F1"] = SourceUnit{Values: map[string]string{"heo": "13"}}
	cur2 := AutoByFacilityWeek(weeks, days)
	r2 := BM02(wk, days, []BM02Adjustment{adj}, cur2)
	if !r2.Rows[0].Stale || !StaleAdjustments([]BM02Adjustment{adj}, cur2)[adj.ID] {
		t.Fatal("thiếu cờ cần xem lại")
	}
	// Tháng = 4 tuần: gồm cả ngày 01/10 và dòng điều chỉnh tuần K3.
	m := BM02(weeks, days, []BM02Adjustment{adj, other}, nil)
	if !m.Total()["heo"].Equal(dec("999").Add(dec("23")).Add(dec("-2.5")).Add(dec("7"))) {
		t.Fatalf("tháng: %v", m.Total())
	}
	pub := PubBM02(r)
	if len(pub.Rows) != 2 || pub.Rows[0].Cells["heo"] != "20" || pub.Rows[0].Cells["heo_dieu_chinh"] != "-2.5" ||
		pub.Rows[1].Cells["so_ngay_bao_cao"] != "1" || !pub.Provisional || *pub.Rows[0].Parent != "X1" {
		t.Fatalf("công bố BM-02: %+v", pub)
	}
}

func weekPeriod(t *testing.T, key, status string, data map[string]SourceUnit, units ...string) SourcePeriod {
	s := cntsWeek(t, key)
	return SourcePeriod{ID: uuid.New(), Key: key, From: s.From, To: s.To, Status: status, Units: units, Data: data}
}

func catch(ca, tom, muc, khac string) SourceUnit {
	return SourceUnit{Values: map[string]string{"ca": ca, "tom": tom, "muc": muc, "khac": khac}}
}

func TestBM18_ColumnsCumulativeMissingAndTotals_W1144(t *testing.T) {
	// Tuần CNTS (Thứ Ba – Thứ Hai): 2026-W40 = 29/09–05/10 (ngày cuối thuộc quý IV), 2026-W39 = 22/09–28/09 (quý III).
	w40, w39 := cntsWeek(t, "2026-W40"), cntsWeek(t, "2026-W39")
	if w40.To.Month() != 10 || w39.To.Month() != 9 || !w39.To.AddDate(0, 0, 1).Equal(w40.From) {
		t.Fatalf("tuần: %+v %+v", w39, w40)
	}
	periods := []SourcePeriod{
		weekPeriod(t, "2026-W01", "closed", map[string]SourceUnit{"X1": catch("1", "0", "0", "0")}, "X1", "X2"),
		weekPeriod(t, "2026-W39", "closed", map[string]SourceUnit{"X1": catch("2", "1", "0", "0"), "X2": catch("3", "0", "0", "0.5")}, "X1", "X2"),
		weekPeriod(t, "2026-W40", "closed", map[string]SourceUnit{"X1": catch("4", "0", "1", "0")}, "X1"),
		weekPeriod(t, "2026-W40", "open", map[string]SourceUnit{}, "X2"), // lô thứ hai cùng khóa: X2 chưa nộp
	}
	area, out := dec("10"), dec("5")
	pearl := dec("3")
	aqua := []AquaEntry{
		{WeekKey: "2026-W40", WeekTo: w40.To, Object: "ca_tra", Area: &area, Output: &out},
		{WeekKey: "2026-W39", WeekTo: w39.To, Object: "ca_tra", Output: &out},
		{WeekKey: "2026-W40", WeekTo: w40.To, Object: "ngoc_trai", Output: &pearl},
	}
	r := BM18(BM18Input{Week: w40, Periods: periods, Aqua: aqua, Regions: map[string]string{"X1": "KV1", "X2": "KV2"}})
	if r.Prev == nil || r.Prev.Key != "2026-W39" || r.SourceCount != 2 || r.SourceClosed != 1 || r.AllClosed {
		t.Fatalf("nguồn: prev %+v %d/%d", r.Prev, r.SourceClosed, r.SourceCount)
	}
	if r.UnitsTotal != 2 || r.UnitsReported != 1 || len(r.MissingUnits) != 1 || r.MissingUnits[0] != "X2" {
		t.Fatalf("xã thiếu: %+v", r.MissingUnits)
	}
	x1 := r.Communes[0]
	if x1.Commune != "X1" || !x1.Cols.Cur["tong"].Equal(dec("5")) || !x1.Cols.Prev["tong"].Equal(dec("3")) ||
		!x1.Cols.QTD["tong"].Equal(dec("5")) || !x1.Cols.YTD["tong"].Equal(dec("9")) {
		t.Fatalf("X1: %+v", x1.Cols)
	}
	x2 := r.Communes[1]
	if !x2.Cols.Cur["tong"].IsZero() || !x2.Cols.YTD["tong"].Equal(dec("3.5")) || len(x2.MissingWeeks) != 2 {
		t.Fatalf("X2 (thiếu kỳ W01, W40): %+v %v", x2.Cols, x2.MissingWeeks)
	}
	if !r.Catch.YTD["tong"].Equal(dec("12.5")) || len(r.Weeks) != 3 || len(r.Regions) != 2 {
		t.Fatalf("tổng khai thác: %v weeks %d", r.Catch.YTD, len(r.Weeks))
	}
	// Nuôi trồng: ngọc trai không vào tổng tấn; tổng = khai thác + nuôi trồng.
	if !r.AquaOutput.Cur[AquaKey].Equal(dec("5")) || !r.AquaOutput.YTD[AquaKey].Equal(dec("10")) || !r.AquaArea.Cur[AquaKey].Equal(dec("10")) {
		t.Fatalf("nuôi trồng: %+v", r.AquaOutput)
	}
	if !r.Total.Cur[AquaKey].Equal(dec("10")) || !r.Total.YTD[AquaKey].Equal(dec("22.5")) || !r.Total.Prev[AquaKey].Equal(dec("11.5")) {
		t.Fatalf("tổng sản lượng: %+v", r.Total)
	}
	// Bất biến hợp đồng: Σ rows của ô tong_san_luong = dòng tổng toàn tỉnh (kỳ và lũy kế).
	p := PubBM18(r)
	for _, cell := range []string{"tong_san_luong", "tong_san_luong" + SuffixQTD, "tong_san_luong" + SuffixYTD} {
		sum := decimal.Zero
		for _, row := range p.Rows {
			if s, ok := row.Cells[cell]; ok {
				sum = sum.Add(dec(s))
			}
		}
		prov := p.Totals[len(p.Totals)-1]
		if prov.Kind != "province" || !sum.Equal(dec(prov.Cells[cell])) {
			t.Fatalf("%s: Σ rows %v ≠ tổng %v", cell, sum, prov.Cells[cell])
		}
	}
	if !p.Provisional || len(p.Rows) != 2+len(AquaObjects) {
		t.Fatalf("công bố: %d dòng", len(p.Rows))
	}
	h1, _ := FiguresHash(p.Rows, p.Totals)
	h2, _ := FiguresHash(PubBM18(BM18(BM18Input{Week: w40, Periods: periods, Aqua: aqua, Regions: map[string]string{"X1": "KV1", "X2": "KV2"}})).Rows, p.Totals)
	if h1 == "" || h1 != h2 {
		t.Fatal("băm phải ổn định")
	}
}

func TestBM18_YearBoundaryByWeekEnd_W1144(t *testing.T) {
	// Tuần 1/2027 lịch CNTS = 29/12/2026 – 04/01/2027: lũy kế năm 2027 bắt đầu từ tuần này, tuần trước (W52/2026) chỉ ở
	// cột "Tuần trước liền kề".
	w1 := cntsWeek(t, "2027-W01")
	prevKey := ""
	for _, k := range []string{"2026-W52", "2026-W53"} {
		if s, err := SpanOfKey(CNTSRule, PeriodWeek, k, nil); err == nil && s.To.AddDate(0, 0, 1).Equal(w1.From) {
			prevKey = k
		}
	}
	if prevKey == "" || w1.From.Year() != 2026 {
		t.Fatalf("tuần 1/2027: %+v", w1)
	}
	periods := []SourcePeriod{
		weekPeriod(t, prevKey, "closed", map[string]SourceUnit{"X1": catch("7", "0", "0", "0")}, "X1"),
		weekPeriod(t, "2027-W01", "closed", map[string]SourceUnit{"X1": catch("2", "0", "0", "0")}, "X1"),
	}
	r := BM18(BM18Input{Week: w1, Periods: periods})
	c := r.Communes[0].Cols
	if !c.Prev["ca"].Equal(dec("7")) || !c.YTD["ca"].Equal(dec("2")) || !c.QTD["ca"].Equal(dec("2")) || !r.AllClosed {
		t.Fatalf("vắt năm: %+v", c)
	}
}

func TestAggIDsStable_W1144(t *testing.T) {
	tid := uuid.New()
	a := AggPeriodID(tid, "89", CNTSBM18, AggWeek, "2026-W40")
	if a != AggPeriodID(tid, "89", CNTSBM18, AggWeek, "2026-W40") || a == AggPeriodID(tid, "89", CNTSBM18, AggWeek, "2026-W41") {
		t.Fatal("mã kỳ tổng hợp phải ổn định và phân biệt khóa")
	}
	if AggDefinitionID(CNTSBM02) == AggDefinitionID(CNTSBM18) {
		t.Fatal("mã định nghĩa phân biệt biểu")
	}
	for _, o := range AquaObjects {
		if o.Group != "" {
			found := false
			for _, g := range AquaGroups {
				found = found || g.Key == o.Group
			}
			if !found {
				t.Fatalf("nhóm %s chưa khai", o.Group)
			}
		}
	}
}
