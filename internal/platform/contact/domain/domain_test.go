package domain_test

import (
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/domain"
)

func TestFieldSets(t *testing.T) {
	if got := domain.SetBasic.Fields(); !slices.Equal(got, []string{"full_name", "birth_year", "phone"}) {
		t.Fatalf("basic: %v", got)
	}
	if got := domain.SetFull.Fields(); len(got) != 7 || !slices.Contains(got, "email") {
		t.Fatalf("full: %v", got)
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		q, name, phone string
		extra          []string
		want           bool
	}{
		{"tran thi", "Trần Thị Mai", "84912000111", nil, true},
		{"ĐỖ", "Đỗ Văn A", "", nil, true},
		{"do van", "Đỗ Văn A", "", nil, true},
		{"0912 000 111", "X", "84912000111", nil, true},
		{"+84912000111", "X", "84912000111", nil, true},
		{"912", "X", "84912000111", nil, true},
		{"ttkn.vn", "X", "", nil, false},
		{"ttkn.vn", "X", "", []string{"a@ttkn.vn"}, true},
		{"", "X", "", nil, true},
		{"01", "Nguyễn 01", "84912000111", nil, true}, // 2 chữ số: tìm theo tên
	}
	for _, c := range cases {
		if got := domain.Match(c.q, c.name, c.phone, c.extra...); got != c.want {
			t.Errorf("Match(%q, %q, %q, %v) = %v", c.q, c.name, c.phone, c.extra, got)
		}
	}
}

func TestUnitInputAndChecks(t *testing.T) {
	p := "+84 296 3852 111"
	u, err := domain.UnitInput{Name: "  Sở  NN&MT ", Level: "tinh", Phone: &p}.Normalize()
	if err != nil || u.Name != "Sở NN&MT" || *u.Phone != "02963852111" {
		t.Fatalf("%+v %v", u, err)
	}
	bad := "12ab"
	if _, err := (domain.UnitInput{Name: "X", Level: "tinh", Phone: &bad}).Normalize(); err == nil {
		t.Fatal("SĐT cơ quan sai dạng")
	}
	if _, err := (domain.UnitInput{Name: "X", Level: "huyen"}).Normalize(); err == nil {
		t.Fatal("cấp lạ")
	}
	if domain.CheckEmail("email", "a@@b") == nil || domain.CheckEmail("email", "a@b.vn") != nil {
		t.Fatal("email")
	}
	if domain.CheckBirthYear("1875") == nil || domain.CheckBirthYear("1990") != nil || domain.CheckBirthYear("") != nil {
		t.Fatal("năm sinh")
	}
	if got := domain.LocalPhone("84912000111"); got != "0912000111" {
		t.Fatal(got)
	}
}
