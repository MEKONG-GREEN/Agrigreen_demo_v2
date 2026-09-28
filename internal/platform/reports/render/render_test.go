package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

func sample() domain.Dataset {
	d := func(s string) domain.Value { return domain.D(decimal.RequireFromString(s)) }
	return domain.Dataset{
		Title: "Báo cáo tiến độ gieo sạ",
		Meta:  []domain.Meta{{Label: "Ngày xuất", Value: "28/09/2026 10:00"}, {Label: "Kỳ báo cáo", Value: "Tháng 09/2026"}},
		Columns: []domain.DataColumn{
			{Key: "province_name", Title: "Tỉnh/Thành phố", Kind: domain.KindText},
			{Key: "sown_area_ha", Title: "Diện tích đã sạ (ha)", Kind: domain.KindDecimal, Scale: 2},
			{Key: "cooperatives", Title: "Số HTX báo cáo", Kind: domain.KindInt},
			{Key: "coverage_pct", Title: "Mức đáp ứng (%)", Kind: domain.KindPercent, Scale: 2},
		},
		Rows: [][]domain.Value{
			{domain.Txt("An Giang"), d("1234567.8912"), domain.Int(12), d("60.0001")},
			{domain.Txt("Cần Thơ"), d("0.1"), domain.Int(1), {}},
		},
		Total:       []domain.Value{domain.Txt("Tổng cộng"), d("1234567.9912"), domain.Int(13), {}},
		Provisional: true,
		Notes:       []string{"Còn 1/3 xã chưa gửi báo cáo của kỳ: 30301"},
		Empty:       domain.EmptyMessage,
	}
}

var at = time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)

// US-DASH-02: mở tệp Excel bằng excelize và kiểm từng ô; số giữ nguyên chuỗi decimal.
func TestXLSXContent(t *testing.T) {
	ds := sample()
	b, err := XLSX(ds, at)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	get := func(cell string) string {
		v, err := f.GetCellValue(SheetName, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	hr := HeaderRow(ds) // 1 tiêu đề + 2 dòng đầu + 1 trống → 5
	if hr != 5 {
		t.Fatalf("dòng tiêu đề cột: %d", hr)
	}
	checks := map[string]string{
		"A1": "Báo cáo tiến độ gieo sạ", "A2": "Ngày xuất: 28/09/2026 10:00", "A3": "Kỳ báo cáo: Tháng 09/2026",
		"A5": "Tỉnh/Thành phố", "B5": "Diện tích đã sạ (ha)", "D5": "Mức đáp ứng (%)",
		"A6": "An Giang", "B6": "1234567.8912", "C6": "12", "D6": "60.0001",
		"A7": "Cần Thơ", "B7": "0.1", "D7": "",
		"A8": "Tổng cộng", "B8": "1234567.9912", "C8": "13",
	}
	for cell, want := range checks {
		if got := get(cell); got != want {
			t.Errorf("%s = %q, muốn %q", cell, got, want)
		}
	}
	if typ, _ := f.GetCellType(SheetName, "B6"); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Fatal("ô số phải là số, không phải chuỗi")
	}
	if v, _ := f.GetCellValue(SheetName, "B6"); v != "1,234,567.89" {
		t.Fatalf("định dạng số hiển thị #,##0.00: %q", v)
	}
	if got := get("A10"); !strings.HasPrefix(got, domain.ProvisionalLabel) {
		t.Fatalf("chân trang tạm tính: %q", got)
	}
	if got := get("A11"); got != "Còn 1/3 xã chưa gửi báo cáo của kỳ: 30301" {
		t.Fatalf("ghi chú: %q", got)
	}
	props, _ := f.GetDocProps()
	if props.Created != "2026-09-28T03:00:00Z" {
		t.Fatalf("thuộc tính tệp theo ngày xuất: %+v", props)
	}
}

func utf16be(s string) []byte {
	var out []byte
	for _, r := range utf16.Encode([]rune(s)) {
		out = append(out, byte(r>>8), byte(r))
	}
	return out
}

// PDF có chữ tiếng Việt (phông Roboto nhúng) và số định dạng kiểu Việt Nam.
func TestPDFVietnamese(t *testing.T) {
	b, err := PDF(sample(), at, PDFOptions{NoCompression: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) || len(b) < 10000 {
		t.Fatalf("PDF rỗng hoặc sai: %d byte", len(b))
	}
	if !bytes.Contains(b, []byte("/FontName /utf8roboto")) {
		t.Fatal("phải nhúng phông Roboto")
	}
	for _, s := range []string{"Số liệu tạm tính", "Báo cáo tiến độ gieo sạ", "Cần Thơ", "1.234.567,89", "60,00", "Tổng cộng"} {
		if !bytes.Contains(b, utf16be(s)) {
			t.Errorf("PDF thiếu văn bản %q", s)
		}
	}
	// Bản nén (mặc định) vẫn dựng được và nhỏ hơn.
	c, err := PDF(sample(), at, PDFOptions{})
	if err != nil || len(c) == 0 || len(c) >= len(b) {
		t.Fatalf("PDF nén: %d byte, %v", len(c), err)
	}
	// Bảng rỗng: vẫn có tệp với tiêu đề + thông báo (FN-10 luồng ngoại lệ).
	ds := sample()
	ds.Rows, ds.Total = nil, nil
	e, err := PDF(ds, at, PDFOptions{NoCompression: true})
	if err != nil || !bytes.Contains(e, utf16be(domain.EmptyMessage)) {
		t.Fatalf("bảng rỗng: %v", err)
	}
}
