// Package domain: quy tắc hồ sơ KN riêng cho HTX, Nông hộ, Tổ hợp tác (KNSPLIT-2-KN-RECORDS #67; TT-08, TT-09). Hồ sơ
// thuộc KN (schema pf_extension), KHÔNG phải HTX nền tảng: KN không tạo/sửa HTX, không ghi bảng HTX. Họ tên, SĐT người
// đại diện/chủ hộ ở kho định danh — sổ chỉ giữ person_ref.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleVersion ghim vào mọi dòng sổ kn_producer.
const RuleVersion = "extension.kn_producer.v1"

// Loại hồ sơ (TT-08 "Loại", TT-09 ba danh sách).
const (
	KindCooperative = "cooperative" // HTX
	KindHousehold   = "household"   // Nông hộ
	KindCoopGroup   = "coop_group"  // Tổ hợp tác
)

// ValidKind: một trong ba loại.
func ValidKind(k string) bool {
	return k == KindCooperative || k == KindHousehold || k == KindCoopGroup
}

// KindLabel là nhãn tiếng Việt của loại.
func KindLabel(k string) string {
	switch k {
	case KindCooperative:
		return "HTX"
	case KindHousehold:
		return "Nông hộ"
	case KindCoopGroup:
		return "Tổ hợp tác"
	}
	return k
}

// Sự việc trên sổ.
const (
	EventCreated     = "created"
	EventUpdated     = "updated"
	EventDeactivated = "deactivated"
	EventReactivated = "reactivated"
)

// Trạng thái hiện hành.
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// StatusOf: hồ sơ ngừng hoạt động khi sự việc mới nhất là deactivated.
func StatusOf(event string) string {
	if event == EventDeactivated {
		return StatusInactive
	}
	return StatusActive
}

// Giới hạn độ dài (khớp CHECK của migration; kiểm trước khi gọi kho định danh/DB).
const (
	MaxName       = 200
	MaxPersonName = 120
	MaxPhoneInput = 20
	MaxAddress    = 300
	MinReason     = 10
	MaxReason     = 1000
	MaxAreaInput  = 16
)

// MaxArea là diện tích lớn nhất nhận (ha) — khớp CHECK area_ha ≤ 1.000.000.
var MaxArea = decimal.NewFromInt(1_000_000)

// FieldError là lỗi của một trường.
type FieldError struct{ Field, Reason string }

// Errors là danh sách lỗi theo trường.
type Errors []FieldError

func (es *Errors) add(field, reason string) {
	*es = append(*es, FieldError{Field: field, Reason: reason})
}

// Person là người đại diện/chủ hộ gửi lên (chỉ đi vào kho định danh, không vào sổ).
type Person struct {
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
}

var phoneSep = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// NormalizePhone đưa SĐT Việt Nam về 84xxxxxxxxx như kho định danh; ok=false nếu không hợp lệ. Độ dài đầu vào kiểm
// trước (chuỗi rất dài bị từ chối ngay).
func NormalizePhone(s string) (string, bool) {
	if len(s) > MaxPhoneInput*2 {
		return "", false
	}
	p := phoneSep.Replace(strings.TrimSpace(s))
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
		return "", false
	}
	if len(p) != 9 || p[0] == '0' {
		return "", false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return "84" + p, true
}

// Validate kiểm người đại diện/chủ hộ (prefix = tên trường gốc, vd "representative").
func (p *Person) Validate(prefix string) Errors {
	var es Errors
	p.FullName = strings.TrimSpace(p.FullName)
	switch n := utf8.RuneCountInString(p.FullName); {
	case n == 0:
		es.add(prefix+".full_name", "bắt buộc")
	case n > MaxPersonName:
		es.add(prefix+".full_name", "tối đa 120 ký tự")
	case strings.ContainsFunc(p.FullName, unicode.IsDigit):
		es.add(prefix+".full_name", "họ tên không chứa chữ số")
	}
	if strings.TrimSpace(p.Phone) == "" {
		es.add(prefix+".phone", "bắt buộc")
	} else if _, ok := NormalizePhone(p.Phone); !ok {
		es.add(prefix+".phone", "số điện thoại Việt Nam không hợp lệ (10 chữ số, bắt đầu bằng 0)")
	}
	return es
}

var (
	areaRe       = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,4})?$`)
	taxRe        = regexp.MustCompile(`^[0-9]{10}(-[0-9]{3})?$`)
	communeRe    = regexp.MustCompile(`^[0-9A-Za-z_.-]{1,20}$`)
	phoneTextRe  = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalIDRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ErrArea là lỗi kiểm diện tích.
var ErrArea = errors.New("số thập phân ≥ 0, tối đa 4 chữ số lẻ, không quá 1.000.000 ha")

// ParseArea đọc diện tích (ha): chuỗi thập phân thường (không số mũ, không dấu), kiểm cú pháp và độ dài TRƯỚC khi tạo
// decimal (bài học #107/#109/#29: 1e100000, chuỗi rất dài). Rỗng → nil.
func ParseArea(raw string) (*decimal.Decimal, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	if len(s) > MaxAreaInput || !areaRe.MatchString(s) {
		return nil, ErrArea
	}
	d, err := decimal.NewFromString(s)
	if err != nil || d.IsNegative() || d.GreaterThan(MaxArea) {
		return nil, ErrArea
	}
	return &d, nil
}

// ValidCommune: mã xã/phường 1–20 ký tự chữ/số.
func ValidCommune(s string) bool { return communeRe.MatchString(s) }

// ContainsPII: SĐT/CCCD trong văn bản tự do (phải vào kho định danh).
func ContainsPII(s string) bool { return phoneTextRe.MatchString(s) || nationalIDRe.MatchString(s) }

// Record là phần hồ sơ người dùng nhập (trừ người đại diện).
type Record struct {
	Kind        string
	CommuneCode string
	Name        string // HTX, THT; rỗng với Nông hộ
	AreaHa      *decimal.Decimal
	TaxCode     string // chỉ HTX
	Address     string // HTX, THT
}

// Normalize cắt khoảng trắng và kiểm theo loại (TT-08: Xã/phường, Loại, Tên bắt buộc).
func (r *Record) Normalize() Errors {
	var es Errors
	r.CommuneCode = strings.TrimSpace(r.CommuneCode)
	r.Name = strings.TrimSpace(r.Name)
	r.TaxCode = strings.TrimSpace(r.TaxCode)
	r.Address = strings.TrimSpace(r.Address)
	if !ValidKind(r.Kind) {
		es.add("kind", "bắt buộc: cooperative | household | coop_group")
		return es
	}
	switch {
	case r.CommuneCode == "":
		es.add("commune_code", "bắt buộc")
	case !ValidCommune(r.CommuneCode):
		es.add("commune_code", "mã xã/phường không hợp lệ")
	}
	if r.Kind == KindHousehold {
		if r.Name != "" {
			es.add("name", "Nông hộ: tên là họ tên chủ hộ — nhập ở chủ hộ (lưu kho định danh)")
		}
		if r.TaxCode != "" {
			es.add("tax_code", "chỉ dùng cho HTX")
		}
		if r.Address != "" {
			es.add("address", "Nông hộ: địa chỉ là dữ liệu cá nhân — không nhập ở đây")
		}
		return es
	}
	switch n := utf8.RuneCountInString(r.Name); {
	case n == 0:
		es.add("name", "bắt buộc")
	case n > MaxName:
		es.add("name", "tối đa 200 ký tự")
	case ContainsPII(r.Name):
		es.add("name", "không ghi số điện thoại/CCCD trong tên")
	}
	if r.TaxCode != "" {
		if r.Kind != KindCooperative {
			es.add("tax_code", "chỉ dùng cho HTX")
		} else if !taxRe.MatchString(r.TaxCode) {
			es.add("tax_code", "mã số thuế 10 chữ số (hoặc 10 chữ số-3 chữ số)")
		}
	}
	if r.Address != "" {
		if utf8.RuneCountInString(r.Address) > MaxAddress {
			es.add("address", "tối đa 300 ký tự")
		} else if ContainsPII(r.Address) {
			es.add("address", "không ghi số điện thoại/CCCD trong địa chỉ")
		}
	}
	return es
}

// NameKey là khóa so trùng tên (không phân biệt hoa thường, gộp khoảng trắng).
func NameKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// ValidReason: lý do vô hiệu hóa/kích hoạt lại 10–1000 ký tự, không chứa SĐT/CCCD.
func ValidReason(s string) (string, string) {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n < MinReason:
		return s, "bắt buộc, ít nhất 10 ký tự"
	case n > MaxReason:
		return s, "tối đa 1000 ký tự"
	case ContainsPII(s):
		return s, "không ghi số điện thoại/CCCD trong lý do"
	}
	return s, ""
}

// ParentAllowed: Nông hộ chỉ thuộc HTX hoặc Tổ hợp tác.
func ParentAllowed(kind string) bool { return kind == KindCooperative || kind == KindCoopGroup }
