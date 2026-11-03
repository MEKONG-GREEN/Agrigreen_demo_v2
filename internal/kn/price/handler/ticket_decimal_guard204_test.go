package handler

import (
	"strings"
	"testing"
)

// Small exponent witnesses the guard without provoking a huge rescale on a mutant.
func TestTicketDecimal204RejectExponent(t *testing.T) {
	for _, raw := range []string{"1e3", "1E3"} {
		if d, err := parseDec(&raw, "min"); err == nil || d != nil {
			t.Fatalf("parseDec(%q) accepted exponent: %v, %v", raw, d, err)
		}
	}
}

func TestTicketDecimal204Compatibility(t *testing.T) {
	if d, e := parseDec(nil, "min"); d != nil || e != nil {
		t.Fatalf("nil: %v %v", d, e)
	}
	for _, raw := range []string{"", "0", "-1", "00001.2300", "9000.00", "1.234", "999999999999.99", "1000000000000", "-0.0001"} {
		d, e := parseDec(&raw, "min")
		if e != nil || (raw != "" && d == nil) {
			t.Fatalf("compatible %q: %v %v", raw, d, e)
		}
	}
}

func TestTicketDecimal204Hostile(t *testing.T) {
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), strings.Repeat("0", 513), "+1", ".5", "1.", " 1", "1 ", "1,2", "１２"} {
		if d, e := parseDec(&raw, "max"); e == nil || d != nil {
			t.Fatalf("accepted hostile length=%d", len(raw))
		}
	}
	raw := strings.Repeat("0", 508) + "1.23"
	if d, e := parseDec(&raw, "max"); e != nil || d == nil || d.String() != "1.23" {
		t.Fatalf("512-byte compatible control: %v %v", d, e)
	}
}
