package render

import (
	"bytes"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

// htxSample dựng tập dữ liệu từ mẫu hệ thống htx.production (HTX US-DASH-02).
func htxSample(t *testing.T) domain.Dataset {
	t.Helper()
	tpl, ok := domain.Builtin(domain.HTXProductionKey)
	if !ok {
		t.Fatal("thiếu mẫu hệ thống htx.production")
	}
	d := func(s string) domain.Value { return domain.D(decimal.RequireFromString(s)) }
	row := domain.Row{"cooperative_name": domain.Txt("HTX Lúa Xanh Bình Đức"), "season": domain.Txt("Hè Thu 2026"), "variety_name": domain.Txt("OM 5451"),
		"seasons": domain.Int(2), "households": domain.Int(2), "area_ha": d("3.0000"), "harvested_area_ha": d("1.5000"),
		"expected_t": d("20.25"), "actual_t": d("9.750125"), "actual_yield_t_per_ha": d("6.5001"), "sop_progress_pct": d("40.0")}
	total := domain.Row{"seasons": domain.Int(2), "households": domain.Int(2), "area_ha": d("3.0000"), "harvested_area_ha": d("1.5000"),
		"expected_t": d("20.25"), "actual_t": d("9.750125"), "actual_yield_t_per_ha": d("6.5001"), "sop_progress_pct": d("40.0")}
	ds := domain.Build(tpl.Def, domain.LevelCooperative, []domain.Row{row}, total)
	ds.Meta = []domain.Meta{{Label: "Ngày xuất", Value: "28/09/2026 10:00"}, {Label: "HTX", Value: "HTX Lúa Xanh Bình Đức"}}
	return ds
}

// HTX US-DASH-02: tệp Excel của mẫu sản lượng HTX — đủ 11 cột, số là ô số, giữ nguyên decimal.
func TestXLSX_HTXProduction_HTX_US_DASH_02(t *testing.T) {
	ds := htxSample(t)
	if len(ds.Columns) != 11 {
		t.Fatalf("11 cột mặc định: %d", len(ds.Columns))
	}
	b, err := XLSX(ds, at)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	hr := HeaderRow(ds)
	get := func(col, row int) string {
		c, _ := excelize.CoordinatesToCellName(col, row)
		v, err := f.GetCellValue(SheetName, c, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	want := []string{"HTX Lúa Xanh Bình Đức", "Hè Thu 2026", "OM 5451", "2", "2", "3", "1.5", "20.25", "9.750125", "6.5001", "40"}
	for i, v := range want {
		if got := get(i+1, hr+1); got != v {
			t.Errorf("cột %d = %q, muốn %q", i+1, got, v)
		}
	}
	if got := get(1, hr+2); got != "Tổng cộng" {
		t.Fatalf("dòng tổng: %q", got)
	}
	c, _ := excelize.CoordinatesToCellName(9, hr+1)
	if typ, _ := f.GetCellType(SheetName, c); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Fatal("sản lượng phải là ô số")
	}
	if v, _ := f.GetCellValue(SheetName, c); v != "9.750" {
		t.Fatalf("hiển thị tấn 3 chữ số thập phân: %q", v)
	}
}

// HTX US-DASH-02: PDF của mẫu sản lượng HTX có tiếng Việt và số định dạng Việt Nam.
func TestPDF_HTXProduction_HTX_US_DASH_02(t *testing.T) {
	b, err := PDF(htxSample(t), at, PDFOptions{NoCompression: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Báo cáo sản lượng HTX", "Sản lượng", "HTX Lúa Xanh Bình Đức", "9,750", "20,250", "40,0", "Tổng cộng"} {
		if !bytes.Contains(b, utf16be(s)) {
			t.Errorf("PDF thiếu %q", s)
		}
	}
}
