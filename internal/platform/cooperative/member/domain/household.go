// Package domain: quy tắc thuần của nông hộ và xã viên HTX (pf/cooperative, chuyển từ pf/party ở W3.3).
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Household là hồ sơ nông hộ ở pf_coop: CHỈ mã tham chiếu và thuộc tính không
// định danh (R-04). Họ tên, SĐT, CCCD, địa chỉ nằm ở kho định danh.
type Household struct {
	Ref              uuid.UUID // = Created.Ref của kho định danh
	CommuneCode      string
	CultivatedAreaHa *decimal.Decimal
	Active           bool
	CooperativeRef   uuid.UUID  // HTX hiện tại; Nil nếu chưa là thành viên
	MemberSince      *time.Time // ngày vào HTX hiện tại
	CreatedAt        time.Time
}

// Lỗi kiểm dữ liệu nông hộ (thông điệp giữ nguyên từ pf/party — hợp đồng HTTP không đổi).
var (
	ErrIdentity    = errors.New("họ tên và SĐT của chủ hộ là bắt buộc")
	ErrCooperative = errors.New("cooperative_ref bắt buộc, phải là HTX đang hoạt động")
	ErrArea        = errors.New("diện tích (ha) phải là số không âm, tối đa 4 chữ số thập phân")
	ErrDate        = errors.New("ngày không hợp lệ hoặc ở tương lai")
)

// FieldError gắn lỗi với tên trường.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e FieldError) Unwrap() error { return e.Err }

// ValidArea: diện tích không âm, tối đa 4 chữ số thập phân, dưới 10^10 (numeric(14,4)).
func ValidArea(a decimal.Decimal) bool {
	return !a.IsNegative() && a.Exponent() >= -4 && a.LessThan(decimal.New(1, 10))
}

// ValidateHousehold kiểm thuộc tính không định danh của nông hộ so với now.
func ValidateHousehold(area *decimal.Decimal, since *time.Time, now time.Time) error {
	if area != nil && !ValidArea(*area) {
		return FieldError{"cultivated_area_ha", ErrArea}
	}
	if since != nil && since.After(now) {
		return FieldError{"member_since", ErrDate}
	}
	return nil
}

// FieldChange là một dòng "giá trị cũ → mới" của Log (kn-v7.7-plan §1.5).
type FieldChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

// Diff so hai ảnh chụp JSON (object phẳng) và trả các trường đổi, sắp theo tên. before nil (tạo
// mới) → mọi trường của after là mới.
func Diff(before, after []byte) []FieldChange {
	var b, a map[string]any
	decodeObj(before, &b)
	decodeObj(after, &a)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	out := []FieldChange{}
	for _, k := range names {
		ov, ok1 := b[k]
		nv, ok2 := a[k]
		o, n := scalar(ov, ok1), scalar(nv, ok2)
		if eq(o, n) {
			continue
		}
		out = append(out, FieldChange{Field: k, Old: o, New: n})
	}
	return out
}

// decodeObj đọc object JSON giữ số dạng chuỗi (không qua float).
func decodeObj(raw []byte, out *map[string]any) {
	if len(raw) == 0 {
		return
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	_ = d.Decode(out)
}

func scalar(v any, ok bool) *string {
	if !ok || v == nil {
		return nil
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case json.Number:
		s = x.String()
	default:
		raw, _ := json.Marshal(x)
		s = string(raw)
	}
	return &s
}

func eq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
