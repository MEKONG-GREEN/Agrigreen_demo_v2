// Package domain: quy tắc thuần của danh sách hộ chăn nuôi CNTS — CNTS-V4-HOUSEHOLD-LIST (#251), ADR-0056, BRD CNTS v4.0
// FN-10 (bảng trường HCN-01…HCN-09, BR-03…BR-08). Không phụ thuộc hạ tầng, không gọi time.Now.
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RuleVersion của sổ pf_extension.cnts_livestock_household.
const RuleVersion = "kn.cnts.livestock_household/v1"

// Sự việc của một phiên bản (HCN-09).
const (
	EventCreated        = "created"         // thêm từ danh sách, kèm check-in (H1–H3)
	EventCreatedPending = "created_pending" // hộ nhập mới tại BM-10 DB-04 (BR-08 — #252)
	EventUpdated        = "updated"         // sửa thông tin (H4)
	EventCheckedIn      = "checked_in"      // Chờ bổ sung vị trí → Đang dùng (BR-08)
	EventRelocated      = "relocated"       // đổi vị trí: check-in lại, có lý do (H4)
	EventDeactivated    = "deactivated"     // vô hiệu hóa (BR-04)
)

// Trạng thái HCN-08.
const (
	StatusActive      = "active"           // Đang dùng
	StatusPending     = "pending_location" // Chờ bổ sung vị trí
	StatusDeactivated = "deactivated"      // Vô hiệu hóa
)

// Loại chăn nuôi HCN-06.
const (
	TypeHousehold  = "nong_ho"
	TypeFarmSmall  = "trang_trai_nho"
	TypeFarmMedium = "trang_trai_vua"
	TypeFarmLarge  = "trang_trai_cong_nghiep"
)

// TypeLabels là nhãn HCN-06.
var TypeLabels = map[string]string{TypeHousehold: "Nông hộ", TypeFarmSmall: "Trang trại nhỏ", TypeFarmMedium: "Trang trại vừa",
	TypeFarmLarge: "Trang trại công nghiệp"}

// StatusLabels là nhãn HCN-08.
var StatusLabels = map[string]string{StatusActive: "Đang dùng", StatusPending: "Chờ bổ sung vị trí", StatusDeactivated: "Vô hiệu hóa"}

// LivestockTypes theo thứ tự hiển thị.
var LivestockTypes = []string{TypeHousehold, TypeFarmSmall, TypeFarmMedium, TypeFarmLarge}

// Tên trường trong changed_fields (HCN-09).
const (
	FieldOwnerName = "owner_name"     // HCN-02
	FieldPhone     = "owner_phone"    // HCN-03
	FieldCommune   = "commune_code"   // HCN-04 xã
	FieldHamlet    = "hamlet"         // HCN-04 ấp
	FieldAnimals   = "animals"        // HCN-05
	FieldType      = "livestock_type" // HCN-06
	FieldLocation  = "location"       // HCN-07
	FieldStatus    = "status"         // HCN-08
)

// Giới hạn.
const (
	MaxName    = 200 // HCN-02
	MaxHamlet  = 200
	MaxAnimals = 30
	MaxHeads   = 1_000_000_000
	MaxReason  = 1000
	MinReason  = 5
)

// FieldError là lỗi theo trường; Errors là danh sách lỗi.
type FieldError struct{ Field, Reason string }

// Errors gom lỗi.
type Errors []FieldError

func (e *Errors) add(field, reason string) { *e = append(*e, FieldError{field, reason}) }

// Animal là một dòng vật nuôi, số con (HCN-05).
type Animal struct {
	Species string
	Heads   int64
}

// Fields là các trường không phải dữ liệu cá nhân của hộ (HCN-04…HCN-06).
type Fields struct {
	CommuneCode   string
	Hamlet        string
	LivestockType string
	Animals       []Animal
}

var unitRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,20}$`)

// ValidType cho biết t là loại chăn nuôi HCN-06.
func ValidType(t string) bool {
	for _, x := range LivestockTypes {
		if x == t {
			return true
		}
	}
	return false
}

// Normalize gộp khoảng trắng của ấp và kiểm các trường.
func (f *Fields) Normalize() Errors {
	var e Errors
	f.CommuneCode = strings.TrimSpace(f.CommuneCode)
	f.Hamlet = strings.Join(strings.Fields(f.Hamlet), " ")
	f.LivestockType = strings.TrimSpace(f.LivestockType)
	if !unitRe.MatchString(f.CommuneCode) {
		e.add(FieldCommune, "chọn xã")
	}
	if utf8.RuneCountInString(f.Hamlet) > MaxHamlet {
		e.add(FieldHamlet, fmt.Sprintf("tối đa %d ký tự", MaxHamlet))
	}
	if !ValidType(f.LivestockType) {
		e.add(FieldType, "nông hộ, trang trại nhỏ, vừa hoặc công nghiệp")
	}
	switch n := len(f.Animals); {
	case n == 0:
		e.add(FieldAnimals, "nhập ít nhất một vật nuôi")
	case n > MaxAnimals:
		e.add(FieldAnimals, fmt.Sprintf("tối đa %d dòng", MaxAnimals))
	}
	seen := map[string]bool{}
	for i := range f.Animals {
		a := &f.Animals[i]
		a.Species = strings.TrimSpace(a.Species)
		field := fmt.Sprintf("%s[%d]", FieldAnimals, i)
		switch {
		case a.Species == "" || utf8.RuneCountInString(a.Species) > 64:
			e.add(field+".species_code", "chọn vật nuôi")
		case seen[a.Species]:
			e.add(field+".species_code", "vật nuôi trùng dòng khác")
		}
		seen[a.Species] = true
		if a.Heads < 0 || a.Heads > MaxHeads {
			e.add(field+".head_count", "số nguyên ≥ 0")
		}
	}
	return e
}

// NormalizeOwnerName gộp khoảng trắng của họ tên chủ hộ.
func NormalizeOwnerName(s string) string { return strings.Join(strings.Fields(s), " ") }

// ValidOwnerName: HCN-02 bắt buộc, tối đa 200 ký tự (đã chuẩn hóa).
func ValidOwnerName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= MaxName
}

var digitsRe = regexp.MustCompile(`^[0-9]{1,15}$`)

// ValidPhoneDigits: HCN-03 "Số" — chỉ chữ số (kho định danh kiểm tiếp dạng SĐT Việt Nam).
func ValidPhoneDigits(s string) bool { return digitsRe.MatchString(s) }

// SameName so họ tên đã chuẩn hóa, không phân biệt hoa thường (cảnh báo trùng — luồng ngoại lệ FN-10).
func SameName(a, b string) bool {
	return strings.EqualFold(NormalizeOwnerName(a), NormalizeOwnerName(b))
}

// SameHamlet so ấp đã chuẩn hóa, không phân biệt hoa thường.
func SameHamlet(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// ValidReason: lý do đổi vị trí / vô hiệu hóa 5–1000 ký tự.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= MinReason && n <= MaxReason
}

// Code trả mã hộ HCN-01 từ số đếm: HCN + 7 chữ số.
func Code(n int64) string { return fmt.Sprintf("HCN%07d", n) }

// MaxCode là số đếm lớn nhất (HCN9999999).
const MaxCode = 9_999_999

// Checkin là một lần check-in tại nhà hộ (HCN-07).
type Checkin struct {
	Lat, Lon   float64
	AccuracyM  decimal.Decimal
	Mocked     bool
	CapturedAt time.Time
}

var accuracyRe = regexp.MustCompile(`^[0-9]{1,8}(\.[0-9]{1,2})?$`)

// ParseAccuracy kiểm cú pháp thập phân thường (≤ 8 chữ số nguyên, ≤ 2 chữ số lẻ) TRƯỚC khi parse.
func ParseAccuracy(s string) (decimal.Decimal, bool) {
	if !accuracyRe.MatchString(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	return d, err == nil
}

// ValidCheckin kiểm tọa độ (lãnh thổ Việt Nam mở rộng) và thời điểm bắt GPS (không quá now + 5 phút).
func ValidCheckin(c Checkin, now time.Time) Errors {
	var e Errors
	if c.Lat < 5 || c.Lat > 25 || c.Lon < 100 || c.Lon > 118 {
		e.add("checkin", "tọa độ GPS ngoài phạm vi Việt Nam")
	}
	if c.CapturedAt.IsZero() || c.CapturedAt.After(now.Add(5*time.Minute)) {
		e.add("checkin.captured_at", "thời điểm bắt GPS không hợp lệ")
	}
	return e
}

// AnimalsEqual so hai danh sách vật nuôi (cùng thứ tự).
func AnimalsEqual(a, b []Animal) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Changed trả tên trường không phải dữ liệu cá nhân đổi từ prev sang next.
func Changed(prev, next Fields) []string {
	var out []string
	if prev.CommuneCode != next.CommuneCode {
		out = append(out, FieldCommune)
	}
	if prev.Hamlet != next.Hamlet {
		out = append(out, FieldHamlet)
	}
	if !AnimalsEqual(prev.Animals, next.Animals) {
		out = append(out, FieldAnimals)
	}
	if prev.LivestockType != next.LivestockType {
		out = append(out, FieldType)
	}
	return out
}
