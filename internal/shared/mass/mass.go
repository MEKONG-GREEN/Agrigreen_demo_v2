// Package mass là kiểu khối lượng của hệ thống (R-10). Không bao giờ dùng float.
//
// Đơn vị gốc là kg, numeric(14,3). Tấn chỉ là cách hiển thị và quy đổi.
package mass

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Scale là số chữ số thập phân của cột kg.
const Scale = 3

const intDigits = 14 - Scale

var (
	// ErrScale báo giá trị có nhiều hơn 3 chữ số thập phân.
	ErrScale = errors.New("mass: vượt 3 chữ số thập phân")
	// ErrRange báo giá trị vượt numeric(14,3).
	ErrRange = errors.New("mass: vượt độ rộng cho phép")
	// ErrNegative báo khối lượng âm ở nơi không cho phép.
	ErrNegative = errors.New("mass: khối lượng âm")
)

var kgPerTonne = decimal.NewFromInt(1000)

// Kg là khối lượng tính bằng kg.
type Kg struct{ d decimal.Decimal }

// ParseKg đọc khối lượng kg từ chuỗi.
func ParseKg(s string) (Kg, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Kg{}, fmt.Errorf("mass: %q không phải số thập phân: %w", s, err)
	}
	return fromDecimal(d)
}

// MustKg dùng cho hằng số và kiểm thử.
func MustKg(s string) Kg {
	k, err := ParseKg(s)
	if err != nil {
		panic(err)
	}
	return k
}

// FromTonnes quy đổi tấn sang kg; tấn có tối đa 6 chữ số thập phân để kg không mất chính xác.
func FromTonnes(s string) (Kg, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Kg{}, fmt.Errorf("mass: %q không phải số thập phân: %w", s, err)
	}
	return fromDecimal(d.Mul(kgPerTonne))
}

func fromDecimal(d decimal.Decimal) (Kg, error) {
	if !d.Equal(d.Truncate(Scale)) {
		return Kg{}, fmt.Errorf("%w: %s", ErrScale, d)
	}
	if d.Abs().GreaterThanOrEqual(decimal.New(1, intDigits)) {
		return Kg{}, fmt.Errorf("%w: %s", ErrRange, d)
	}
	return Kg{d}, nil
}

// NonNegative kiểm khối lượng không âm (khối lượng cân luôn không âm; bản ghi bù mới được âm).
func (k Kg) NonNegative() error {
	if k.d.IsNegative() {
		return ErrNegative
	}
	return nil
}

// Decimal trả về giá trị decimal bên trong.
func (k Kg) Decimal() decimal.Decimal { return k.d }

// String trả về kg với 3 chữ số thập phân.
func (k Kg) String() string { return k.d.StringFixed(Scale) }

// Tonnes trả về tấn dạng chuỗi 6 chữ số thập phân — chỉ để hiển thị.
func (k Kg) Tonnes() string { return k.d.Div(kgPerTonne).StringFixed(6) }

// Add cộng khối lượng.
func (k Kg) Add(o Kg) Kg { return Kg{k.d.Add(o.d)} }

// Sub trừ khối lượng.
func (k Kg) Sub(o Kg) Kg { return Kg{k.d.Sub(o.d)} }

// Cmp so sánh: -1, 0, 1.
func (k Kg) Cmp(o Kg) int { return k.d.Cmp(o.d) }

// PerUnit chia khối lượng cho số đơn vị (vd kg/cuộn), làm tròn nửa lên về 3 chữ số.
func (k Kg) PerUnit(n int64) (Kg, error) {
	if n <= 0 {
		return Kg{}, errors.New("mass: số đơn vị phải dương")
	}
	return Kg{k.d.Div(decimal.NewFromInt(n)).Round(Scale)}, nil
}

// MarshalJSON ghi dạng chuỗi.
func (k Kg) MarshalJSON() ([]byte, error) { return json.Marshal(k.String()) }

// UnmarshalJSON chỉ nhận chuỗi.
func (k *Kg) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("mass: khối lượng phải là chuỗi JSON, ví dụ \"1520.500\"")
	}
	v, err := ParseKg(s)
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// Value ghi xuống DB dạng chuỗi numeric.
func (k Kg) Value() (driver.Value, error) { return k.String(), nil }

// Scan đọc từ cột numeric.
func (k *Kg) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("mass: không đọc được kiểu %T", src)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return err
	}
	*k = Kg{d}
	return nil
}
