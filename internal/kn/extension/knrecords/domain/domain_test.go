package domain

import (
	"strings"
	"testing"
)

func TestParseArea(t *testing.T) {
	for _, ok := range []string{"", "0", "12.5", "1000000", "0.0001", " 3 "} {
		if _, err := ParseArea(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"1e100000", "1e-100000", "1E5", "-1", "+1", "1000001", "1.23456", "abc", ".5", "1.", "0x10",
		strings.Repeat("9", 100000), "NaN", "Infinity", "1_000"} {
		if _, err := ParseArea(bad); err == nil {
			t.Errorf("%.30q phải lỗi", bad)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{"0912345678": "84912345678", "+84 912 345 678": "84912345678", "0912.345.678": "84912345678"} {
		if got, ok := NormalizePhone(in); !ok || got != want {
			t.Errorf("%q → %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "12345", "0012345678", "091234567x", strings.Repeat("0", 1000)} {
		if _, ok := NormalizePhone(bad); ok {
			t.Errorf("%.20q phải lỗi", bad)
		}
	}
}

func TestRecordNormalize(t *testing.T) {
	has := func(es Errors, f string) bool {
		for _, e := range es {
			if e.Field == f {
				return true
			}
		}
		return false
	}
	r := Record{Kind: "x"}
	if es := r.Normalize(); !has(es, "kind") {
		t.Fatalf("loại lạ: %v", es)
	}
	r = Record{Kind: KindCooperative}
	if es := r.Normalize(); !has(es, "commune_code") || !has(es, "name") {
		t.Fatalf("thiếu bắt buộc: %v", es)
	}
	r = Record{Kind: KindHousehold, CommuneCode: "31117", Name: "Hộ A", Address: "Ấp 1"}
	if es := r.Normalize(); !has(es, "name") || !has(es, "address") {
		t.Fatalf("nông hộ không nhận tên/địa chỉ: %v", es)
	}
	r = Record{Kind: KindCoopGroup, CommuneCode: "31117", Name: "THT gọi 0912345678", TaxCode: "1801234567"}
	if es := r.Normalize(); !has(es, "name") || !has(es, "tax_code") {
		t.Fatalf("PII trong tên, MST cho THT: %v", es)
	}
	r = Record{Kind: KindCooperative, CommuneCode: " 31117 ", Name: "  HTX  A ", TaxCode: "1801234567-001"}
	if es := r.Normalize(); len(es) != 0 || r.CommuneCode != "31117" || r.Name != "HTX  A" {
		t.Fatalf("hợp lệ: %v %+v", es, r)
	}
	if NameKey("  HTX   Thuận PHÁT ") != NameKey("htx thuận phát") {
		t.Fatal("khóa tên")
	}
}

func TestPersonValidate(t *testing.T) {
	p := Person{FullName: " ", Phone: ""}
	if es := p.Validate("representative"); len(es) != 2 {
		t.Fatalf("%v", es)
	}
	p = Person{FullName: strings.Repeat("a", 121), Phone: "0912345678"}
	if es := p.Validate("holder"); len(es) != 1 || es[0].Field != "holder.full_name" {
		t.Fatalf("%v", es)
	}
}

func TestValidReason(t *testing.T) {
	if _, bad := ValidReason("ngắn"); bad == "" {
		t.Fatal("lý do ngắn")
	}
	if _, bad := ValidReason("liên hệ 0912345678 nhé"); bad == "" {
		t.Fatal("lý do có SĐT")
	}
	if s, bad := ValidReason("  HTX giải thể theo quyết định  "); bad != "" || s != "HTX giải thể theo quyết định" {
		t.Fatal(bad)
	}
}
