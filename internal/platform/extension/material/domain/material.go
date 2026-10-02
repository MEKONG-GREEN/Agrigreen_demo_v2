// Package domain: quy tắc sổ vật tư KN v7.7 (FN-XA-03.06) — số lượng theo ĐVT, ngày ghi nhận, ghi chú không PII.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleVersion ghim vào mọi dòng sổ material_record.
const RuleVersion = "extension.material.v1"

// MaxLines là số dòng tối đa của một lần ghi nhận.
const MaxLines = 200

// CountableUnits là ĐVT đếm được — số lượng phải là số nguyên (tạm, chờ khách: Q về danh mục ĐVT).
var CountableUnits = []string{"bao", "cái", "chai", "gói", "con", "chiếc", "cây", "hộp"}

// MaxQuantity là số lượng tối đa của một dòng (tạm, chờ khách).
var MaxQuantity = decimal.NewFromInt(1_000_000_000)

// MaxScale là số chữ số lẻ tối đa với ĐVT không đếm được (khớp numeric(14,4)).
const MaxScale = 4

// Countable cho biết ĐVT đếm được (so không phân biệt hoa thường, bỏ khoảng trắng).
func Countable(unit string) bool {
	u := strings.ToLower(strings.TrimSpace(unit))
	for _, c := range CountableUnits {
		if u == c {
			return true
		}
	}
	return false
}

// ErrQuantity mô tả số lượng không hợp lệ theo ĐVT.
var (
	ErrQuantityNegative = errors.New("số lượng không được âm")
	ErrQuantityTooLarge = errors.New("số lượng vượt 1.000.000.000")
	ErrQuantityInteger  = errors.New("ĐVT đếm được: số lượng phải là số nguyên")
	ErrQuantityScale    = errors.New("số lượng tối đa 4 chữ số thập phân")
)

// CheckQuantity áp quy tắc ĐVT: ≥ 0; ≤ 1e9; ĐVT đếm được → số nguyên; còn lại ≤ 4 chữ số lẻ.
func CheckQuantity(q decimal.Decimal, unit string) error {
	switch {
	case q.IsNegative():
		return ErrQuantityNegative
	case q.GreaterThan(MaxQuantity):
		return ErrQuantityTooLarge
	case Countable(unit) && !q.Equal(q.Truncate(0)):
		return ErrQuantityInteger
	case !q.Equal(q.Round(MaxScale)):
		return ErrQuantityScale
	}
	return nil
}

// VN là giờ Việt Nam (UTC+7, không đổi giờ).
var VN = time.FixedZone("ICT", 7*3600)

// Today là ngày hiện tại theo giờ Việt Nam.
func Today(now time.Time) time.Time {
	y, m, d := now.In(VN).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDate đọc ngày YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) { return time.Parse(time.DateOnly, strings.TrimSpace(s)) }

// Dấu hiệu dữ liệu cá nhân trong văn bản tự do: SĐT Việt Nam, số CCCD 12 chữ số (như extension/domain).
var (
	phoneRe      = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalIDRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ContainsPII phát hiện SĐT/CCCD trong ghi chú.
func ContainsPII(s string) bool { return phoneRe.MatchString(s) || nationalIDRe.MatchString(s) }

// NoteError kiểm ghi chú: ≤ 1000 ký tự, không SĐT/CCCD. Trả lý do hoặc rỗng.
func NoteError(s string) string {
	if utf8.RuneCountInString(s) > 1000 {
		return "tối đa 1000 ký tự"
	}
	if ContainsPII(s) {
		return "không ghi SĐT hoặc số CCCD vào ghi chú"
	}
	return ""
}

// ValidReason: lý do sửa 10–500 ký tự.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 10 && n <= 500
}
