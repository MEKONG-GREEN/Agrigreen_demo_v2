package domain

import (
	"errors"
	"strings"
	"testing"
)

// W11.53c: số do người dùng nhập (AUTO-REVIEW §4) — số mũ, chuỗi dài, giá trị rất lớn bị chặn TRƯỚC decimal.
func TestParseScale(t *testing.T) {
	ok := map[string]string{"1500": "1500", "2,5": "2.5", "2.5": "2.5", "1.500,25": "1500.25", "0012": "12", "1500.125": "1500.13",
		"12.350000000000001": "12.35", "999999999999.99": "999999999999.99", "": ""}
	for in, want := range ok {
		d, why := ParseScale(in)
		got := ""
		if d != nil {
			got = d.String()
		}
		if why != "" || got != want {
			t.Errorf("%q: muốn %q, nhận %q (%s)", in, want, got, why)
		}
	}
	for _, in := range []string{"1e100000", "1E-100000", "1.5E+3", strings.Repeat("9", 13), "1000000000000", strings.Repeat("1", 5000),
		"-5", "0", "0,001", "abc", "1.500", "12.345.678", "1 500", "NaN", "Infinity", "0x10", "1," + strings.Repeat("0", 16)} {
		if d, why := ParseScale(in); why == "" || d != nil {
			t.Errorf("%q phải bị chặn, nhận %v", in, d)
		}
	}
}

func TestParseDateAndPhone(t *testing.T) {
	for in, want := range map[string]string{"05/03/2027": "2027-03-05", "5-3-2027": "2027-03-05", "2027-03-05": "2027-03-05", "46451": "2027-03-05"} {
		d, why := ParseDate(in)
		if why != "" || d == nil || d.Format("2006-01-02") != want {
			t.Errorf("%q: muốn %s, nhận %v %s", in, want, d, why)
		}
	}
	for _, in := range []string{"31/02/2027", "1/1/1999", "99999", "2027/03/05", strings.Repeat("1", 50), "1e5"} {
		if _, why := ParseDate(in); why == "" {
			t.Errorf("%q phải bị chặn", in)
		}
	}
	for in, want := range map[string]string{"0912345678": "0912345678", "912345678": "0912345678", "+84 912 345 678": "0912345678",
		"84912345678": "0912345678", "0912.345.678": "0912345678"} {
		if p, ok := NormalizePhone(in); !ok || p != want {
			t.Errorf("%q: muốn %s, nhận %s", in, want, p)
		}
	}
	for _, in := range []string{"12345", "0012345678", "09123456789", "abc", strings.Repeat("9", 40)} {
		if _, ok := NormalizePhone(in); ok {
			t.Errorf("%q phải sai", in)
		}
	}
	if MaskPhone("0912345678") != "091•••678" || MaskName("Nguyễn Văn Bình") != "N. V. Bình" {
		t.Fatal("che bớt")
	}
}

func siteSheet(rows ...[]string) [][]string { return append([][]string{SiteHeaders}, rows...) }

func TestParseSites(t *testing.T) {
	rows := siteSheet(
		[]string{"d1", "Điểm ấp Bình Hòa", "30001", "GA_TP", "1.500,5", "con", "bm-08", "Cán bộ A", ""},
		nil, // dòng trống
		[]string{"D1", "Điểm khác", "30001", "GA_TP", "", "", "", "", ""},              // trùng mã (không phân biệt hoa thường)
		[]string{"D2", "điểm ấp bình hòa", "30001", "GA_TP", "", "", "", "", ""},       // trùng tên cùng xã
		[]string{"D3", "Điểm 3", "30001", "GA_TP", "1e100000", "con", "", "", ""},      // số mũ
		[]string{"D4", "Điểm 4", "x y", "GA_TP", "10", "", "BM-01", "", ""},            // mã xã, thiếu đơn vị, mẫu nhật ký
		[]string{"D5", strings.Repeat("a", 301), "30002", "", "", "", "", "", "", "z"}, // tên dài, thiếu loại, ô ngoài bảng
	)
	got, issues, err := ParseSites(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != "D1" || got[0].Row != 2 || got[0].Scale.Value.String() != "1500.5" || got[0].DiaryForm != "BM-08" ||
		got[0].Officer != "Cán bộ A" {
		t.Fatalf("dòng hợp lệ: %+v", got)
	}
	want := map[string]bool{"4A": true, "5B": true, "6E": true, "7C": true, "7E": true, "7G": true, "8B": true, "8D": true, "8J": true}
	for _, is := range issues {
		k := strings.TrimSpace(strings.Repeat(" ", 0) + itoa(is.Row) + is.Column)
		if !want[k] {
			t.Errorf("lỗi lạ: %+v", is)
		}
		delete(want, k)
		if strings.Contains(is.Message, "1e100000") {
			t.Errorf("lỗi không nhắc lại giá trị: %s", is.Message)
		}
	}
	if len(want) > 0 {
		t.Errorf("thiếu lỗi: %v (%+v)", want, issues)
	}
}

func itoa(n int) string {
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func TestParseLayout(t *testing.T) {
	if _, _, err := ParseSites([][]string{{"STT", "Tên"}}); !errors.Is(err, ErrImportLayout) {
		t.Fatalf("thiếu tiêu đề: %v", err)
	}
	bad := append([]string(nil), SiteHeaders...)
	bad[2], bad[3] = bad[3], bad[2]
	if _, _, err := ParseSites([][]string{bad}); !errors.Is(err, ErrImportLayout) || !strings.Contains(err.Error(), "cột C") {
		t.Fatalf("đổi thứ tự cột: %v", err)
	}
	if _, _, err := ParseSites([][]string{append(append([]string(nil), SiteHeaders...), "Thừa")}); !errors.Is(err, ErrImportLayout) {
		t.Fatalf("cột thừa: %v", err)
	}
	// Tiêu đề không có "(*)", có dòng tiêu đề phụ phía trên: vẫn nhận.
	plain := make([]string, len(SiteHeaders))
	for i, h := range SiteHeaders {
		plain[i] = strings.ToUpper(strings.ReplaceAll(h, " (*)", ""))
	}
	if _, _, err := ParseSites([][]string{{"Danh sách điểm"}, plain}); err != nil {
		t.Fatalf("tiêu đề không dấu *: %v", err)
	}
	many := [][]string{SiteHeaders}
	for i := 0; i <= ImportMaxSites; i++ {
		many = append(many, []string{"D" + itoa(i+1), "Điểm " + itoa(i+1), "30001", "GA_TP", "", "", "", "", ""})
	}
	if _, _, err := ParseSites(many); !errors.Is(err, ErrImportLayout) {
		t.Fatalf("quá số dòng: %v", err)
	}
}

func TestParseHouseholds(t *testing.T) {
	ref := "0192a000-0000-7000-8000-0000000000aa"
	rows := [][]string{HouseholdHeaders,
		{"d1", "Nguyễn Văn Bình", "0912345678", "500", "con", "", "05/03/2027", ""},
		{"D1", "Trần Thị Cúc", "912345678", "", "", "Hộ tự ghi", "", ""}, // trùng SĐT cùng điểm (Excel mất số 0)
		{ref, "Lê Văn Dũng", "0987654321", "", "", "hộ tự ghi", "", ""},
		{"D2", "", "", "1e5", "con", "khác", "30/02/2027", ""},
	}
	got, issues, err := ParseHouseholds(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SiteCode != "D1" || got[0].PhoneKey != "0912345678" || got[0].KeeperKind != KeeperOfficer ||
		got[0].JoinedOn == nil || got[1].SiteRef == nil || got[1].SiteRef.String() != ref || !got[1].SelfEntry {
		t.Fatalf("dòng hợp lệ: %+v", got)
	}
	want := map[string]bool{"3C": true, "5B": true, "5C": true, "5D": true, "5F": true, "5G": true}
	for _, is := range issues {
		k := itoa(is.Row) + is.Column
		if !want[k] {
			t.Errorf("lỗi lạ: %+v", is)
		}
		delete(want, k)
		if strings.Contains(is.Message, "0912345678") || strings.Contains(is.Message, "Cúc") {
			t.Errorf("lỗi lộ dữ liệu cá nhân: %s", is.Message)
		}
	}
	if len(want) > 0 {
		t.Errorf("thiếu lỗi: %v (%+v)", want, issues)
	}
}
