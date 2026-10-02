package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ActionAttributesChanged: sửa thuộc tính thửa KN v7.7 (FN-XA-03.05) — sự kiện bù trên plot_event.
const ActionAttributesChanged = "attributes_changed"

// Cách vẽ thửa KN v7.7: chấm điểm, hình tròn, hình vuông (máy đổi tròn/vuông thành đa giác trước khi gửi).
const (
	ShapePolygon = "polygon"
	ShapeCircle  = "circle"
	ShapeSquare  = "square"
)

// KnownShape cho biết cách vẽ hợp lệ.
func KnownShape(s string) bool { return s == ShapePolygon || s == ShapeCircle || s == ShapeSquare }

// Giới hạn độ dài thuộc tính v7.7.
const (
	MaxLotName    = 200
	MaxNote       = 1000
	MaxFarmerName = 200
)

// Dấu hiệu dữ liệu cá nhân trong văn bản tự do (cùng quy tắc với pf/extension): SĐT Việt Nam, CCCD 12 chữ số.
var (
	phoneRe      = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalIDRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ContainsPII phát hiện SĐT/CCCD trong ghi chú, tên lô (R-04: dữ liệu cá nhân chỉ ở kho định danh).
func ContainsPII(s string) bool { return phoneRe.MatchString(s) || nationalIDRe.MatchString(s) }

// TextOK kiểm một trường văn bản tự do: độ dài tối đa (ký tự), không chứa SĐT/CCCD. Trả lý do lỗi hoặc rỗng.
func TextOK(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "quá dài"
	}
	if ContainsPII(s) {
		return "không ghi SĐT hoặc số CCCD vào trường này"
	}
	return ""
}
