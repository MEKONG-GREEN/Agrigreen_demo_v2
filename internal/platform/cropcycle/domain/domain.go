// Package domain: quy tắc nghiệp vụ của pf/cropcycle (van thu gom MKG ↔ HTX, sản phẩm collection_volume).
// Không phụ thuộc hạ tầng; không đọc đồng hồ (R-12). Quy tắc vụ/nhật ký/SOP chuyển sang pf/cooperative (W3.3).
package domain

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// FieldError là lỗi dữ liệu vào theo trường; app quy về problem.Validation.
type FieldError struct {
	Field  string
	Reason string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Reason }

// AsFieldError trả FieldError nếu err là lỗi theo trường.
func AsFieldError(err error) (FieldError, bool) {
	var fe FieldError
	ok := errors.As(err, &fe)
	return fe, ok
}

var (
	phoneRe    = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ContainsPII phát hiện SĐT Việt Nam hoặc số CCCD 12 chữ số trong văn bản tự do
// (dữ liệu cá nhân chỉ ở kho định danh — R-41).
func ContainsPII(s string) bool { return phoneRe.MatchString(s) || nationalRe.MatchString(s) }

// CleanText cắt khoảng trắng và kiểm độ dài/ký tự điều khiển của văn bản tự do.
func CleanText(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > max {
		return "", FieldError{field, "tối đa " + strconv.Itoa(max) + " ký tự"}
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", FieldError{field, "chứa ký tự điều khiển"}
		}
	}
	return s, nil
}

// ParseDay đọc YYYY-MM-DD.
func ParseDay(s string) (time.Time, error) { return time.Parse(time.DateOnly, strings.TrimSpace(s)) }

// FormatDay trả YYYY-MM-DD.
func FormatDay(d time.Time) string { return d.Format(time.DateOnly) }
