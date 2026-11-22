package handler

import (
	"encoding/json"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
)

// Bound each occurrence at the decoder boundary, including duplicate/case-folded
// fields. The domain still enforces nonnegative amounts, scale and business caps.
// Keep ordinary bounded negative values here so the domain retains its 422 errors.
type planDecimal decimal.Decimal

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
	value, err := domain.ParseInputDecimal(text)
	if err != nil {
		return err
	}
	*d = planDecimal(value)
	return nil
}

func (d *planDecimal) value() *decimal.Decimal {
	return (*decimal.Decimal)(d)
}
