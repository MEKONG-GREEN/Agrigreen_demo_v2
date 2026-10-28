package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dd(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// CT-05: giá thành / kg = tổng chi phí / sản lượng; lợi nhuận = sản lượng × giá bán + thu khác − tổng chi phí.
func TestCT05(t *testing.T) {
	c := CT05(FormFigures{AreaHa: dd("2"), OutputKg: dd("12000"), SalePrice: dd("7000"), OtherIncome: dd("500000"),
		Costs: map[string]decimal.Decimal{"seed": dd("2400000"), "fertilizer": dd("8400000"), "pesticide": dd("3000000"),
			"labor": dd("6000000"), "machine": dd("3600000"), "other": dd("1000000")}})
	if !c.TotalCost.Equal(dd("24400000")) || !c.CostPerKg.Equal(dd("2033.3333")) || !c.YieldPerHa.Equal(dd("6000")) {
		t.Fatalf("CT-05 sai: %+v", c)
	}
	if !c.Profit.Equal(dd("60100000")) || !c.ProfitPerHa.Equal(dd("30050000")) || c.MarginPct == nil || !c.MarginPct.Equal(dd("246.3115")) {
		t.Fatalf("lợi nhuận sai: %+v", c)
	}
}

// BG-FN-TW-01.04 AC-01: 5.274, 5.580, 5.384 → bình quân 5.413 (làm tròn đồng), trung vị 5.384.
func TestMedianMean(t *testing.T) {
	vs := []decimal.Decimal{dd("5274"), dd("5580"), dd("5384")}
	m, ok := Mean(vs)
	if !ok || !m.Round(0).Equal(dd("5413")) || !Median(vs).Equal(dd("5384")) {
		t.Fatalf("bình quân %s, trung vị %s", m, Median(vs))
	}
	if !Median([]decimal.Decimal{dd("1"), dd("4"), dd("2"), dd("3")}).Equal(dd("2.5")) || Median(nil) != nil {
		t.Fatal("trung vị số chẵn phần tử / rỗng sai")
	}
}

// BG-FN-TI-02.12 AC-01 (41 phiếu Xoài keo trung vị 12.000, phiếu 20.000 = +66,7 % → cờ), AC-02 (Mít Thái 25.000 khi trung
// vị 10.000 → Cao bất thường), ngưỡng ≤ −40 % → Thấp, dưới 5 phiếu → không trung vị, không cờ.
func TestCT06(t *testing.T) {
	var xoai []decimal.Decimal // 20 × 11.000, 12.000, 19 × 12.500, 20.000 = 41 phiếu, trung vị 12.000
	for i := 0; i < 20; i++ {
		xoai = append(xoai, dd("11000"))
	}
	xoai = append(xoai, dd("12000"))
	for i := 0; i < 19; i++ {
		xoai = append(xoai, dd("12500"))
	}
	xoai = append(xoai, dd("20000"))
	d := CT06(dd("20000"), xoai, dd("40"))
	if d.Flag != FlagHigh || !d.Median.Equal(dd("12000")) || !d.Pct.Round(1).Equal(dd("66.7")) {
		t.Fatalf("Xoài keo: %+v", d)
	}
	mit := []decimal.Decimal{dd("10000"), dd("10000"), dd("10000"), dd("10000"), dd("25000")}
	if d := CT06(dd("25000"), mit, dd("40")); d.Flag != FlagHigh || !d.Pct.Equal(dd("150")) {
		t.Fatalf("Mít Thái: %+v", d)
	}
	if d := CT06(dd("6000"), mit, dd("40")); d.Flag != FlagLow {
		t.Fatalf("−40 %% phải là Thấp bất thường: %+v", d)
	}
	if d := CT06(dd("13000"), mit, dd("40")); d.Flag != "" || d.Median == nil {
		t.Fatalf("+30 %% không cờ: %+v", d)
	}
	if d := CT06(dd("25000"), mit[:4], dd("40")); d.Flag != "" || d.Median != nil {
		t.Fatalf("dưới 5 phiếu không tính trung vị: %+v", d)
	}
}

// BG-FN-TI-02.11 AC-01: "Dưa leo", "dưa leo", "DƯA LEO" cùng một khóa; tên xã bỏ tiền tố.
func TestFold(t *testing.T) {
	for _, s := range []string{"Dưa leo", "dưa leo", "DƯA LEO", "  dưa   LEO "} {
		if Fold(s) != "dua leo" {
			t.Fatalf("Fold(%q) = %q", s, Fold(s))
		}
	}
	if FoldCommune("Xã Núi Cấm") != "nui cam" || FoldCommune("Phường Mỹ Bình") != "my binh" || Fold("Đồng Tháp") != "dong thap" {
		t.Fatal("FoldCommune sai")
	}
	if ParseCrop("Cây ăn trái") != CropFruit || ParseCrop("LÚA") != CropRice || ParseCrop("Nấm") != CropMush || ParseCrop("xx") != "" {
		t.Fatal("ParseCrop sai")
	}
}

func TestParseNumber(t *testing.T) {
	cases := map[string]string{"25.000.000": "25000000", "1,2": "1.2", "1.5": "1.5", "12 000 đ": "12000", "1.234,5": "1234.5",
		"1,234.5": "1234.5", "7000": "7000", "1,200,000": "1200000", "0": "0"}
	for in, want := range cases {
		got, ok := ParseNumber(in)
		if !ok || !got.Equal(dd(want)) {
			t.Fatalf("ParseNumber(%q) = %s, %v (muốn %s)", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "1..2"} {
		if _, ok := ParseNumber(bad); ok {
			t.Fatalf("ParseNumber(%q) phải lỗi", bad)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{"0912 000 111": "84912000111", "+84912000111": "84912000111", "912000111": "84912000111"} {
		if got, ok := NormalizePhone(in); !ok || got != want {
			t.Fatalf("NormalizePhone(%q) = %q", in, got)
		}
	}
	if _, ok := NormalizePhone("12345"); ok {
		t.Fatal("SĐT ngắn phải lỗi")
	}
}

// BR-03 FN-TI-02.10: hết ngày kết thúc thu thập → Đang rà soát (phép chiếu).
func TestRoundState(t *testing.T) {
	to := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	if RoundState(RoundCollecting, to, to) != RoundCollecting || RoundState(RoundCollecting, to, to.AddDate(0, 0, 1)) != RoundReviewing ||
		RoundState(RoundDraft, to, to.AddDate(0, 0, 9)) != RoundDraft {
		t.Fatal("RoundState sai")
	}
}
