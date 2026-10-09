// Package domain là mô hình thuần của kho quan sát (W8.2): độ bất định có lan truyền, hạn dùng, ánh xạ bản ghi feed →
// quan sát. Không I/O, không đồng hồ hệ thống (thời điểm là tham số), không float (decimal).
package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Measured là một đại lượng có cận sai số: giá trị đúng nằm trong [Value − U, Value + U] (U ≥ 0, cùng đơn vị).
// Lan truyền theo cận tuyến tính (bảo thủ, không giả định phân phối, không cần căn bậc hai): engine dùng cận để định
// cỡ tồn đệm, nên thà rộng hơn còn hơn hẹp.
type Measured struct {
	Value decimal.Decimal
	U     decimal.Decimal
}

// Exact là đại lượng không sai số.
func Exact(v decimal.Decimal) Measured { return Measured{Value: v} }

// ErrNotDecimal: chuỗi không phải số thập phân của hợp đồng.
var ErrNotDecimal = errors.New("không phải số thập phân")

// Quantized đọc số thập phân dạng chuỗi của hợp đồng (vd "812.40"): sai số làm tròn của nguồn là nửa đơn vị chữ số
// cuối, U = 0,5 × 10^−(số chữ số lẻ). "812.40" → U = 0,005; "1" → U = 0,5.
func Quantized(s string) (Measured, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "eE") {
		return Measured{}, fmt.Errorf("%w: %q", ErrNotDecimal, s)
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return Measured{}, fmt.Errorf("%w: %q", ErrNotDecimal, s)
	}
	scale := int32(0)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		scale = int32(len(s) - i - 1)
	}
	return Measured{Value: v, U: decimal.New(5, -scale-1)}, nil
}

// Add: |Δ(a+b)| ≤ Ua + Ub.
func (a Measured) Add(b Measured) Measured {
	return Measured{Value: a.Value.Add(b.Value), U: a.U.Add(b.U)}
}

// Sub: |Δ(a−b)| ≤ Ua + Ub.
func (a Measured) Sub(b Measured) Measured {
	return Measured{Value: a.Value.Sub(b.Value), U: a.U.Add(b.U)}
}

// Mul: |Δ(ab)| ≤ |a|Ub + |b|Ua + UaUb (cận chặt của tích hai khoảng quanh tâm).
func (a Measured) Mul(b Measured) Measured {
	u := a.Value.Abs().Mul(b.U).Add(b.Value.Abs().Mul(a.U)).Add(a.U.Mul(b.U))
	return Measured{Value: a.Value.Mul(b.Value), U: u}
}

// ErrDivUncertain: mẫu số có thể bằng 0 trong khoảng sai số của nó.
var ErrDivUncertain = errors.New("mẫu số có thể bằng 0 trong cận sai số")

// divPrecision: số chữ số lẻ của thương (giá trị và cận).
const divPrecision = 12

// Div: |Δ(a/b)| ≤ (|a|Ub + |b|Ua) / (|b|(|b| − Ub)), cần |b| > Ub. Cận làm tròn LÊN để không hẹp đi.
func (a Measured) Div(b Measured) (Measured, error) {
	bb := b.Value.Abs()
	if bb.LessThanOrEqual(b.U) {
		return Measured{}, ErrDivUncertain
	}
	num := a.Value.Abs().Mul(b.U).Add(bb.Mul(a.U))
	den := bb.Mul(bb.Sub(b.U))
	return Measured{Value: a.Value.DivRound(b.Value, divPrecision), U: num.Div(den).RoundUp(divPrecision)}, nil
}

// Scale: nhân với hằng đúng k.
func (a Measured) Scale(k decimal.Decimal) Measured {
	return Measured{Value: a.Value.Mul(k), U: a.U.Mul(k.Abs())}
}

// Relative trả U/|Value| (làm tròn lên 6 chữ số lẻ); ok = false khi Value = 0.
func (a Measured) Relative() (decimal.Decimal, bool) {
	if a.Value.IsZero() {
		return decimal.Zero, false
	}
	return a.U.Div(a.Value.Abs()).RoundUp(6), true
}

// Widen cộng thêm cận bất định e ≥ 0 (vd ước tính thay ô bị ẩn dưới k).
func (a Measured) Widen(e decimal.Decimal) Measured {
	return Measured{Value: a.Value, U: a.U.Add(e.Abs())}
}
