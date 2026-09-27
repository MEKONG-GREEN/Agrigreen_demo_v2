package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func dp(s string) *decimal.Decimal { v := d(s); return &v }

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

var brdThresholds = Thresholds{
	Sufficient: &ParamRef{Key: "s", Value: d("85"), Version: 1},
	Attention:  &ParamRef{Key: "a", Value: d("60"), Version: 1},
	Surplus:    &ParamRef{Key: "t", Value: d("120"), Version: 1},
}

// FN-04/FN-05 BR-02: mã CSH-/MAY-<tên viết tắt tỉnh>-00001; không gắn tỉnh → XX.
func TestProvinceLabelAndCodeFormat(t *testing.T) {
	cases := map[string]string{
		"An Giang": "AG", "Cần Thơ": "CT", "Thành phố Cần Thơ": "CT", "Đồng Tháp": "DT", "Tỉnh Vĩnh Long": "VL",
		"Hồ Chí Minh": "HCM", "Huế": "HU", "": NoProvince,
	}
	for name, want := range cases {
		if got := ProvinceLabel(name); got != want {
			t.Errorf("ProvinceLabel(%q) = %q, muốn %q", name, got, want)
		}
	}
	if got := FormatCode(PrefixMachine, "AG", 1); got != "MAY-AG-00001" {
		t.Fatalf("mã máy: %s", got)
	}
	if got := FormatCode(PrefixOwner, NoProvince, 42); got != "CSH-XX-00042" {
		t.Fatalf("mã CSH: %s", got)
	}
	if got := FormatCode(PrefixMachine, "CT", 123456); got != "MAY-CT-123456" {
		t.Fatalf("quá 5 chữ số vẫn không cắt: %s", got)
	}
}

// QT-02: mốc đúng bằng 60/85/120 thuộc mức cao hơn; không giới hạn 100%.
func TestClassifyBoundariesQT02(t *testing.T) {
	cases := []struct {
		pct  string
		want Coverage
	}{
		{"0", CoverageShortage}, {"59.9999", CoverageShortage}, {"60", CoverageAttention}, {"84.9999", CoverageAttention},
		{"85", CoverageSufficient}, {"119.9999", CoverageSufficient}, {"120", CoverageSurplus}, {"350", CoverageSurplus},
	}
	for _, c := range cases {
		if got := Classify(d(c.pct), brdThresholds); got != c.want {
			t.Errorf("Classify(%s) = %s, muốn %s", c.pct, got, c.want)
		}
	}
	if got := Classify(d("90"), Thresholds{Sufficient: brdThresholds.Sufficient}); got != CoverageUnclassified {
		t.Fatalf("thiếu ngưỡng → chưa phân loại, nhận %s", got)
	}
}

// QT-01: số máy cần = diện tích ÷ định mức; mức đáp ứng = năng lực ÷ nhu cầu.
func TestComputeBalanceQT01(t *testing.T) {
	norm := &ParamRef{Key: "n", Value: d("100"), Version: 3}
	cases := []struct {
		area     string
		capacity int
		pct      string
		whole    int
		gap      int
		status   Coverage
	}{
		{"500", 3, "60", 5, -2, CoverageAttention},     // đúng mốc 60
		{"2000", 17, "85", 20, -3, CoverageSufficient}, // đúng mốc 85
		{"500", 6, "120", 5, 1, CoverageSurplus},       // đúng mốc 120 — không cắt ở 100
		{"250", 2, "80", 3, -1, CoverageAttention},     // 2,5 máy → cần 3 (làm tròn lên để hiển thị)
		{"500", 0, "0", 5, -5, CoverageShortage},
	}
	for _, c := range cases {
		f := ComputeBalance(BalanceInput{AreaHa: dp(c.area), Norm: norm, Capacity: c.capacity, MachinesTotal: c.capacity, Thresholds: brdThresholds})
		if f.Status != c.status || !f.CoveragePct.Equal(d(c.pct)) || *f.RequiredWhole != c.whole || *f.Gap != c.gap {
			t.Errorf("diện tích %s, năng lực %d: %+v (pct %s)", c.area, c.capacity, f, f.CoveragePct)
		}
	}
	if f := ComputeBalance(BalanceInput{AreaHa: nil, Norm: norm, Capacity: 4}); f.Status != CoverageNoData || f.Required != nil {
		t.Fatalf("không có diện tích → Chưa có dữ liệu: %+v", f)
	}
	if f := ComputeBalance(BalanceInput{AreaHa: dp("0"), Norm: norm}); f.Status != CoverageNoData {
		t.Fatalf("diện tích 0 → Chưa có dữ liệu: %+v", f)
	}
	if f := ComputeBalance(BalanceInput{AreaHa: dp("10"), Norm: nil}); f.Status != CoverageNoNorm {
		t.Fatalf("thiếu định mức → no_norm: %+v", f)
	}
	f := ComputeBalance(BalanceInput{AreaHa: dp("0.0001"), Norm: &ParamRef{Value: d("400")}, Capacity: 9, Thresholds: brdThresholds})
	if !f.CoveragePct.Equal(MaxPct) || f.Status != CoverageSurplus {
		t.Fatalf("mức đáp ứng bão hòa ở numeric(9,4): %+v", f)
	}
}

// FN-05 BR-09: Ngày HTX sở hữu ≤ T và (chưa vô hiệu hóa hoặc Ngày vô hiệu hóa > T).
func TestCountsAtBR09(t *testing.T) {
	owned := day("2026-03-15")
	deact := day("2026-06-01")
	cases := []struct {
		at    string
		deact *time.Time
		want  bool
	}{
		{"2026-03-14", nil, false}, {"2026-03-15", nil, true}, {"2027-01-01", nil, true},
		{"2026-05-31", &deact, true}, {"2026-06-01", &deact, false}, {"2026-07-01", &deact, false},
	}
	for _, c := range cases {
		if got := CountsAt(owned, c.deact, day(c.at)); got != c.want {
			t.Errorf("CountsAt(T=%s, deact=%v) = %v", c.at, c.deact, got)
		}
	}
}

// QT-03: App HTX ưu tiên, trừ bản ghi đã khóa; nhập tay luôn được áp.
func TestResolveAreaQT03(t *testing.T) {
	if ok, _ := ResolveArea(nil, AreaFromAppHTX); !ok {
		t.Fatal("chưa có bản ghi → nhận App HTX")
	}
	manual := &SeasonArea{Source: AreaManual}
	if ok, _ := ResolveArea(manual, AreaFromAppHTX); !ok {
		t.Fatal("App HTX có lại dữ liệu → ưu tiên trở lại")
	}
	manual.Locked = true
	if ok, reason := ResolveArea(manual, AreaFromAppHTX); ok || reason != ReasonLocked {
		t.Fatal("bản ghi đã khóa không bị ghi đè")
	}
	if ok, _ := ResolveArea(manual, AreaManual); !ok {
		t.Fatal("Admin vẫn sửa được bản ghi khóa")
	}
}

func TestValidateCalendar(t *testing.T) {
	ok := []CalendarEntry{{Stage: "gieo_sa", Start: day("2026-11-01"), End: day("2026-11-20")}, {Stage: "thu_hoach", Start: day("2027-02-01"), End: day("2027-02-20")}}
	if err := ValidateCalendar(ok); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCalendar([]CalendarEntry{{Stage: "gieo_sa", Start: day("2026-11-20"), End: day("2026-11-20")}}); err == nil {
		t.Fatal("ngày kết thúc phải sau ngày bắt đầu")
	}
	overlap := append(ok, CalendarEntry{Stage: "gieo_sa", Start: day("2026-11-15"), End: day("2026-11-30")})
	if err := ValidateCalendar(overlap); err == nil {
		t.Fatal("khâu chồng lấn phải bị chặn")
	}
}

// Tổng hợp: mức của đơn vị là khâu thấp nhất; nhu cầu, năng lực là tổng.
func TestSummarizeBottleneck(t *testing.T) {
	rows := []ResultRow{
		{Stage: "lam_dat", MachineType: "MLD", Required: dp("5"), Capacity: 2, MachinesTotal: 3, Status: CoverageShortage},
		{Stage: "thu_hoach", MachineType: "MGD", Required: dp("2"), Capacity: 4, MachinesTotal: 4, Status: CoverageSurplus},
		{Stage: "thu_hoach", MachineType: "MGX", Status: CoverageNoNorm},
	}
	all, stages := Summarize(rows, brdThresholds)
	if all.Status != CoverageShortage || !all.CoveragePct.Equal(d("40")) || !all.Required.Equal(d("7")) || all.Capacity != 6 || all.MachinesTotal != 7 {
		t.Fatalf("tổng thể: %+v", all)
	}
	if g := stages["thu_hoach"]; g.Status != CoverageSurplus || !g.CoveragePct.Equal(d("200")) {
		t.Fatalf("khâu thu hoạch: %+v", g)
	}
	none, _ := Summarize([]ResultRow{{Stage: "lam_dat", Status: CoverageNoData}}, brdThresholds)
	if none.Status != CoverageNoData || none.CoveragePct != nil {
		t.Fatalf("không có dữ liệu: %+v", none)
	}
}

func TestParseLabels(t *testing.T) {
	if s, ok := ParseStatus("Ngừng hoạt động"); !ok || s != StatusStopped {
		t.Fatal("nhãn tình trạng")
	}
	if s, ok := ParseStatus("hong"); !ok || s != StatusBroken {
		t.Fatal("mã không dấu")
	}
	if o, ok := ParseOwnerType("Thành viên HTX"); !ok || o != OwnerCoopMember {
		t.Fatal("loại chủ sở hữu")
	}
	if _, ok := ParseOwnerType("cá nhân"); ok {
		t.Fatal("loại lạ phải bị từ chối")
	}
}
