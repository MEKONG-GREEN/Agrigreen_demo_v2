package app

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// xlsxWithRawNumber dựng tệp theo bố cục mẫu, ô C3 là ô SỐ với giá trị thô raw đúng như trong XML (<c r="C3"><v>raw</v></c>).
func xlsxWithRawNumber(t *testing.T, raw string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sh := "Sheet1"
	for _, p := range [][2]string{{"A1", "A"}, {"B1", "B"}, {"C1", "C"}, {"A2", "Cố định"}, {"B2", "Cố định"}, {"C2", "Nhập"}, {"B3", "Mỹ Hòa Hưng"}} {
		if err := f.SetCellValue(sh, p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetCellDefault(sh, "C3", "0"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`<c\b[^>]*r="C3"[^>]*>.*?</c>`)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	changed := false
	for _, z := range zr.File {
		r, err := z.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if z.Name == "xl/worksheets/sheet1.xml" {
			nx := re.ReplaceAll(data, []byte(`<c r="C3"><v>`+raw+`</v></c>`))
			changed = !bytes.Equal(nx, data)
			data = nx
		}
		w, err := zw.Create(z.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("không vá được ô C3")
	}
	return out.Bytes()
}

// Codex P2 (#107): ô số có số mũ / độ dài vô lý bị từ chối với lỗi nêu đúng ô, không nở thành chuỗi khổng lồ.
func TestParseGridImport_XLSXNumberOutOfRange(t *testing.T) {
	for name, raw := range map[string]string{
		"1E100000":         "1E100000",
		"1E-100000":        "1E-100000",
		"số mũ rất dài":    "1E" + strings.Repeat("9", 40),
		"chuỗi chữ số dài": strings.Repeat("1", 5000),
		"phần lẻ dài":      "0." + strings.Repeat("3", 5000),
		"lớn hơn double":   "1.8E+308",
		"1E309":            "1E309",
		"dưới số chuẩn":    "4.9E-324",
		"âm quá lớn":       "-1E+400",
	} {
		g, err := ParseGridImport(xlsxWithRawNumber(t, raw))
		var p *problem.Problem
		if !errors.As(err, &p) || p.Code != "extension.grid_import_invalid" || !strings.Contains(p.Detail, "Ô C3") {
			t.Errorf("%s: muốn lỗi nêu ô C3, nhận %+v %v", name, g, err)
		}
	}
}

// Ô số hợp lệ (kể cả sát biên double) vẫn đọc đúng, đầu ra có giới hạn độ dài.
func TestParseGridImport_XLSXNumberWithinRange(t *testing.T) {
	for raw, want := range map[string]string{
		"1E-3":                    "0.001",
		"1E+20":                   "100000000000000000000",
		"1.234":                   "1.234",
		"-1.5E+3":                 "-1500",
		"0E+99999":                "0",
		"1.7976931348623157E+308": "17976931348623200" + strings.Repeat("0", 292),
		"2.2250738585072014E-308": "0." + strings.Repeat("0", 307) + "22250738585072",
	} {
		g, err := ParseGridImport(xlsxWithRawNumber(t, raw))
		if err != nil || len(g.Rows) != 1 {
			t.Errorf("%s: %+v %v", raw, g, err)
			continue
		}
		got := g.Rows[0].Cells["C"]
		if got != want || len(got) > maxCanonicalNumberLen || len(g.Rows[0].Numeric) != 1 {
			t.Errorf("%s: nhận %q (%d ký tự), muốn %q", raw, got, len(got), want)
		}
	}
}

func TestNormalizeExcelNumber_Errors(t *testing.T) {
	for raw, want := range map[string]error{
		"":                         errNotNumber,
		"NaN":                      errNotNumber,
		"1E":                       errNotNumber,
		"E5":                       errNotNumber,
		".":                        errNotNumber,
		"1.2.3":                    errNotNumber,
		"1E5x":                     errNotNumber,
		"1E100000":                 errNumberOutOfRange,
		"1E-100000":                errNumberOutOfRange,
		strings.Repeat("9", 65):    errNumberOutOfRange,
		"1.7976931348623158E+308":  errNumberOutOfRange,
		"-2.2250738585072013E-308": errNumberOutOfRange,
	} {
		if got, err := normalizeExcelNumber(raw); !errors.Is(err, want) {
			t.Errorf("%q: nhận %q %v, muốn %v", raw, got, err, want)
		}
	}
	// 64 ký tự vẫn nhận (giới hạn không cắt số hợp lệ), làm tròn 15 chữ số có nghĩa.
	if got, err := normalizeExcelNumber("1." + strings.Repeat("0", 62)); err != nil || got != "1" {
		t.Errorf("64 ký tự: %q %v", got, err)
	}
}
