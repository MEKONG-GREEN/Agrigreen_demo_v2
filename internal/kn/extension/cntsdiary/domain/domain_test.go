package domain

import (
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dp(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

func codes(cs []Check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Code)
	}
	return out
}

var d0 = time.Date(2027, 10, 12, 0, 0, 0, 0, time.UTC)

func TestEntryNormalize(t *testing.T) {
	e := Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "100", "feed_kg": "3.25"}}
	if es := e.Normalize(FormBM08); len(es) > 0 || e.Metrics["dead"] != "0" {
		t.Fatalf("NC-06 mặc định 0: %v %v", es, e.Metrics)
	}
	bad := Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "-1", "feed_kg": "1.234", "ph": "7"}}
	if es := bad.Normalize(FormBM08); len(es) != 3 {
		t.Fatalf("lỗi chỉ số: %v", es)
	}
	vit := Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "10", "feed_kg": "1"},
		Tables: Tables{Sales: []Sale{{Date: "2027-10-12", Heads: 1, WeightKg: "2", UnitPrice: "1"}}}}
	if es := vit.Normalize(FormBM09); len(es) != 1 || es[0].Field != "tables.sales" {
		t.Fatalf("sổ vịt xiêm không có xuất bán: %v", es)
	}
	fish := Entry{Kind: KindGrowthEnv, EntryDate: d0, Metrics: map[string]string{"loss": "1", "ph": "7.55", "do_mgl": "4", "feed_kg": "2"}}
	if es := fish.Normalize(FormBM19); len(es) != 4 { // thiếu NT-04, NT-05, NT-06; pH 1 chữ số thập phân
		t.Fatalf("BM-19 cân đo: %v", es)
	}
	if es := (&Entry{Kind: KindDaily, Metrics: map[string]string{}}).Normalize(FormBM19); len(es) != 1 {
		t.Fatalf("loại lần ghi theo mẫu: %v", es)
	}
}

func TestEvaluatePoultry(t *testing.T) {
	in := EvalInput{Form: FormBM08, InitialCount: 100, Location: Location{HasFix: true, DistanceM: dp("20")},
		Entry: Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "95", "dead": "2"},
			Tables: Tables{Sales: []Sale{{Date: "2027-10-12", Heads: 3}, {Date: "2027-10-11", Heads: 50}},
				Treatments: []Treatment{{Date: "2027-10-10", Drug: "X", WithdrawalDays: 3}}}},
		Thresholds: Thresholds{MortalityMaxPct: dp("1")}}
	got := codes(Evaluate(in))
	for _, c := range []string{"death_cause_missing", "mortality_high", "withdrawal_period"} {
		if !slices.Contains(got, c) {
			t.Fatalf("thiếu %s: %v", c, got)
		}
	}
	if slices.Contains(got, "headcount_mismatch") {
		t.Fatalf("95 = 100 − 2 − 3 (chỉ tính bán trong ngày): %v", got)
	}
	in.Prev = &Previous{EntryDate: d0.AddDate(0, 0, -1), Metrics: map[string]string{"alive": "99"}}
	if got := codes(Evaluate(in)); !slices.Contains(got, "headcount_mismatch") {
		t.Fatalf("lệch số con sống: %v", got)
	}
	in.Entry.Metrics["weight_avg_g"] = "500"
	in.PrevWeight = &Previous{EntryDate: d0.AddDate(0, 0, -7), Metrics: map[string]string{"weight_avg_g": "520"}}
	if got := codes(Evaluate(in)); !slices.Contains(got, "low_growth") || !slices.Contains(got, "growth_pct") {
		t.Fatalf("tăng trọng: %v", got)
	}
}

// BF-CN-CATALOG #170: ngưỡng theo chỉ tiêu (không chỉ pH, DO) — nhiệt độ, độ mặn vượt ngưỡng sinh cảnh báo môi trường nước.
func TestEvaluateIndicatorThresholds(t *testing.T) {
	th := Thresholds{PHMin: dp("7.5"), PHMax: dp("8.5"), DOMin: dp("4"), DOMax: dp("9"),
		Others: map[string]Range{"temp_c": {Min: dp("25"), Max: dp("32")}, "salinity": {Max: dp("5")}}}
	in := EvalInput{Form: FormBM19, Thresholds: th, MaxDistM: decimal.NewFromInt(200), MaxAccM: decimal.NewFromInt(50),
		Entry: Entry{Kind: KindEnv, EntryDate: d0, Metrics: map[string]string{"ph": "7.8", "do_mgl": "9.5", "temp_c": "33.5", "salinity": "4"}}}
	cs := Evaluate(in)
	got := codes(cs)
	for _, c := range []string{"do_high", "temp_c_out_of_range", "action_note_missing"} {
		if !slices.Contains(got, c) {
			t.Fatalf("thiếu %s: %v", c, got)
		}
	}
	if slices.Contains(got, "salinity_out_of_range") || slices.Contains(got, "ph_out_of_range") {
		t.Fatalf("trong ngưỡng không cảnh báo: %v", got)
	}
	for _, c := range cs {
		if c.Code == "temp_c_out_of_range" && (c.Alert != AlertWater || c.Message != "Nhiệt độ 33.5 °C ngoài ngưỡng 25–32 °C") {
			t.Fatalf("cảnh báo nhiệt độ: %+v", c)
		}
	}
	in.Entry.Metrics = map[string]string{"ph": "7.8", "do_mgl": "5", "temp_c": "30", "salinity": "6", "action_note": "Thay nước"}
	if got := codes(Evaluate(in)); !slices.Contains(got, "salinity_out_of_range") || slices.Contains(got, "temp_c_out_of_range") || slices.Contains(got, "action_note_missing") {
		t.Fatalf("độ mặn vượt ngưỡng trên: %v", got)
	}
	in.Thresholds.Others = nil // chưa đặt ngưỡng → không cảnh báo
	if got := codes(Evaluate(in)); slices.Contains(got, "salinity_out_of_range") {
		t.Fatalf("không ngưỡng: %v", got)
	}
}

func TestEvaluateAquaticAndLocation(t *testing.T) {
	ph := Thresholds{PHMin: dp("7.5"), PHMax: dp("8.5"), DOMin: dp("4")}
	in := EvalInput{Form: FormBM19, Thresholds: ph, MaxDistM: decimal.NewFromInt(200), MaxAccM: decimal.NewFromInt(50),
		Entry: Entry{Kind: KindGrowthEnv, EntryDate: d0, Metrics: map[string]string{"ph": "8.7", "do_mgl": "3.5"}}}
	got := codes(Evaluate(in))
	for _, c := range []string{"ph_out_of_range", "do_low", "action_note_missing", "photo_missing", "no_fix"} {
		if !slices.Contains(got, c) {
			t.Fatalf("thiếu %s: %v", c, got)
		}
	}
	if b := Blocking(Evaluate(in)); len(b) != 2 {
		t.Fatalf("chặn: %v", b)
	}
	target := &Point{10.38, 105.43}
	near := EvaluateLocation(&Fix{Lat: 10.3805, Lng: 105.4305, AccuracyM: dp("8")}, target, decimal.NewFromInt(200), decimal.NewFromInt(50), nil)
	if !near.HasFix || near.Far || near.MockSuspected || near.DistanceM == nil || near.DistanceM.GreaterThan(decimal.NewFromInt(100)) {
		t.Fatalf("gần điểm: %+v", near)
	}
	far := EvaluateLocation(&Fix{Lat: 10.40, Lng: 105.43, AccuracyM: dp("0")}, target, decimal.NewFromInt(200), decimal.NewFromInt(50),
		[]Point{{10.40, 105.43}})
	if !far.Far || !far.MockSuspected || len(far.MockReasons) != 2 {
		t.Fatalf("xa, nghi giả lập (sai số 0, trùng tọa độ): %+v", far)
	}
	none := EvaluateLocation(&Fix{Lat: 10.4, Lng: 105.4, AccuracyM: dp("80")}, nil, decimal.NewFromInt(200), decimal.NewFromInt(50), nil)
	if !none.NoTargetPoint || !none.WeakAccuracy {
		t.Fatalf("điểm chưa có tọa độ, GPS yếu: %+v", none)
	}
	if d := DistanceM(Point{10, 105}, Point{10.01, 105}); d < 1100 || d > 1125 {
		t.Fatalf("haversine: %f", d)
	}
}

func TestTransitionAndAccounting(t *testing.T) {
	cases := []struct{ cur, act, next string }{
		{StatusDraft, ActSubmit, StatusSubmitted}, {StatusSubmitted, ActReturn, StatusReturned}, {StatusReturned, ActSubmit, StatusSubmitted},
		{StatusSubmitted, ActVerify, StatusVerified}, {StatusVerified, ActCorrect, StatusSubmitted}, {StatusSubmitted, ActUpdate, StatusSubmitted},
	}
	for _, c := range cases {
		if _, n, ok := Transition(c.cur, c.act); !ok || n != c.next {
			t.Fatalf("%s/%s → %s, %v", c.cur, c.act, n, ok)
		}
	}
	for _, c := range [][2]string{{StatusVerified, ActUpdate}, {StatusDraft, ActVerify}, {StatusReturned, ActVerify}, {StatusSubmitted, ActCorrect}} {
		if _, _, ok := Transition(c[0], c[1]); ok {
			t.Fatalf("%s/%s phải bị chặn", c[0], c[1])
		}
	}
	a := Accounting{CostSeed: dp("1000"), CostFeed: dp("2000"), OutputKg: dp("3"), Revenue: dp("5000")}
	if es := a.Normalize(); len(es) > 0 || !a.TotalCost().Equal(decimal.NewFromInt(3000)) || !a.Profit().Equal(decimal.NewFromInt(2000)) ||
		!a.UnitCost().Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("NC-15: %v", es)
	}
	if es := (&Accounting{CostSeed: dp("1.5")}).Normalize(); len(es) != 2 {
		t.Fatalf("tổng thu bắt buộc, số đồng nguyên: %v", es)
	}
	tb := Tables{FeedPurchases: []FeedPurchase{{QtyKg: "25", UnitPrice: "12000"}}, Sales: []Sale{{Heads: 10, WeightKg: "18", UnitPrice: "70000"}}}
	if h, kg, amt := tb.SalesSum(); h != 10 || !kg.Equal(decimal.NewFromInt(18)) || !amt.Equal(decimal.NewFromInt(1260000)) ||
		!tb.FeedCost().Equal(decimal.NewFromInt(300000)) {
		t.Fatalf("gợi ý hạch toán: %d %s %s %s", h, kg, amt, tb.FeedCost())
	}
}

// BF-CN-DIARY (#109): đủ trường sổ gốc BM-08/BM-09 2026 — mục 1 vật tư (vật tư + chủng loại), mục 4 mua nguyên liệu thức
// ăn, mục 5 ghi chú tiêm phòng (không có cột "lần"), phần 3 họ tên cán bộ.
func TestPaperFormFields(t *testing.T) {
	cy := Cycle{Form: FormBM09, Breed: "Vịt xiêm Pháp", ReceivedDate: d0, InitialCount: 300,
		Supplies: []Supply{{Item: "Thức ăn vịt con", Date: "2027-10-12", Quantity: "200", Unit: "kg"}, {Date: "2027-10-12", Kind: "Cám 551", Quantity: "1"}, {Date: "2027-10-12", Quantity: "1"}}}
	es := cy.Normalize(d0)
	if len(es) != 1 || es[0].Field != "supplies[2].item" {
		t.Fatalf("vật tư hoặc chủng loại bắt buộc (dòng cũ chỉ có chủng loại vẫn hợp lệ): %v", es)
	}
	for _, form := range []string{FormBM08, FormBM09} {
		en := Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "10", "feed_kg": "1"}, Tables: Tables{
			FeedPurchases: []FeedPurchase{{Date: "2027-10-12", Name: "Cám vịt con", QtyKg: "25", UnitPrice: "12000"}},
			FeedMaterials: []FeedPurchase{{Date: "2027-10-12", Name: "Bắp", QtyKg: "10.5", UnitPrice: "8000", Note: "mua chợ"}},
			Vaccinations:  []Vaccination{{Vaccine: "Dịch tả vịt", Date: "2027-10-12", Dosage: "1 liều", Method: "tiêm dưới da", MfgDate: "2027-09-01", Note: "lô A"}},
			OfficerNotes:  []OfficerNote{{Date: "2027-10-12", Officer: "Nguyễn Văn A", Content: "Vệ sinh máng", FarmerOpinion: "Đồng ý"}},
		}}
		if es := en.Normalize(form); len(es) > 0 {
			t.Fatalf("%s đủ trường sổ gốc: %v", form, es)
		}
		if got := en.Tables.FeedCost().String(); got != "384000" { // 25×12.000 + 10,5×8.000
			t.Fatalf("%s chi thức ăn gồm mục 3 + mục 4: %s", form, got)
		}
	}
	bad := Entry{Kind: KindDaily, EntryDate: d0, Metrics: map[string]string{"alive": "10", "feed_kg": "1"}, Tables: Tables{
		FeedMaterials: []FeedPurchase{{Date: "x", QtyKg: "-1", UnitPrice: "1"}},
		Vaccinations:  []Vaccination{{Vaccine: "V", Shot: 21, Date: "2027-10-12"}}}}
	es = bad.Normalize(FormBM08)
	var fields []string
	for _, x := range es {
		fields = append(fields, x.Field)
	}
	want := []string{"tables.feed_materials[0].date", "tables.feed_materials[0].name", "tables.feed_materials[0].quantity_kg", "tables.vaccinations[0].shot"}
	if !slices.Equal(fields, want) {
		t.Fatalf("lỗi trường: %v", fields)
	}
	fish := Entry{Kind: KindEnv, EntryDate: d0, Metrics: map[string]string{"loss": "0", "ph": "7.8", "do_mgl": "5", "feed_kg": "1"},
		Tables: Tables{FeedMaterials: []FeedPurchase{{Date: "2027-10-12", Name: "Bắp", QtyKg: "1", UnitPrice: "1"}}}}
	if es := fish.Normalize(FormBM19); len(es) != 1 || es[0].Field != "tables.feed_materials" {
		t.Fatalf("BM-19 không có mục nguyên liệu thức ăn: %v", es)
	}
}

// #109 (Codex P2): "Tuần tuổi (khi cân)" phi thực tế bị chặn ở server; tuần bình thường và quá tuần 12 vẫn nhận.
func TestEntryAgeWeekBound(t *testing.T) {
	entry := func(w string) *Entry {
		return &Entry{Kind: KindDaily, Metrics: map[string]string{"alive": "100", "feed_kg": "1", "age_week": w}}
	}
	for _, form := range []string{FormBM08, FormBM09} {
		for _, w := range []string{"0", "1", "6", "12", "13", "14", "20", "52", "104"} {
			e := entry(w)
			if es := e.Normalize(form); len(es) > 0 || e.Metrics["age_week"] != w {
				t.Fatalf("%s age_week=%s: %v", form, w, es)
			}
		}
		for _, w := range []string{"105", "10000", "100000000"} {
			es := entry(w).Normalize(form)
			if len(es) != 1 || es[0].Field != "metrics.age_week" {
				t.Fatalf("%s age_week=%s phải bị chặn: %v", form, w, es)
			}
		}
	}
	// Vượt cả giới hạn số nguyên chung: một lỗi, không trùng.
	if es := entry("100000001").Normalize(FormBM08); len(es) != 1 || es[0].Field != "metrics.age_week" {
		t.Fatalf("age_week quá lớn: %v", es)
	}
	// BM-19 không có chỉ số tuần tuổi.
	fish := &Entry{Kind: KindGrowthEnv, Metrics: map[string]string{"age_week": "5"}}
	if es := fish.Normalize(FormBM19); !slices.ContainsFunc(es, func(x FieldErr) bool { return x.Field == "metrics.age_week" }) {
		t.Fatalf("BM-19 phải từ chối age_week: %v", es)
	}
}
