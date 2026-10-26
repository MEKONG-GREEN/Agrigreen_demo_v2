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
