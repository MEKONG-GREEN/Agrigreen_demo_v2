package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/shopspring/decimal"
	"regexp"
)

// A new adhoc refresh validates raw port cells before EvaluateAt can parse them.
// The bound allows exact source aggregates while rejecting exponent notation.
var adhocSourceNumber = regexp.MustCompile(`^-?[0-9]{1,30}(\.[0-9]{1,12})?$`)

func validateAdhocSources(periods []port.KHTCPeriod) error {
	if len(periods) > 200 {
		return ErrDependency.WithDetail("Nguồn có quá nhiều kỳ")
	}
	count := 0
	check := func(cells map[string]string) bool {
		count += len(cells)
		if count > 200000 {
			return false
		}
		for k, v := range cells {
			if len(k) > 512 || len(v) > 44 || !adhocSourceNumber.MatchString(v) {
				return false
			}
		}
		return true
	}
	figure := func(f port.TargetFigure) bool {
		for _, cells := range []map[string]string{f.Sum, f.Last, f.SumConfirmed, f.SumUnconfirmed, f.LastConfirmed, f.LastUnconfirmed} {
			if !check(cells) {
				return false
			}
		}
		for _, source := range f.Sources {
			if !check(source.Cells) {
				return false
			}
		}
		return true
	}
	for _, period := range periods {
		for _, d := range period.Departments {
			for _, f := range d.Forms {
				if !figure(f) {
					return ErrDependency.WithDetail("Số nguồn vượt giới hạn hoặc sai định dạng")
				}
			}
		}
		for _, f := range period.PriceAsOf {
			if !figure(f) {
				return ErrDependency.WithDetail("Số giá vượt giới hạn hoặc sai định dạng")
			}
		}
	}
	return nil
}
func validateAdhocFrozen(frozen map[string]map[string]domain.Frozen) error {
	valid := func(n *decimal.Decimal) bool {
		return n == nil || n.Exponent() >= -18 && n.Exponent() <= 18 && n.Coefficient().BitLen() <= 160
	}
	for _, m := range frozen {
		for _, f := range m {
			for _, n := range []*decimal.Decimal{f.Sum, &f.Cumulative, f.Increment, f.YearToDate, f.Unconfirmed} {
				if !valid(n) {
					return ErrDependency.WithDetail("Số đóng băng vượt giới hạn")
				}
			}
		}
	}
	return nil
}
