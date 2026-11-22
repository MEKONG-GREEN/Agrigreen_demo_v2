package domain

import (
	"errors"
	"regexp"

	"github.com/shopspring/decimal"
)

var boundedPlainDecimal = regexp.MustCompile(`^-?[0-9]{1,32}(\.[0-9]{1,32})?$`)

// ParseInputDecimal bounds work before decimal parsing or arithmetic. Business
// bounds (quantity16, budget18 integer digits; scale4/2) remain in ValidateEntries.
func ParseInputDecimal(text string) (decimal.Decimal, error) {
	if len(text) > 66 || !boundedPlainDecimal.MatchString(text) {
		return decimal.Decimal{}, errors.New("bounded plain decimal required")
	}
	return decimal.NewFromString(text)
}

var boundedPlainInteger = regexp.MustCompile(`^[+-]?[0-9]{1,10}$`)

// IsInputInteger precedes Atoi when decoding externally supplied spreadsheet cells.
func IsInputInteger(text string) bool {
	return len(text) <= 11 && boundedPlainInteger.MatchString(text)
}
