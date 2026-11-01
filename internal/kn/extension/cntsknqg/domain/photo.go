package domain

import (
	"strings"
	"unicode/utf8"
)

// Ảnh thực địa Mẫu 03 (W11.53b — UR-TINH-21 "kiểm tra thực địa (kèm ảnh)").
const (
	MaxPhotoBytes   = 5 << 20 // một ảnh ≤ 5 MB (ảnh điện thoại đã nén)
	MaxPhotos       = 20      // ảnh hiện hành của một báo cáo
	MaxPhotoCaption = 300
)

// PhotoForms là mẫu nhận ảnh thực địa.
func PhotoForms(form string) bool { return form == Form03 }

// PhotoType kiểm ảnh: đuôi .jpg/.jpeg/.png KHỚP chữ ký đầu tệp (không tin kiểu do máy khách khai); trả kiểu, tên tệp đã
// làm sạch (bỏ đường dẫn, ký tự điều khiển; ≤ 200 ký tự).
func PhotoType(fileName string, head []byte) (ct, name string, ok bool) {
	ct, name, ok = ScanType(fileName, head)
	if !ok || (ct != "image/jpeg" && ct != "image/png") {
		return "", name, false
	}
	return ct, name, true
}

// NormalizeCaption cắt khoảng trắng; ok=false khi quá dài.
func NormalizeCaption(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= MaxPhotoCaption
}
