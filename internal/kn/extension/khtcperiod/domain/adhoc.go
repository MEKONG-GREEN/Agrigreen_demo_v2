package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type AdhocIndicator struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Unit string `json:"unit"`
}

var adhocIndicatorCode = regexp.MustCompile("^[A-Z][A-Z0-9_]{0,39}$")

func NormalizeAdhocIndicators(input []AdhocIndicator) ([]AdhocIndicator, error) {
	if len(input) > 20 {
		return nil, errors.New("tối đa 20 chỉ tiêu")
	}
	out := make([]AdhocIndicator, 0, len(input))
	seen := map[string]bool{}
	for _, v := range input {
		v.Code = strings.ToUpper(strings.TrimSpace(v.Code))
		v.Name = strings.TrimSpace(v.Name)
		v.Unit = strings.TrimSpace(v.Unit)
		if !adhocIndicatorCode.MatchString(v.Code) || seen[v.Code] || v.Name == "" || utf8.RuneCountInString(v.Name) > 200 || utf8.RuneCountInString(v.Unit) > 50 || strings.ContainsRune(v.Name+v.Unit, 0) {
			return nil, errors.New("mã/tên/ĐVT chỉ tiêu không hợp lệ")
		}
		seen[v.Code] = true
		out = append(out, v)
	}
	return out, nil
}
