package domain

// Ngưỡng cảnh báo giá cao / thấp bất thường (W11.74 — BRD Báo giá v1.3 FN-AD-04.06, CT-02…CT-04, CT-06). Mọi số là
// decimal (R-CODE-01): ngưỡng %, Δ%, ngưỡng đi ngang, ngưỡng phiếu điều tra.

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/shopspring/decimal"
)

// Phạm vi ngưỡng (BR-02: mặt hàng → nhóm hàng → mặc định).
const (
	ThresholdScopeDefault = "default"
	ThresholdScopeGroup   = "group"
	ThresholdScopeItem    = "item"
)

var (
	pctHundred = decimal.NewFromInt(100)
	// reThresholdItem: mã mặt hàng (W11.73 sở hữu danh mục; ở đây chỉ kiểm dạng — CHECK pf 000139).
	reThresholdItem = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,39}$`)
)

// AlertParams là tham số chung đi cùng ngưỡng mặc định (FN-AD-04.06 B3).
type AlertParams struct {
	LookbackDays int             // N: số ngày báo giá tìm giá kỳ trước (CT-02, mặc định 7)
	FlatPct      decimal.Decimal // ngưỡng đi ngang của bảng giá tổng hợp (CT-04, mặc định 1 %)
	SurveyPct    decimal.Decimal // ngưỡng lệch trung vị phiếu điều tra (CT-06, mặc định 40 %)
}

// Threshold là một phiên bản ngưỡng đang hiệu lực. Phạm vi default mang thêm AlertParams.
type Threshold struct {
	Scope   string
	Code    string // mã nhóm hàng / mặt hàng; rỗng với default
	Pct     decimal.Decimal
	Params  *AlertParams // chỉ với default
	Version int64
}

// ErrDefaultThreshold: không xóa được ngưỡng mặc định (FN-AD-04.06 AC-04).
var ErrDefaultThreshold = errors.New("price: không xóa được ngưỡng mặc định")

// ValidPct cho biết 0 < p ≤ 100 và tối đa 2 chữ số thập phân (numeric(5,2)).
func ValidPct(p decimal.Decimal) bool {
	return p.IsPositive() && p.LessThanOrEqual(pctHundred) && p.Equal(p.Round(2))
}

// ValidateThreshold kiểm một ngưỡng (cùng ràng buộc CHECK của pf 000139). Ngưỡng ≤ 0 % hoặc > 100 % bị chặn.
func ValidateThreshold(t Threshold) error {
	switch t.Scope {
	case ThresholdScopeDefault:
		if t.Code != "" {
			return fmt.Errorf("%w: ngưỡng mặc định không có mã", ErrInvalid)
		}
		if t.Params == nil {
			return fmt.Errorf("%w: ngưỡng mặc định thiếu tham số chung", ErrInvalid)
		}
		if t.Params.LookbackDays < 1 || t.Params.LookbackDays > 60 {
			return fmt.Errorf("%w: số ngày tìm giá kỳ trước 1–60", ErrInvalid)
		}
		if !ValidPct(t.Params.FlatPct) || !ValidPct(t.Params.SurveyPct) {
			return fmt.Errorf("%w: ngưỡng đi ngang, ngưỡng phiếu điều tra phải trong (0; 100] %%, tối đa 2 số lẻ", ErrInvalid)
		}
	case ThresholdScopeGroup:
		if !reGroupCode.MatchString(t.Code) {
			return fmt.Errorf("%w: mã nhóm hàng %q", ErrInvalid, t.Code)
		}
	case ThresholdScopeItem:
		if !reThresholdItem.MatchString(t.Code) {
			return fmt.Errorf("%w: mã mặt hàng %q", ErrInvalid, t.Code)
		}
	default:
		return fmt.Errorf("%w: phạm vi %q", ErrInvalid, t.Scope)
	}
	if t.Scope != ThresholdScopeDefault && t.Params != nil {
		return fmt.Errorf("%w: chỉ ngưỡng mặc định có tham số chung", ErrInvalid)
	}
	if !ValidPct(t.Pct) {
		return fmt.Errorf("%w: ngưỡng phải trong (0; 100] %%, tối đa 2 số lẻ", ErrInvalid)
	}
	return nil
}

// ThresholdSet là các ngưỡng đang hiệu lực của một bên dùng (default luôn có — từ sổ hoặc mặc định của Profile).
type ThresholdSet struct {
	Default Threshold
	Groups  map[string]Threshold
	Items   map[string]Threshold
}

// For trả ngưỡng áp cho mặt hàng item thuộc nhóm group (BR-02: mặt hàng → nhóm → mặc định) và phạm vi đã dùng.
func (s ThresholdSet) For(group, item string) Threshold {
	if t, ok := s.Items[item]; ok && item != "" {
		return t
	}
	if t, ok := s.Groups[group]; ok && group != "" {
		return t
	}
	return s.Default
}

// Hướng cảnh báo (CT-03).
const (
	AlertNone = ""
	AlertHigh = "high" // Cao bất thường (▲)
	AlertLow  = "low"  // Thấp bất thường (▼)
)

// ChangePct trả Δ% = (p − ref) / ref × 100 (CT-03), làm tròn 4 số lẻ; ref ≤ 0 → false ("Giá mới", không cảnh báo).
func ChangePct(p, ref decimal.Decimal) (decimal.Decimal, bool) {
	if !ref.IsPositive() {
		return decimal.Zero, false
	}
	return p.Sub(ref).Mul(pctHundred).DivRound(ref, 4), true
}

// Classify xếp Δ% theo ngưỡng T: Δ% ≥ +T → Cao; Δ% ≤ −T → Thấp; còn lại không cảnh báo (FN-AD-04.06 BR-01, AC-02).
func Classify(delta, threshold decimal.Decimal) string {
	switch {
	case delta.GreaterThanOrEqual(threshold):
		return AlertHigh
	case delta.LessThanOrEqual(threshold.Neg()):
		return AlertLow
	}
	return AlertNone
}

// Flat cho biết biến động đi ngang của bảng giá tổng hợp: |Δ%| < ngưỡng đi ngang (CT-04).
func Flat(delta, flatPct decimal.Decimal) bool { return delta.Abs().LessThan(flatPct) }
