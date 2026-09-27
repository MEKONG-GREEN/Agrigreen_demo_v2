// Package domain: quy tắc thuần của kho định danh — chuẩn hóa, chỉ mục mù,
// nhãn hiển thị. Không phụ thuộc hạ tầng, không gọi time.Now (R-12).
package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Loại chỉ mục mù.
const (
	BlindPhone      = "phone"
	BlindNationalID = "national_id"
)

// Thao tác ghi vào sổ truy cập (vault.access_log.op).
const (
	OpCreate  = "create"
	OpDisplay = "display"
	OpResolve = "resolve"
	OpLookup  = "lookup"
	OpUnwrap  = "unwrap"
	OpErase   = "erase"
)

// Kết quả ghi vào sổ truy cập (vault.access_log.outcome).
const (
	OutcomeOK        = "ok"
	OutcomeNotFound  = "not_found"
	OutcomeErased    = "erased"
	OutcomeDuplicate = "duplicate"
	OutcomeRejected  = "rejected"
)

// RuleVersion của sổ truy cập.
const RuleVersion = "vault.access.v1"

// Lỗi kiểm dữ liệu. Thông báo KHÔNG chứa giá trị người dùng nhập (R-14).
var (
	ErrPhone      = errors.New("số điện thoại Việt Nam không hợp lệ")
	ErrNationalID = errors.New("số CCCD/CMND phải có 9 hoặc 12 chữ số")
)

// FieldError là lỗi theo trường.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

var digitsOnly = regexp.MustCompile(`^[0-9]+$`)

func stripSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '(', ')', '\t':
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// NormalizePhone đưa SĐT Việt Nam về dạng 84xxxxxxxxx (84 + 9 chữ số).
// Nhận 0xxxxxxxxx, +84xxxxxxxxx, 84xxxxxxxxx, 0084xxxxxxxxx, có thể có dấu cách, gạch, chấm.
func NormalizePhone(s string) (string, error) {
	p := stripSeparators(s)
	switch {
	case strings.HasPrefix(p, "+84"):
		p = p[3:]
	case strings.HasPrefix(p, "0084"):
		p = p[4:]
	case strings.HasPrefix(p, "84") && len(p) == 11:
		p = p[2:]
	case strings.HasPrefix(p, "0"):
		p = p[1:]
	default:
		return "", ErrPhone
	}
	if len(p) != 9 || !digitsOnly.MatchString(p) || p[0] == '0' {
		return "", ErrPhone
	}
	return "84" + p, nil
}

// NormalizeNationalID bỏ dấu cách/gạch; CCCD 12 số hoặc CMND 9 số.
func NormalizeNationalID(s string) (string, error) {
	p := stripSeparators(s)
	if (len(p) != 12 && len(p) != 9) || !digitsOnly.MatchString(p) {
		return "", ErrNationalID
	}
	return p, nil
}

// Candidate là một cách hiểu giá trị tra cứu.
type Candidate struct {
	Kind  string
	Value string // đã chuẩn hóa
}

// LookupCandidates trả mọi cách hiểu hợp lệ của một giá trị tra cứu (SĐT hoặc CCCD).
// Một chuỗi có thể vừa là SĐT vừa là CMND 9 số khi thiếu số 0 đầu — tra cả hai.
func LookupCandidates(s string) []Candidate {
	var out []Candidate
	if p, err := NormalizePhone(s); err == nil {
		out = append(out, Candidate{BlindPhone, p})
	}
	if n, err := NormalizeNationalID(s); err == nil {
		out = append(out, Candidate{BlindNationalID, n})
	}
	return out
}

// BlindIndex = HMAC-SHA256(khóa riêng của vault, tenant ‖ loại ‖ giá trị đã chuẩn hóa).
// Có tenant trong đầu vào nên cùng một SĐT ở hai tenant cho hai giá trị khác
// nhau — không liên kết chéo tenant được.
func BlindIndex(key []byte, tenant uuid.UUID, kind, normalized string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("ag-vault-blind-v1\x00"))
	m.Write(tenant[:])
	m.Write([]byte{0})
	m.Write([]byte(kind))
	m.Write([]byte{0})
	m.Write([]byte(normalized))
	return m.Sum(nil)
}

// NormalizeName gộp khoảng trắng thừa.
func NormalizeName(s string) string { return strings.Join(strings.Fields(s), " ") }

// Label che bớt họ tên: giữ nguyên từ đầu (họ), các từ sau chỉ còn chữ đầu.
// "Nguyễn Văn An" → "Nguyễn V. A."; "An" → "A.".
func Label(fullName string) string {
	words := strings.Fields(fullName)
	if len(words) == 0 {
		return ""
	}
	if len(words) == 1 {
		return initial(words[0])
	}
	parts := []string{words[0]}
	for _, w := range words[1:] {
		parts = append(parts, initial(w))
	}
	return strings.Join(parts, " ")
}

func initial(w string) string {
	r, _ := utf8.DecodeRuneInString(w)
	return string(unicode.ToUpper(r)) + "."
}

// MaskPhone che SĐT đã chuẩn hóa 84xxxxxxxxx thành "09xx xxx 123".
func MaskPhone(normalized string) string {
	if len(normalized) != 11 || !strings.HasPrefix(normalized, "84") {
		return ""
	}
	local := "0" + normalized[2:]
	return local[:2] + "xx xxx " + local[len(local)-3:]
}

var areaRe = regexp.MustCompile(`^[0-9A-Za-z._-]{1,32}$`)

// ValidArea kiểm mã địa bàn (mã xã/tỉnh).
func ValidArea(s string) bool { return areaRe.MatchString(s) }

var purposeRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,63}$`)

// ValidPurpose kiểm dạng mục đích — cùng quy ước với pf_party.consent.purpose.
func ValidPurpose(s string) bool { return purposeRe.MatchString(s) }

// ValidReason kiểm lý do xóa: 10–500 ký tự.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 10 && n <= 500
}

// AAD gắn bản mã với đúng (tenant, chủ thể, mục dùng) để không đổi chỗ bản mã giữa các dòng.
func AAD(use string, tenant, ref uuid.UUID) []byte {
	return []byte("ag-vault/" + use + "/" + tenant.String() + "/" + ref.String())
}
