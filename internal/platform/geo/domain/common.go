package domain

import (
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// Hành động ngừng dùng / khôi phục trên các sổ phiên bản (cơ sở, đường bộ). Thửa và ranh giới HTX ở pf/cooperative
// (W3.3) dùng cùng giá trị.
const (
	ActionRetired  = "retired"
	ActionRestored = "restored"
)

// MaxAreaHa là diện tích tối đa hợp lý (ha) của polygon cơ sở (kho/Hub) — cùng giới hạn của một thửa.
var MaxAreaHa = decimal.RequireFromString("1000")

// ValidReason: lý do sửa/ngừng dùng bắt buộc, ≥ 10 ký tự (RB-1).
func ValidReason(s string) bool { return utf8.RuneCountInString(strings.TrimSpace(s)) >= 10 }
