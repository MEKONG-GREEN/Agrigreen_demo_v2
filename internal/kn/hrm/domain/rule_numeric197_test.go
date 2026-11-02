package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRule197PercentRawBoundsBeforeDecimal(t *testing.T) {
	keys := []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"}
	bad := []struct{ name, value string }{
		{"equivalent_exponent", "1e0"}, {"positive_exponent", "1e100000"}, {"negative_exponent", "1e-100000"},
		{"very_long", strings.Repeat("9", 100000)}, {"nan", "NaN"}, {"infinity", "Infinity"},
		{"over_max", "100.01"}, {"negative", "-1"}, {"precision", "0.001"},
	}
	for _, key := range keys {
		for _, input := range bad {
			for _, quoted := range []bool{true, false} {
				kind := "number"
				if quoted {
					kind = "string"
				}
				t.Run(key+"/"+input.name+"/"+kind, func(t *testing.T) {
					var body map[string]json.RawMessage
					if err := json.Unmarshal(DefaultRules()[0].Payload, &body); err != nil {
						t.Fatal(err)
					}
					value := []byte(input.value)
					if quoted {
						value, _ = json.Marshal(input.value)
					}
					body[key] = value
					raw, err := json.Marshal(body)
					if err != nil { // Raw NaN/Infinity are invalid JSON; exercise the same API rejection.
						raw = []byte(`{"` + key + `":` + input.value + `}`)
					}
					_, _, err = ParseRule(RulePromotion, raw)
					if !errors.Is(err, ErrInvalid) {
						t.Fatal("unsafe percent accepted")
					}
					if len(err.Error()) > 256 {
						t.Fatalf("unbounded numeric diagnostic: %d bytes", len(err.Error()))
					}
				})
			}
		}
	}
}

func TestRule197PercentBoundaryStringAndNumber(t *testing.T) {
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		for _, input := range []string{"0", "100", "12.34", "0.01", "100.00", "000000001.25"} {
			for _, quoted := range []bool{true, false} {
				if !quoted && strings.HasPrefix(input, "00") {
					continue
				} // JSON number syntax forbids leading zeroes.
				var body map[string]json.RawMessage
				if err := json.Unmarshal(DefaultRules()[0].Payload, &body); err != nil {
					t.Fatal(err)
				}
				value := []byte(input)
				if quoted {
					value, _ = json.Marshal(input)
				}
				body[key] = value
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = ParseRule(RulePromotion, raw); err != nil {
					t.Fatalf("valid %s %s string=%v rejected: %v", key, input, quoted, err)
				}
			}
		}
	}
}
func TestRule197PercentCaseFoldedFieldNames(t *testing.T) {
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		names := []string{strings.ToUpper(key)}
		if strings.Contains(key, "k") {
			names = append(names, strings.Replace(key, "k", "K", 1))
		}
		for _, name := range names {
			t.Run(name, func(t *testing.T) {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(DefaultRules()[0].Payload, &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, key)
				fields[name] = json.RawMessage(`"1e0"`)
				raw, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = ParseRule(RulePromotion, raw); !errors.Is(err, ErrInvalid) {
					t.Fatal("case-folded decimal field bypassed raw guard")
				}
				fields[name] = json.RawMessage(`"12.34"`)
				raw, _ = json.Marshal(fields)
				if _, _, err = ParseRule(RulePromotion, raw); err != nil {
					t.Fatalf("ordinary case-folded value: %v", err)
				}
			})
		}
	}
}
func TestRule197PercentDuplicateKeys(t *testing.T) {
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		t.Run(key, func(t *testing.T) {
			raw := DefaultRules()[0].Payload
			wire := append([]byte(`{"`+key+`":"1e0",`), raw[1:]...)
			if _, _, err := ParseRule(RulePromotion, wire); !errors.Is(err, ErrInvalid) {
				t.Fatal("overwritten duplicate decimal bypassed raw guard")
			}
		})
	}
}
