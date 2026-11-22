package domain

import (
	"strings"
	"testing"
)

func TestBoundedPlanInputs194(t *testing.T) {
	for _, value := range []string{"0", "120.5000", "-1", "999999999999999999.99"} {
		if _, err := ParseInputDecimal(value); err != nil {
			t.Fatalf("ordinary value %s: %v", value, err)
		}
	}
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "NaN", "0." + strings.Repeat("0", 100000)} {
		if _, err := ParseInputDecimal(value); err == nil {
			t.Fatal("unbounded input accepted")
		}
		if IsInputInteger(value) {
			t.Fatal("unbounded integer accepted")
		}
	}
	if !IsInputInteger("100000") || !IsInputInteger("-1") || IsInputInteger("1.5") {
		t.Fatal("bounded integer syntax changed")
	}
}
