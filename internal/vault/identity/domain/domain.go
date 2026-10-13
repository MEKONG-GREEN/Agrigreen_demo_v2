// Package domain: quy tắc thuần của kho định danh — chuẩn hóa, chỉ mục mù,
// nhãn hiển thị. Không phụ thuộc hạ tầng, không gọi time.Now (R-12).
package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"regexp"
	"sort"
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
	// W11.22 (vault 000004): sửa định danh, ghi/đọc ngăn hồ sơ.
	OpAmend        = "amend"
	OpSectionWrite = "section_write"
	OpSectionRead  = "section_read"
	// W11.23 (vault 20261013165158): đọc thẻ danh bạ theo lô (họ tên, SĐT, năm sinh, email — chỉ trường được hỏi).
	OpCard = "card"
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

// MaskPhone che SĐT đã chuẩn hóa 84xxxxxxxxx theo mẫu 4 số đầu · 3 số cuối
// (UAT DEF-CGH-08): 84912345678 → "0912•••678".
func MaskPhone(normalized string) string {
	if len(normalized) != 11 || !strings.HasPrefix(normalized, "84") {
		return ""
	}
	local := "0" + normalized[2:]
	return local[:4] + "•••" + local[len(local)-3:]
}

var areaRe = regexp.MustCompile(`^[0-9A-Za-z._-]{1,32}$`)

// ValidArea kiểm mã địa bàn (mã xã/tỉnh).
func ValidArea(s string) bool { return areaRe.MatchString(s) }

var purposeRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{2,63}$`)

// ValidPurpose kiểm dạng mục đích — cùng quy ước với pf_party.consent.purpose.
func ValidPurpose(s string) bool { return purposeRe.MatchString(s) }

// opPurposes là danh mục mục đích ĐƯỢC PHÉP theo thao tác (R-42, M13). Mỗi thao tác
// gắn với một tập vai cố định (client/roles.go: Create/Lookup/Resolve chỉ
// vault_registrar, Unwrap chỉ vault_evidence_viewer, Display ba vai đọc), nên danh
// mục theo thao tác cũng là danh mục theo vai. Rút từ mọi nơi gọi kho hiện có
// (pf/party, pf/accounts, pf/geo farmerid, pf/mechanization, pf/sync files) — mục
// đích ngoài danh mục bị từ chối dù đúng dạng, để token bị lộ không thể "khai" mục
// đích tùy ý mà dò SĐT/CCCD. Thêm mục đích mới = sửa danh mục này + kiểm thử.
//
// W11.22: hồ sơ nhân sự HCTH (pf/hrm) dùng PurposeHRMProfile cho định danh và ngăn hrm.profile, PurposeHRMSensitive CHỈ
// cho ngăn hrm.sensitive (thẻ Thống kê BC Sở) — ngăn gắn cứng với mục đích (SectionPurpose).
var opPurposes = map[string]map[string]bool{
	OpCreate: set("registry.admin", "registry.dedup", "extension.contact", "mechanization.owner", PurposeHRMProfile,
		PurposeContactDirectory),
	OpLookup: set("registry.admin", "registry.dedup", PurposeHRMProfile),
	// Resolve (định danh đầy đủ): quản trị hồ sơ, liên hệ hộ, khuyến nông hỗ trợ hộ, hồ sơ nhân sự.
	OpResolve: set("registry.admin", "extension.contact", "extension.field_support", PurposeHRMProfile),
	OpDisplay: set("registry.admin", "extension.contact", "extension.field_support",
		"mechanization.owner", "mrv.evidence", PurposeHRMProfile),
	OpAmend:        set(PurposeHRMProfile, PurposeContactDirectory),
	OpSectionWrite: set(PurposeHRMProfile, PurposeHRMSensitive, PurposeContactDirectory),
	OpSectionRead:  set(PurposeHRMProfile, PurposeHRMSensitive, PurposeContactDirectory),
	// W11.23: thẻ danh bạ (pf/contact, FN-13) — CHỈ mục đích danh bạ.
	OpCard: set(PurposeContactDirectory),
	// Unwrap (mở DEK ảnh): đúng tập mục đích tệp của pf/sync (FilePurposes).
	OpUnwrap: set("mrv.evidence", "extension.field_support"),
	OpErase:  set("registry.erasure"),
}

func set(vs ...string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// AllowedPurpose cho biết mục đích purpose được phép cho thao tác op.
func AllowedPurpose(op, purpose string) bool { return opPurposes[op][purpose] }

// AllowedPurposes trả danh mục mục đích của op (sắp xếp) — để báo lỗi rõ ràng.
func AllowedPurposes(op string) []string {
	out := make([]string, 0, len(opPurposes[op]))
	for p := range opPurposes[op] {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ValidReason kiểm lý do xóa: 10–500 ký tự.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 10 && n <= 500
}

// Mục đích của hồ sơ nhân sự (W11.22).
const (
	PurposeHRMProfile   = "hrm.profile"   // hồ sơ nhân sự: định danh + ngăn hrm.profile
	PurposeHRMSensitive = "hrm.sensitive" // thẻ Thống kê BC Sở (TK-01..06) — chỉ ngăn hrm.sensitive
	// PurposeContactDirectory (W11.23, Q-071 — đề xuất của dev: mục đích riêng "danh bạ liên hệ", có thông báo chủ thể):
	// thẻ danh bạ của pf/contact và định danh + ngăn contact.card của liên hệ ngoài tỉnh.
	PurposeContactDirectory = "contact.directory"
)

// Ngăn hồ sơ (vault.section, vault 000004).
const (
	SectionHRMProfile   = "hrm.profile"
	SectionHRMSensitive = "hrm.sensitive"
	SectionContactCard  = "contact.card" // W11.23: email, năm sinh của liên hệ ngoài tỉnh
)

// sections: ngăn → mục đích DUY NHẤT được đọc/ghi + trường được phép (độ dài tối đa theo rune). Trường lạ bị từ chối
// để ngăn không thành chỗ chứa tùy ý.
var sections = map[string]struct {
	purpose string
	fields  map[string]int
}{
	SectionHRMProfile: {PurposeHRMProfile, map[string]int{
		"birth_date": 10, "birth_precision": 5, "gender": 6, "email": 254, "note": 1000,
	}},
	SectionHRMSensitive: {PurposeHRMSensitive, map[string]int{
		"ethnicity": 40, "religion": 40, "party_date": 10, "party_precision": 5, "hometown": 300, "birthplace": 300,
		"revolution_date": 10, "revolution_precision": 5,
	}},
	SectionContactCard: {PurposeContactDirectory, map[string]int{"email": 254, "birth_year": 4}},
}

// ErrSection: ngăn không có hoặc trường không thuộc ngăn / quá dài. Thông báo không chứa giá trị (R-14).
var ErrSection = errors.New("ngăn hoặc trường không hợp lệ")

// SectionPurpose trả mục đích duy nhất của ngăn; ok = false nếu không có ngăn.
func SectionPurpose(section string) (string, bool) {
	s, ok := sections[section]
	return s.purpose, ok
}

// ValidSectionFields kiểm tên trường và độ dài theo ngăn; lỗi là FieldError (tên trường, không có giá trị).
func ValidSectionFields(section string, fields map[string]string) error {
	s, ok := sections[section]
	if !ok {
		return FieldError{Field: "section", Err: ErrSection}
	}
	for k, v := range fields {
		limit, ok := s.fields[k]
		if !ok || utf8.RuneCountInString(v) > limit {
			return FieldError{Field: k, Err: ErrSection}
		}
	}
	return nil
}

// AAD gắn bản mã với đúng (tenant, chủ thể, mục dùng) để không đổi chỗ bản mã giữa các dòng.
func AAD(use string, tenant, ref uuid.UUID) []byte {
	return []byte("ag-vault/" + use + "/" + tenant.String() + "/" + ref.String())
}

// --- W11.23: thẻ danh bạ (OpCard). ---

// Trường của thẻ danh bạ. Kho chỉ trả trường được hỏi; KHÔNG BAO GIỜ trả CCCD, địa chỉ hay trường khác của ngăn hồ sơ
// (giới tính, ngày sinh đầy đủ, ghi chú, thẻ Thống kê BC Sở).
const (
	CardFullName  = "full_name"
	CardPhone     = "phone"
	CardBirthYear = "birth_year"
	CardEmail     = "email"
)

// MaxCardRefs là số chủ thể tối đa của một lần đọc thẻ.
const MaxCardRefs = 200

// ValidCardFields kiểm tập trường được hỏi (không rỗng, không lạ, không lặp).
func ValidCardFields(fields []string) bool {
	if len(fields) == 0 || len(fields) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range fields {
		switch f {
		case CardFullName, CardPhone, CardBirthYear, CardEmail:
		default:
			return false
		}
		if seen[f] {
			return false
		}
		seen[f] = true
	}
	return true
}

var yearRe = regexp.MustCompile(`^[0-9]{4}`)

// BirthYear trả năm của ngày sinh dạng YYYY, YYYY-MM hoặc YYYY-MM-DD (FN-13 BR-04: chỉ hiển thị năm); "" nếu không rõ.
func BirthYear(s string) string {
	return yearRe.FindString(strings.TrimSpace(s))
}
