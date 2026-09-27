// Package domain chứa quy tắc nghiệp vụ Bản đồ CGH: mã hiển thị, chủ sở hữu,
// máy, diện tích vụ (QT-03) và cân đối cung–cầu (QT-01/QT-02). Không phụ thuộc
// hạ tầng, không đọc đồng hồ (R-12).
package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Tiền tố mã hiển thị.
const (
	PrefixOwner   = "CSH" // FN-04 BR-02
	PrefixMachine = "MAY" // FN-05 BR-02
)

// NoProvince là nhãn tỉnh mặc định khi chủ sở hữu không gắn HTX (FN-04 BR-02: CSH-XX-).
const NoProvince = "XX"

// Các tiền tố hành chính bỏ đi trước khi lấy chữ cái đầu.
var provincePrefixes = []string{"thành phố ", "tỉnh ", "tp. ", "tp "}

// vnFold bỏ dấu tiếng Việt của một chữ cái (đ → d).
func vnFold(r rune) rune {
	const groups = "aàáạảãâầấậẩẫăằắặẳẵ|eèéẹẻẽêềếệểễ|iìíịỉĩ|oòóọỏõôồốộổỗơờớợởỡ|uùúụủũưừứựửữ|yỳýỵỷỹ|dđ"
	lr := unicode.ToLower(r)
	for _, g := range strings.Split(groups, "|") {
		if strings.ContainsRune(g, lr) {
			base := []rune(g)[0]
			if unicode.IsUpper(r) {
				return unicode.ToUpper(base)
			}
			return base
		}
	}
	return r
}

// Fold bỏ dấu tiếng Việt và viết thường — dùng so khớp tên, nhãn tình trạng.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		b.WriteRune(vnFold(r))
	}
	return b.String()
}

// ProvinceLabel là "tên viết tắt tỉnh" trong mã CSH-/MAY- (FN-04/FN-05 BR-02):
// chữ cái đầu của các từ trong tên tỉnh, bỏ dấu, bỏ tiền tố "Tỉnh"/"Thành phố".
// "An Giang" → AG, "Cần Thơ" → CT, "Đồng Tháp" → DT, "Hồ Chí Minh" → HCM.
// Tên một từ lấy hai chữ đầu. Danh mục đơn vị hành chính chưa có trường viết tắt
// chính thức; hai tỉnh trùng chữ đầu vẫn không trùng mã vì bộ đếm theo nhãn.
func ProvinceLabel(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range provincePrefixes {
		n = strings.TrimPrefix(n, p)
	}
	var letters []rune
	words := strings.Fields(Fold(n))
	for _, w := range words {
		for _, r := range w {
			if r >= 'a' && r <= 'z' {
				letters = append(letters, unicode.ToUpper(r))
				break
			}
		}
	}
	if len(words) == 1 {
		letters = letters[:0]
		for _, r := range words[0] {
			if r >= 'a' && r <= 'z' && len(letters) < 2 {
				letters = append(letters, unicode.ToUpper(r))
			}
		}
	}
	if len(letters) < 2 {
		return NoProvince
	}
	if len(letters) > 6 {
		letters = letters[:6]
	}
	return string(letters)
}

// FormatCode dựng mã hiển thị, vd FormatCode("MAY", "AG", 1) = "MAY-AG-00001".
func FormatCode(prefix, province string, n int) string {
	return fmt.Sprintf("%s-%s-%05d", prefix, province, n)
}

// ErrReason: lý do quá ngắn.
var ErrReason = errors.New("lý do bắt buộc, ít nhất 10 ký tự")

// ValidReason kiểm lý do của thao tác vô hiệu hóa.
func ValidReason(s string) bool { return len([]rune(strings.TrimSpace(s))) >= 10 }
