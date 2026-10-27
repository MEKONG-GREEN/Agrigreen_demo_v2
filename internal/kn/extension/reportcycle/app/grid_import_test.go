package app

import (
	"bytes"
	"errors"
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
		{Line: 5, Cells: map[string]string{"A": "1", "B": "Long Xuyên", "C": "Mỹ Hòa Hưng", "E": "12.5"}},
		{Line: 7, Cells: map[string]string{"A": "2", "C": "Bình Đức", "E": "3"}},
	}
	if g.Sheet != sh || !reflect.DeepEqual(g.Rows, want) {
		t.Fatalf("nhận %+v", g)
	}
}

func TestParseGridImport_CSVAndErrors(t *testing.T) {
	g, err := ParseGridImport([]byte("\xef\xbb\xbfTiêu đề,,\nA,B,C,D\nMỹ Hòa Hưng,,,7\n"))
	if err != nil || len(g.Rows) != 1 || g.Rows[0].Cells["D"] != "7" || g.Rows[0].Line != 3 {
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
