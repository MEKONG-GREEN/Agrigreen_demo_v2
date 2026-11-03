package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var rulePercentSyntax = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)

// guardRulePercentages walks each occurrence before shopspring/decimal sees it.
// A map would discard overwritten duplicate keys, while encoding/json still
// unmarshals their earlier values into decimal fields. Token traversal preserves
// those occurrences and case-folding mirrors encoding/json's field matching.
func guardRulePercentages(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid("tham số phải là đối tượng JSON hợp lệ")
	}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return invalid("tham số phải là JSON hợp lệ")
		}
		name, ok := token.(string)
		if !ok {
			return invalid("tham số phải là JSON hợp lệ")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return invalid("tham số phải là JSON hợp lệ")
		}
		if err = guardRulePercentage(name, value); err != nil {
			return err
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return invalid("tham số phải là JSON hợp lệ")
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return invalid("tham số phải là JSON hợp lệ")
	}
	return nil
}

func guardRulePercentage(name string, value json.RawMessage) error {
	key := ""
	for _, expected := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		if strings.EqualFold(name, expected) {
			key = expected
			break
		}
	}
	if key == "" {
		return nil
	} // Preserve missing/default and unknown-field validation.
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
	// Bounded syntax/length precede normalization and exact-point conversion.
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
	return nil
}

func invalidRulePercent(key string) error {
	// Never echo an unbounded numeric value in a validation response.
	return invalid("%s: nhập thập phân thường 0–100, tối đa 2 số lẻ và 32 ký tự; không dùng số mũ", key)
}
