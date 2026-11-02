package domain

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var rulePercentSyntax = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)

// guardRulePercentages checks raw JSON before shopspring/decimal unmarshals it.
// The existing exact-point primitive bounds syntax, precision and [0,100]
// through strings before any conversion, so exponents cannot trigger expansion.
// Both JSON strings and plain JSON numbers remain accepted for native clients.
func guardRulePercentages(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalid("tham số phải là JSON hợp lệ")
	}
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		value, present := fields[key]
		if !present {
			continue
		} // Existing missing-field/default semantics stay unchanged.
		if len(value) > 32*6+2 {
			return invalidRulePercent(key)
		}
		value = bytes.TrimSpace(value)
		text := string(value)
		if len(value) > 0 && value[0] == '"' {
			if err := json.Unmarshal(value, &text); err != nil {
				return invalidRulePercent(key)
			}
		}
		if len(text) > 32 || !rulePercentSyntax.MatchString(text) {
			return invalidRulePercent(key)
		}
		// Preserve ordinary zero-padded input within the raw bound; trimming is
		// bounded string work, after syntax/length checks and before conversion.
		whole, fraction, decimalPoint := strings.Cut(text, ".")
		whole = strings.TrimLeft(whole, "0")
		if whole == "" {
			whole = "0"
		}
		checked := whole
		if decimalPoint {
			checked += "." + fraction
		}
		if _, err := ParseAppraisalPoints(checked, 100*100); err != nil {
			return invalidRulePercent(key)
		}
	}
	return nil
}

func invalidRulePercent(key string) error {
	// Never echo an unbounded numeric value in a validation response.
	return invalid("%s: nhập thập phân thường 0–100, tối đa 2 số lẻ và 32 ký tự; không dùng số mũ", key)
}
