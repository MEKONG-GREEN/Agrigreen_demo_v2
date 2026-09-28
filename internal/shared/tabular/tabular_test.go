package tabular

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestNormalizeHeader(t *testing.T) {
	cases := map[string]string{
		"Họ tên":              "ho_ten",
		"  Số máy/SN ":        "so_may_sn",
		"\xef\xbb\xbfMã HTX":  "ma_htx",
		"cooperative_code":    "cooperative_code",
		"Ngày vào HTX":        "ngay_vao_htx",
		"Diện tích (ha)":      "dien_tich_ha",
		"ĐỊA CHỈ":             "dia_chi",
		"Năm SX":              "nam_sx",
		"Mã tổ chức cấp trên": "ma_to_chuc_cap_tren",
	}
	for in, want := range cases {
		if got := NormalizeHeader(in); got != want {
			t.Errorf("NormalizeHeader(%q) = %q, muốn %q", in, got, want)
		}
	}
}

func TestReadCSV(t *testing.T) {
	data := "\xef\xbb\xbfHọ tên, Mã HTX ,Diện tích (ha)\n\nA,HTX1,2.5\n,,\n B , HTX2 ,\n"
	tb, err := ReadBytes([]byte(data), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if tb.Format != FormatCSV || !slices.Equal(tb.Keys, []string{"ho_ten", "ma_htx", "dien_tich_ha"}) {
		t.Fatalf("tiêu đề: %+v", tb)
	}
	if len(tb.Rows) != 2 || tb.Rows[0].Line != 3 || tb.Rows[1].Line != 5 || tb.Rows[1].Cells[1] != "HTX2" {
		t.Fatalf("dòng: %+v", tb.Rows)
	}
	col := tb.Columns(map[string]string{"ho_ten": "full_name", "ma_htx": "coop"})
	if tb.Rows[1].Get(col, "full_name") != "B" || tb.Rows[1].Get(col, "missing") != "" {
		t.Fatalf("Get: %v", col)
	}
	if m := Missing(col, "full_name", "phone"); !slices.Equal(m, []string{"phone"}) {
		t.Fatalf("Missing: %v", m)
	}
	if _, err := ReadBytes([]byte("a\n\xff\xfe\n"), Limits{}); !errors.Is(err, ErrEncoding) {
		t.Fatalf("không phải UTF-8: %v", err)
	}
	if _, err := ReadBytes([]byte("a\n1\n2\n3\n"), Limits{MaxRows: 2}); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("quá số dòng: %v", err)
	}
	if _, err := Read(strings.NewReader(strings.Repeat("x", 100)), Limits{MaxBytes: 50}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("quá dung lượng: %v", err)
	}
	if _, err := ReadBytes([]byte("\n\n"), Limits{}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("tệp rỗng: %v", err)
	}
	if _, err := ReadBytes([]byte{0xD0, 0xCF, 0x11, 0xE0, 0, 0}, Limits{}); !errors.Is(err, ErrLegacyExcel) {
		t.Fatalf(".xls: %v", err)
	}
}

func TestXLSXRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	header := []string{"Mã", "Tên", "Ghi chú"}
	rows := [][]string{{"0001", "Tổ KNCĐ Xã A", "=1+1"}, {"TW", "Trung tâm Khuyến nông", ""}}
	if err := WriteXLSX(&buf, "Cây tổ chức", header, rows); err != nil {
		t.Fatal(err)
	}
	tb, err := ReadBytes(buf.Bytes(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if tb.Format != FormatXLSX || !slices.Equal(tb.Header, header) {
		t.Fatalf("tiêu đề: %+v", tb.Header)
	}
	// Số 0 đầu giữ nguyên; chuỗi "=1+1" là chữ, không thành công thức.
	if len(tb.Rows) != 2 || !slices.Equal(tb.Rows[0].Cells, rows[0]) || !slices.Equal(tb.Rows[1].Cells, rows[1][:2]) || tb.Rows[1].Line != 3 {
		t.Fatalf("dòng: %+v", tb.Rows)
	}
}

// Ô có công thức bị từ chối cả tệp — không tính công thức.
func TestXLSXFormulaRejected(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"Mã", "Diện tích"})
	_ = f.SetSheetRow("Sheet1", "A2", &[]any{"X1", 2})
	_ = f.SetCellFormula("Sheet1", "B3", "B2*2")
	_ = f.SetCellStr("Sheet1", "A3", "X2")
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	_, err := ReadBytes(buf.Bytes(), Limits{})
	var fe *FormulaError
	if !errors.As(err, &fe) || fe.Cell != "B3" {
		t.Fatalf("phải từ chối ô công thức B3: %v", err)
	}
	if msg := Message(err, Limits{}); !strings.Contains(msg, "B3") || !strings.Contains(msg, "công thức") {
		t.Fatalf("thông báo: %q", msg)
	}
}

// Ô số định dạng ngày đổi sang YYYY-MM-DD; số thường giữ dạng thô.
func TestXLSXDatesAndNumbers(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"Ngày", "Số", "Ngày riêng"})
	_ = f.SetCellValue("Sheet1", "A2", 45689) // 2025-02-01
	_ = f.SetCellValue("Sheet1", "B2", 2.5)
	_ = f.SetCellValue("Sheet1", "C2", 45689)
	st, _ := f.NewStyle(&excelize.Style{NumFmt: 14})
	custom := "dd/mm/yyyy"
	st2, _ := f.NewStyle(&excelize.Style{CustomNumFmt: &custom})
	_ = f.SetCellStyle("Sheet1", "A2", "A2", st)
	_ = f.SetCellStyle("Sheet1", "C2", "C2", st2)
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	tb, err := ReadBytes(buf.Bytes(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := tb.Rows[0].Cells; !slices.Equal(got, []string{"2025-02-01", "2.5", "2025-02-01"}) {
		t.Fatalf("giá trị: %v", got)
	}
}

func TestXLSXLimits(t *testing.T) {
	var buf bytes.Buffer
	rows := [][]string{{"1"}, {"2"}, {"3"}}
	if err := WriteXLSX(&buf, "", []string{"a"}, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBytes(buf.Bytes(), Limits{MaxRows: 2}); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("quá số dòng: %v", err)
	}
	if _, err := ReadBytes([]byte("PK\x03\x04hỏng"), Limits{}); !errors.Is(err, ErrFormat) {
		t.Fatalf("zip hỏng: %v", err)
	}
}
