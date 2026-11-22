package handler

import (
	"strings"
	"testing"
)

func TestConfigDecimalBounds(t *testing.T) {
	for _, raw := range []string{"0.01", "1", "30.50", "99.99", "100", "100.00"} {
		if _, err := calDecimal(raw, "pct"); err != nil {
			t.Errorf("giá trị hợp lệ %q: %v", raw, err)
		}
	}
	for _, field := range []string{"pct", "flat_pct", "survey_pct"} {
		for _, tc := range []struct{ name, raw string }{
			{"large-exponent", "1e100000"}, {"small-exponent", "1e-100000"},
			{"long", strings.Repeat("9", 100000)}, {"negative", "-1"},
			{"zero", "0"}, {"zero-decimal", "0.00"}, {"above-bound", "100.01"},
			{"large", "999999999999999999999999"}, {"precision", "1.001"},
			{"nan", "NaN"}, {"infinity", "Infinity"}, {"hex", "0x10"},
			{"empty", ""}, {"trailing-dot", "1."}, {"leading-dot", ".5"},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				if _, err := calDecimal(tc.raw, field); err == nil {
					t.Fatal("phải từ chối cú pháp/độ dài/biên trước khi tính số thập phân")
				}
			})
		}
	}
}
