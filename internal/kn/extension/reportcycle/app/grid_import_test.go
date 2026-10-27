package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Bố cục "Tải mẫu Excel": tiêu đề gộp → hàng chữ cái → dòng loại cột → dòng dữ liệu → "Yêu cầu biểu mẫu".
func TestParseGridImport_XLSXTemplateLayout(t *testing.T) {
	f := excelize.NewFile()
	sh := "Sheet1"
	set := func(cell string, v any) {
		if err := f.SetCellValue(sh, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	set("A1", "TIẾN ĐỘ XUỐNG GIỐNG")
	set("A2", "STT")
	set("C2", "Xã, phường")
	for i, l := range []string{"A", "B", "C", "D", "E"} {
		set(l+"3", l)
		set(l+"4", []string{"Cố định", "Cố định", "Cố định", "Tỉnh giao", "Nhập"}[i])
	}
	set("A5", 1)
	set("B5", "Long Xuyên")
	set("C5", "Mỹ Hòa Hưng")
	set("E5", "12.5")
	set("A7", 2) // dòng trống giữa vẫn bỏ qua
	set("C7", "Bình Đức")
	set("E7", 3)
	set("A9", "*Yêu cầu biểu mẫu: nhập theo tuần")
	set("C10", "Không đọc")
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	g, err := ParseGridImport(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := []GridImportRow{
		{Line: 5, Cells: map[string]string{"A": "1", "B": "Long Xuyên", "C": "Mỹ Hòa Hưng", "E": "12.5"}, Numeric: []string{"A"}}, // E5 là ô chữ
		{Line: 7, Cells: map[string]string{"A": "2", "C": "Bình Đức", "E": "3"}, Numeric: []string{"A", "E"}},
	}
	if g.Sheet != sh || !reflect.DeepEqual(g.Rows, want) {
		t.Fatalf("nhận %+v", g)
	}
}

func TestParseGridImport_CSVAndErrors(t *testing.T) {
	g, err := ParseGridImport([]byte("\xef\xbb\xbfTiêu đề,,\nA,B,C,D\nMỹ Hòa Hưng,,,7\n"))
	if err != nil || len(g.Rows) != 1 || g.Rows[0].Cells["D"] != "7" || g.Rows[0].Line != 3 || g.Rows[0].Numeric == nil || len(g.Rows[0].Numeric) != 0 {
		t.Fatalf("CSV: %+v %v", g, err)
	}
	for name, data := range map[string][]byte{
		"không có hàng chữ cái": []byte("Xã,Diện tích\nMỹ Hòa Hưng,1\n"),
		"xls cũ":                {0xd0, 0xcf, 0x11, 0xe0, 0, 0},
		"zip hỏng":              []byte("PK\x03\x04hỏng"),
	} {
		_, err := ParseGridImport(data)
		var p *problem.Problem
		if !errors.As(err, &p) || p.Code != "extension.grid_import_invalid" {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestColumnLetter(t *testing.T) {
	for n, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 226: "HR"} {
		if got := columnLetter(n); got != want {
			t.Fatalf("%d: %s ≠ %s", n, got, want)
		}
	}
}

// Codex P1 (#107): ô SỐ của .xlsx phải về API dạng số chuẩn dấu chấm thập phân + đánh dấu `numeric`, để web KHÔNG đọc
// 1.234 thành 1234 (dấu chấm nghìn kiểu VN). Ô CHỮ "1.234,5" giữ nguyên (web mới chuẩn hóa kiểu VN). Ô số có định dạng
// "#,##0.00" lấy giá trị thô (không "1,234.50"). Kết quả so với tệp mẫu dùng chung với test web (importRows.test.ts).
func TestParseGridImport_NumericCellsGolden(t *testing.T) {
	f := excelize.NewFile()
	sh := "Sheet1"
	set := func(cell string, v any) {
		if err := f.SetCellValue(sh, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	set("A1", "TIẾN ĐỘ")
	for i, l := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		set(l+"2", l)
		set(l+"3", []string{"Cố định", "Cố định", "Nhập", "Nhập", "Nhập", "Nhập", "Nhập"}[i])
	}
	set("A4", 1)
	set("B4", "Mỹ Hòa Hưng")
	set("C4", 1.234)
	set("D4", 12.345)
	set("E4", 0.001)
	set("F4", "1.234,5")
	set("G4", 1234.5)
	style, err := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle(sh, "G4", "G4", style); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	g, err := ParseGridImport(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	gotRaw, err := json.Marshal(map[string]any{"sheet": g.Sheet, "rows": g.Rows})
	if err != nil {
		t.Fatal(err)
	}
	wantRaw, err := os.ReadFile(filepath.Join("testdata", "grid_import_numeric.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("API nhập lưới:\n nhận %s\n muốn %s", gotRaw, wantRaw)
	}
}
