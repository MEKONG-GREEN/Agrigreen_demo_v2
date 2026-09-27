// Package money là kiểu tiền của hệ thống (R-10). Không bao giờ dùng float.
//
// Ánh xạ DB: tiền numeric(18,2), đơn giá numeric(18,4), tỷ lệ numeric(9,6).
package money

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Số chữ số thập phân theo quy ước DB.
const (
	AmountScale = 2
	PriceScale  = 4
	RateScale   = 6
)

var (
	// ErrScale báo giá trị có nhiều chữ số thập phân hơn cột DB cho phép.
	ErrScale = errors.New("money: vượt số chữ số thập phân cho phép")
	// ErrRange báo giá trị vượt độ rộng cột DB.
	ErrRange = errors.New("money: vượt độ rộng cho phép")
)

// Amount là số tiền VND, numeric(18,2).
type Amount struct{ d decimal.Decimal }

// Price là đơn giá, numeric(18,4).
type Price struct{ d decimal.Decimal }

// Rate là tỷ lệ, numeric(9,6).
type Rate struct{ d decimal.Decimal }

func parse(s string, scale int32, intDigits int) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("money: %q không phải số thập phân: %w", s, err)
	}
	return check(d, scale, intDigits)
}

func check(d decimal.Decimal, scale int32, intDigits int) (decimal.Decimal, error) {
	if !d.Equal(d.Truncate(scale)) {
		return decimal.Decimal{}, fmt.Errorf("%w: %s (tối đa %d)", ErrScale, d, scale)
	}
	limit := decimal.New(1, int32(intDigits))
	if d.Abs().GreaterThanOrEqual(limit) {
		return decimal.Decimal{}, fmt.Errorf("%w: %s", ErrRange, d)
	}
	return d, nil
}

// ParseAmount đọc số tiền từ chuỗi, ví dụ "1250000.50".
func ParseAmount(s string) (Amount, error) {
	d, err := parse(s, AmountScale, 18-AmountScale)
	return Amount{d}, err
}

// MustAmount dùng cho hằng số và kiểm thử.
func MustAmount(s string) Amount {
	a, err := ParseAmount(s)
	if err != nil {
		panic(err)
	}
	return a
}

// ParsePrice đọc đơn giá.
func ParsePrice(s string) (Price, error) {
	d, err := parse(s, PriceScale, 18-PriceScale)
	return Price{d}, err
}

// ParseRate đọc tỷ lệ.
func ParseRate(s string) (Rate, error) {
	d, err := parse(s, RateScale, 9-RateScale)
	return Rate{d}, err
}

// Decimal trả về giá trị decimal bên trong.
func (a Amount) Decimal() decimal.Decimal { return a.d }

// String trả về dạng cố định 2 chữ số thập phân.
func (a Amount) String() string { return a.d.StringFixed(AmountScale) }

// Add cộng hai số tiền.
func (a Amount) Add(b Amount) Amount { return Amount{a.d.Add(b.d)} }

// Sub trừ hai số tiền.
func (a Amount) Sub(b Amount) Amount { return Amount{a.d.Sub(b.d)} }

// Neg đổi dấu, dùng cho bản ghi bù.
func (a Amount) Neg() Amount { return Amount{a.d.Neg()} }

// IsZero cho biết số tiền bằng 0.
func (a Amount) IsZero() bool { return a.d.IsZero() }

// Equal so sánh bằng theo giá trị.
func (a Amount) Equal(b Amount) bool { return a.d.Equal(b.d) }

// Mul nhân số tiền với tỷ lệ, làm tròn nửa lên về 2 chữ số (quy tắc làm tròn
// phải ghi rule_version ở bản ghi kết quả của module gọi).
func (a Amount) Mul(r Rate) Amount { return Amount{a.d.Mul(r.d).Round(AmountScale)} }

// Decimal trả về giá trị decimal bên trong.
func (p Price) Decimal() decimal.Decimal { return p.d }

// String trả về dạng cố định 4 chữ số thập phân.
func (p Price) String() string { return p.d.StringFixed(PriceScale) }

// Decimal trả về giá trị decimal bên trong.
func (r Rate) Decimal() decimal.Decimal { return r.d }

// String trả về dạng cố định 6 chữ số thập phân.
func (r Rate) String() string { return r.d.StringFixed(RateScale) }

// MarshalJSON ghi số tiền dưới dạng chuỗi để client không đọc thành float.
func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }

// UnmarshalJSON chỉ nhận chuỗi.
func (a *Amount) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("money: số tiền phải là chuỗi JSON, ví dụ \"1250000.50\"")
	}
	v, err := ParseAmount(s)
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// Value ghi xuống DB dạng chuỗi numeric.
func (a Amount) Value() (driver.Value, error) { return a.String(), nil }

// Scan đọc từ cột numeric.
func (a *Amount) Scan(src any) error {
	d, err := scanDecimal(src)
	if err != nil {
		return err
	}
	*a = Amount{d}
	return nil
}

func scanDecimal(src any) (decimal.Decimal, error) {
	switch v := src.(type) {
	case string:
		return decimal.NewFromString(v)
	case []byte:
		return decimal.NewFromString(string(v))
	case nil:
		return decimal.Decimal{}, errors.New("money: giá trị NULL")
	default:
		return decimal.Decimal{}, fmt.Errorf("money: không đọc được kiểu %T", src)
	}
}
