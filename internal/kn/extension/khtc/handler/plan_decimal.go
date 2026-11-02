package handler

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/shopspring/decimal"
)

// Bound each occurrence at the decoder boundary, including duplicate/case-folded
// fields. The domain still enforces nonnegative amounts, scale and business caps.
// Keep ordinary bounded negative values here so the domain retains its 422 errors.
type planDecimal decimal.Decimal

var planPlainDecimal = regexp.MustCompile(`^-?[0-9]{1,32}(\.[0-9]{1,32})?$`)

func (d *planDecimal) UnmarshalJSON(raw []byte) error {
	if len(raw) > 512 {
		return errors.New("plan decimal too long")
	}
	text := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
	}
	if len(text) > 66 || !planPlainDecimal.MatchString(text) {
		return errors.New("plan decimal must be a bounded plain decimal")
	}
	value, err := decimal.NewFromString(text)
	if err != nil {
		return err
	}
	*d = planDecimal(value)
	return nil
}

func (d *planDecimal) value() *decimal.Decimal {
	return (*decimal.Decimal)(d)
}
