package main

import (
	"bytes"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// W11.30 (D3 H10): chuẩn hóa ô của tệp nhân sự cũ.
func TestNormalize_W1130(t *testing.T) {
	dates := []struct{ in, want string }{
		{"15/03/1985", "1985-03-15"}, {"5-3-1985", "1985-03-05"}, {"05.03.1985", "1985-03-05"}, {"1985-03-15", "1985-03-15"},
		{"1985/3/5", "1985-03-05"}, {"03/1985", "1985-03"}, {"1985-03", "1985-03"}, {"1985", "1985"}, {"43831", "2020-01-01"},
		{"43831.0", "2020-01-01"},
	}
	for _, c := range dates {
		if got, _, ok := normDate(c.in); !ok || got != c.want {
			t.Errorf("normDate(%q) = %q, %v; muốn %q", c.in, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "30/02/1988", "13/2020", "1850", "abc", "12/13/2020", "200"} {
		if got, _, ok := normDate(bad); ok {
			t.Errorf("normDate(%q) nhận %q", bad, got)
		}
	}
	if _, serial, _ := normDate("43831"); !serial {
		t.Error("số ngày Excel phải gắn cờ serial")
	}

	steps := []struct {
		in           string
		step, of     int
		fromDate, ok bool
	}{
		{"3", 3, 0, false, true}, {"3.0", 3, 0, false, true}, {"Bậc 3", 3, 0, false, true}, {"3/9", 3, 9, false, true},
		{"3 / 9", 3, 9, false, true}, {"2026-09-03", 3, 9, true, true}, {"2026-03-09", 3, 9, true, true}, {"bậc", 0, 0, false, false},
		{"0", 0, 0, false, false}, {"9/3", 0, 0, false, false}, {"x", 0, 0, false, false},
	}
	for _, c := range steps {
		s, of, fd, ok := normStep(c.in)
		if s != c.step || of != c.of || fd != c.fromDate || ok != c.ok {
			t.Errorf("normStep(%q) = %d,%d,%v,%v", c.in, s, of, fd, ok)
		}
	}

	for in, want := range map[string]string{"2,67": "2.67", "2.67": "2.67", "HS 2,67": "2.67", "hệ số: 3": "3.00", "4,404": "4.40"} {
		if got, ok := normCoeff(in); !ok || got != want {
			t.Errorf("normCoeff(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "0", "-1", "abc", "100", "2,6,7"} {
		if got, ok := normCoeff(bad); ok {
			t.Errorf("normCoeff(%q) nhận %q", bad, got)
		}
	}

	if n, s := normName("Trần Hoàng Anh (19 VC)"); n != "Trần Hoàng Anh" || !s {
		t.Errorf("normName ghi chú: %q %v", n, s)
	}
	if n, s := normName("  Lê   Văn  Bình "); n != "Lê Văn Bình" || s {
		t.Errorf("normName khoảng trắng: %q %v", n, s)
	}

	phones := []struct {
		in, want  string
		added, ok bool
	}{
		{"0912 345 678", "0912345678", false, true}, {"912345678", "0912345678", true, true}, {"+84 912.345.678", "0912345678", false, true},
		{"84912345678", "0912345678", false, true}, {"0912-345-678", "0912345678", false, true}, {"12345", "", false, false},
		{"0912345678 / 0913", "", false, false},
	}
	for _, c := range phones {
		p, added, ok := normPhone(c.in)
		if p != c.want || added != c.added || ok != c.ok {
			t.Errorf("normPhone(%q) = %q,%v,%v", c.in, p, added, ok)
		}
	}
	if g, ok := normGender("Nữ"); !ok || g != "nu" {
		t.Error("Nữ")
	}
	if _, ok := normGender("X"); ok {
		t.Error("X")
	}
	if fold("Phòng HC-TH") != "phonghcth" {
		t.Error(fold("Phòng HC-TH"))
	}
	m, err := LoadMapping("")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := lookupValue(m, "type", "Hợp đồng lao động"); !ok || v != "hop_dong" {
		t.Error("type HĐLĐ")
	}
	if v, ok := lookupValue(m, "position", "PGĐ"); !ok || v != "pho_giam_doc" {
		t.Error("mapping PGĐ")
	}
	for _, bad := range []string{"version: 2\n", "version: 1\ncolumns:\n  khong_co: [X]\n", "version: 1\nvalues:\n  unit:\n    A: \"có dấu cách\"\n",
		"version: 1\ncolumns:\n  unit: [Phòng]\n  position: [Phòng]\n", "version: 1\ndefaults:\n  as_of: 31/13/2026\n"} {
		if _, err := parseMapping([]byte(bad)); err == nil {
			t.Errorf("ánh xạ sai được nhận: %q", bad)
		}
	}
}

// Tệp mẫu: sheet đầu là sheet nhập liệu, tiêu đề khớp mọi trường chuẩn qua ánh xạ mặc định; ô định dạng Văn bản.
func TestTemplate_W1130(t *testing.T) {
	var b bytes.Buffer
	if err := WriteTemplate(&b); err != nil {
		t.Fatal(err)
	}
	tb, err := tabular.ReadBytes(b.Bytes(), tabular.Limits{MaxBytes: 20 << 20, MaxRows: maxRows, MaxCols: 128})
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Header) != len(Fields) || len(tb.Rows) != 0 {
		t.Fatalf("tiêu đề %d cột, %d dòng", len(tb.Header), len(tb.Rows))
	}
	m, _ := LoadMapping("")
	col := tb.Columns(m.aliases)
	for i, f := range Fields {
		if col[f.Key] != i {
			t.Errorf("cột %q không về trường %s", f.Header, f.Key)
		}
	}
	if safeCell("=1+1") != "'=1+1" || safeCell("HC-01") != "HC-01" {
		t.Error("safeCell")
	}
}
