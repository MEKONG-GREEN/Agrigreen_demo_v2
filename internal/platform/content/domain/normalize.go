package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalize đưa chuỗi về dạng tìm kiếm không dấu: bỏ dấu tiếng Việt (NFD rồi
// bỏ dấu kết hợp), đ/Đ → d, chữ thường, mọi ký tự không phải chữ/số thành một
// khoảng trắng. "Lúa Đông Xuân" → "lua dong xuan".
//
// App ngoại tuyến phải dùng đúng quy tắc này để tìm trên dữ liệu đã tải.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'đ' || r == 'Đ':
			r = 'd'
		}
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// maxQueryTerms giới hạn số từ trong một truy vấn.
const maxQueryTerms = 8

// SearchQuery dựng biểu thức to_tsquery('simple', …) từ chuỗi người dùng nhập:
// mỗi từ đã chuẩn hóa thành tiền tố "từ:*", nối bằng "&". Chỉ còn chữ/số nên an
// toàn khi truyền làm tham số. Rỗng → không lọc theo từ khóa.
func SearchQuery(q string) string {
	terms := strings.Fields(Normalize(q))
	if len(terms) > maxQueryTerms {
		terms = terms[:maxQueryTerms]
	}
	for i, t := range terms {
		terms[i] = t + ":*"
	}
	return strings.Join(terms, " & ")
}

// SearchText dựng văn bản lập chỉ mục của một phiên bản.
func SearchText(c Content) string {
	parts := []string{c.Title, c.Summary, PlainText(c.Body), c.Category}
	for _, t := range c.Tags {
		_, code, _ := strings.Cut(t, ":")
		parts = append(parts, code)
	}
	for _, a := range c.Attachments {
		parts = append(parts, a.FileName)
	}
	s := Normalize(strings.Join(parts, " "))
	// tsvector giới hạn ~1 MB; cắt an toàn theo byte (chuỗi đã là ASCII/chữ thường).
	const limit = 256 * 1024
	if len(s) > limit {
		s = s[:limit]
		if i := strings.LastIndexByte(s, ' '); i > 0 {
			s = s[:i]
		}
	}
	return s
}
