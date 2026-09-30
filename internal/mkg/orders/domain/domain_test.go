package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// proto createGoodsReceipt (T-2): > ngưỡng (không phải ≥) mới cần biên bản; cả hai đơn vị tấn/cuộn; dấu
// của chênh lệch giữ nguyên; làm tròn nửa lên 2 số.
func TestCheckReceiptThresholdBothUnits_T2(t *testing.T) {
	cases := []struct {
		unit, ordered, received string
		pct                     string
		needs                   bool
	}{
		{UnitKg, "10000", "10500", "5.00", true},
		{UnitKg, "10000", "10300", "3.00", false},
		{UnitKg, "10000", "10300.5", "3.01", true},
		{UnitKg, "10000", "9699.999", "-3.00", false}, // −3,00001 % làm tròn −3,00 — so sánh trên số đã làm tròn
		{UnitKg, "10000", "9600", "-4.00", true},
		{UnitBale, "500", "480", "-4.00", true},
		{UnitBale, "500", "490", "-2.00", false},
		{UnitBale, "3", "4", "33.33", true},
	}
	for _, c := range cases {
		v := CheckReceipt(c.unit, d(c.ordered), d(c.received), d("3"), "k:default")
		if v.Pct.StringFixed(2) != c.pct || v.NeedsReport != c.needs || (v.NeedsReport && v.AuditNote == "") {
			t.Errorf("%s %s→%s: %s %v (muốn %s %v)", c.unit, c.ordered, c.received, v.Pct.StringFixed(2), v.NeedsReport, c.pct, c.needs)
		}
	}
}

// O-09 / R-10: tiền = kg/1000 × đơn giá/tấn hoặc cuộn × đơn giá/cuộn, làm tròn nửa lên 2 số — decimal, không float.
func TestLineAmountDecimalRounding_O09(t *testing.T) {
	p := func(s string) money.Price {
		x, err := money.ParsePrice(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	a, err := KgAmount(mass.MustKg("19850.5"), p("1250000.5"))
	if err != nil || a.String() != "24813134.93" {
		t.Fatalf("kg: %s %v", a, err)
	}
	a, err = LineAmount(UnitBale, d("480"), p("12000.1234"))
	if err != nil || a.String() != "5760059.23" {
		t.Fatalf("cuộn: %s %v", a, err)
	}
	// 0,1 + 0,2 kiểu float sẽ lệch; decimal thì đúng.
	a, err = KgAmount(mass.MustKg("0.3"), p("1000"))
	if err != nil || a.String() != "0.30" {
		t.Fatalf("0.3 kg: %s", a)
	}
	n, err := NetPrice(p("1500000"), d("5"))
	if err != nil || n.String() != "1425000.0000" {
		t.Fatalf("net: %s %v", n, err)
	}
}

// proto CT-02 / CT-03.
func TestValidateContract_CT02_CT03(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ok := ContractSpec{Basis: port.PricingPerTonne, UnitPrice: money.Price{}, CommittedKg: mass.MustKg("1"), ValidFrom: from, ValidTo: from}
	pr, _ := money.ParsePrice("650000")
	ok.UnitPrice = pr
	if err := ValidateContract(ok); err != nil {
		t.Fatalf("hợp lệ (kết thúc = bắt đầu): %v", err)
	}
	bad := ok
	bad.ValidTo = from.AddDate(0, 0, -1)
	if ValidateContract(bad) == nil {
		t.Error("kết thúc trước bắt đầu")
	}
	bad = ok
	bad.Basis = "theo_kg"
	if ValidateContract(bad) == nil {
		t.Error("cách tính lạ")
	}
	bad = ok
	bad.CommittedKg = mass.MustKg("0")
	if ValidateContract(bad) == nil {
		t.Error("cam kết 0")
	}
	m := d("100.5")
	bad = ok
	bad.MaxMoisturePct = &m
	if ValidateContract(bad) == nil {
		t.Error("ẩm > 100")
	}
	if !Overlaps(from, from.AddDate(0, 0, 10), from.AddDate(0, 0, 10), from.AddDate(0, 0, 20)) ||
		Overlaps(from, from.AddDate(0, 0, 10), from.AddDate(0, 0, 11), from.AddDate(0, 0, 20)) {
		t.Error("CT-01 chồng lấn theo ngày, hai đầu tính")
	}
}

// proto advanceSalesOrder (O-05): chỉ tiến; hủy chỉ từ draft / confirmed.
func TestSalesOrderForwardOnly_O05Domain(t *testing.T) {
	for _, c := range []struct {
		from, to string
		ok       bool
	}{
		{SODraft, SOConfirmed, true}, {SOConfirmed, SODelivering, true}, {SODelivering, SOCompleted, true},
		{SODelivered, SODelivering, false}, {SOConfirmed, SOConfirmed, false}, {SODraft, SOCancelled, true},
		{SODelivering, SOCancelled, false}, {SOCancelled, SOConfirmed, false},
	} {
		if CanAdvance(c.from, c.to) != c.ok {
			t.Errorf("%s → %s", c.from, c.to)
		}
	}
}

// O-06: ngưỡng giá / chiết khấu (mặc định tạm 0 — chờ khách).
func TestCheckPriceApproval_O06(t *testing.T) {
	p := func(s string) money.Price { x, _ := money.ParsePrice(s); return x }
	list := p("1000000")
	if r := CheckPriceApproval(&list, p("1000000"), d("0"), d("0"), d("0")); r.Needed {
		t.Error("đúng giá bảng, không chiết khấu → không cần duyệt")
	}
	if r := CheckPriceApproval(&list, p("1000000"), d("0.5"), d("0"), d("0")); !r.Needed {
		t.Error("chiết khấu > 0 với ngưỡng 0 → cần duyệt")
	}
	if r := CheckPriceApproval(&list, p("950000"), d("0"), d("5"), d("0")); r.Needed || r.DeltaPct.StringFixed(2) != "5.00" {
		t.Error("lệch đúng 5 % với ngưỡng 5 → không cần")
	}
	if r := CheckPriceApproval(&list, p("940000"), d("0"), d("5"), d("0")); !r.Needed {
		t.Error("lệch 6 % > 5 → cần")
	}
	if r := CheckPriceApproval(nil, p("940000"), d("0"), d("0"), d("0")); r.Needed {
		t.Error("không có giá bảng → chỉ xét chiết khấu")
	}
}
