package money

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseAmountRejectsExtraScale(t *testing.T) {
	if _, err := ParseAmount("10.005"); !errors.Is(err, ErrScale) {
		t.Fatalf("muốn ErrScale, nhận %v", err)
	}
	if _, err := ParseAmount("10.50"); err != nil {
		t.Fatal(err)
	}
}

func TestParseAmountRange(t *testing.T) {
	if _, err := ParseAmount("10000000000000000"); !errors.Is(err, ErrRange) { // 17 chữ số phần nguyên
		t.Fatalf("muốn ErrRange, nhận %v", err)
	}
	if _, err := ParseAmount("9999999999999999.99"); err != nil {
		t.Fatal(err)
	}
}

func TestArithmeticIsExact(t *testing.T) {
	// 0.1 + 0.2 với float là 0.30000000000000004.
	a := MustAmount("0.10").Add(MustAmount("0.20"))
	if a.String() != "0.30" {
		t.Fatalf("got %s", a)
	}
	if !a.Add(a.Neg()).IsZero() {
		t.Fatal("bản ghi bù phải triệt tiêu")
	}
}

func TestMulRoundsHalfUp(t *testing.T) {
	r, err := ParseRate("0.450000")
	if err != nil {
		t.Fatal(err)
	}
	if got := MustAmount("100.01").Mul(r).String(); got != "45.00" { // 45.0045 → 45.00
		t.Fatalf("got %s", got)
	}
	if got := MustAmount("100.03").Mul(r).String(); got != "45.01" { // 45.0135 → 45.01
		t.Fatalf("got %s", got)
	}
}

func TestJSONIsString(t *testing.T) {
	b, err := json.Marshal(MustAmount("1250000.5"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"1250000.50"` {
		t.Fatalf("got %s", b)
	}
	var a Amount
	if err := json.Unmarshal([]byte(`1250000.5`), &a); err == nil {
		t.Fatal("phải từ chối số JSON (có thể đã qua float ở client)")
	}
	if err := json.Unmarshal([]byte(`"12.34"`), &a); err != nil || a.String() != "12.34" {
		t.Fatalf("got %v %s", err, a)
	}
}

func TestScan(t *testing.T) {
	var a Amount
	if err := a.Scan([]byte("99.90")); err != nil || a.String() != "99.90" {
		t.Fatalf("got %v %s", err, a)
	}
	if err := a.Scan(nil); err == nil {
		t.Fatal("NULL phải lỗi")
	}
}
