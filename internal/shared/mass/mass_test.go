package mass

import (
	"errors"
	"testing"
)

func TestParseKg(t *testing.T) {
	if _, err := ParseKg("1.0005"); !errors.Is(err, ErrScale) {
		t.Fatalf("muốn ErrScale, nhận %v", err)
	}
	if _, err := ParseKg("100000000000"); !errors.Is(err, ErrRange) {
		t.Fatalf("muốn ErrRange, nhận %v", err)
	}
	if k := MustKg("1520.5"); k.String() != "1520.500" {
		t.Fatalf("got %s", k)
	}
}

func TestTonnes(t *testing.T) {
	k, err := FromTonnes("12.3456")
	if err != nil {
		t.Fatal(err)
	}
	if k.String() != "12345.600" || k.Tonnes() != "12.345600" {
		t.Fatalf("got %s / %s", k, k.Tonnes())
	}
	if _, err := FromTonnes("0.0000001"); !errors.Is(err, ErrScale) {
		t.Fatalf("muốn ErrScale, nhận %v", err)
	}
}

func TestPerUnitYield(t *testing.T) {
	// sản lượng quy đổi kg/cuộn (mục 13.3)
	y, err := MustKg("5000").PerUnit(3)
	if err != nil {
		t.Fatal(err)
	}
	if y.String() != "1666.667" {
		t.Fatalf("got %s", y)
	}
	if _, err := MustKg("1").PerUnit(0); err == nil {
		t.Fatal("chia cho 0 phải lỗi")
	}
}

func TestNonNegative(t *testing.T) {
	if err := MustKg("-1").NonNegative(); !errors.Is(err, ErrNegative) {
		t.Fatal("khối lượng âm phải lỗi")
	}
}
