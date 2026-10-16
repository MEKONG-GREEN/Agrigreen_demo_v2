// Package domain: quy tắc thuần của pf/contact (W11.23, FN-13) — bộ trường theo vai, kiểm dữ liệu vào, tìm kiếm không
// dấu. Không phụ thuộc hạ tầng, không gọi time.Now.
package domain

import (
	"errors"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
)

// RuleVersion của sổ pf_contact.
const RuleVersion = "contact.v1"

// Trường hiển thị của thẻ Cán bộ (FN-13 B3) — tên trên dây.
const (
	FieldFullName     = "full_name"
	FieldBirthYear    = "birth_year"
	FieldPhone        = "phone"
	FieldTitle        = "title"
	FieldUnit         = "unit"
	FieldLevel        = "level"
	FieldEmail        = "email"
	FieldOrganization = "organization" // liên hệ ngoài tỉnh: cơ quan (hiển thị ở cột Đơn vị công tác)
)

// Bộ trường theo vai.
type FieldSet string

// FieldSet.
const (
	// SetBasic: Cấp TW, Cấp Xã — chỉ họ tên, năm sinh, SĐT (BR-03: hệ thống không trả trường khác).
	SetBasic FieldSet = "basic"
	// SetFull: Cấp Tỉnh, Admin — thêm chức danh, đơn vị công tác, cấp, email.
	SetFull FieldSet = "full"
)

// Fields trả tên trường của bộ, theo thứ tự cột.
func (s FieldSet) Fields() []string {
	if s == SetFull {
		return []string{FieldFullName, FieldBirthYear, FieldPhone, FieldTitle, FieldUnit, FieldLevel, FieldEmail}
	}
	return []string{FieldFullName, FieldBirthYear, FieldPhone}
}

// Levels là các cấp hợp lệ.
var Levels = []string{port.LevelTW, port.LevelTinh, port.LevelXa, port.LevelKhac}

// LevelLabel trả nhãn tiếng Việt của cấp.
func LevelLabel(l string) string {
	switch l {
	case port.LevelTW:
		return "Trung ương"
	case port.LevelTinh:
		return "Tỉnh"
	case port.LevelXa:
		return "Xã"
	case port.LevelKhac:
		return "Đơn vị khác"
	}
	return l
}

// Lỗi kiểm dữ liệu — thông báo KHÔNG chứa giá trị người dùng nhập (R-14).
var (
	ErrRequired  = errors.New("bắt buộc")
	ErrTooLong   = errors.New("quá dài")
	ErrLevel     = errors.New("cấp phải là tw, tinh, xa hoặc khac")
	ErrEmail     = errors.New("email không hợp lệ")
	ErrBirthYear = errors.New("năm sinh 4 chữ số, từ 1900")
	ErrUnitPhone = errors.New("số máy cơ quan 8–11 chữ số")
)

// FieldError là lỗi theo trường.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

// Text chuẩn hóa chuỗi tự do: bỏ khoảng trắng thừa; "" → nil.
func Text(s string) *string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil
	}
	return &s
}

func checkLen(field string, s *string, max int) error {
	if s != nil && utf8.RuneCountInString(*s) > max {
		return FieldError{field, ErrTooLong}
	}
	return nil
}

// CheckLevel kiểm cấp.
func CheckLevel(l string) error {
	if !slices.Contains(Levels, l) {
		return FieldError{FieldLevel, ErrLevel}
	}
	return nil
}

// CheckEmail kiểm email (rỗng hợp lệ).
func CheckEmail(field, s string) error {
	if s == "" {
		return nil
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || utf8.RuneCountInString(s) > 254 {
		return FieldError{field, ErrEmail}
	}
	return nil
}

var yearRe = regexp.MustCompile(`^(19|20)[0-9]{2}$`)

// CheckBirthYear kiểm năm sinh (rỗng hợp lệ).
func CheckBirthYear(s string) error {
	if s != "" && !yearRe.MatchString(s) {
		return FieldError{FieldBirthYear, ErrBirthYear}
	}
	return nil
}

// PersonMeta là phần KHÔNG PII của liên hệ ngoài tỉnh (lưu ở pf_contact.person).
type PersonMeta struct {
	Level        string
	Organization *string
	Title        *string
}

// Check kiểm phần không PII.
func (m PersonMeta) Check() error {
	if err := CheckLevel(m.Level); err != nil {
		return err
	}
	if err := checkLen(FieldOrganization, m.Organization, 200); err != nil {
		return err
	}
	return checkLen(FieldTitle, m.Title, 200)
}

// Changed trả TÊN trường không PII khác nhau giữa a và b.
func (m PersonMeta) Changed(b PersonMeta) []string {
	var out []string
	if m.Level != b.Level {
		out = append(out, FieldLevel)
	}
	if !eqp(m.Organization, b.Organization) {
		out = append(out, FieldOrganization)
	}
	if !eqp(m.Title, b.Title) {
		out = append(out, FieldTitle)
	}
	return out
}

func eqp(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// UnitInput là thẻ Đơn vị (liên hệ của TỔ CHỨC — không phải dữ liệu cá nhân).
type UnitInput struct {
	Name    string
	Level   string
	Phone   *string
	Email   *string
	Address *string
}

var digitsRe = regexp.MustCompile(`^[0-9]{8,11}$`)

// Normalize chuẩn hóa và kiểm thẻ Đơn vị: SĐT bỏ khoảng trắng, dấu chấm, gạch; "+84" → "0".
func (u UnitInput) Normalize() (UnitInput, error) {
	out := UnitInput{Level: u.Level, Email: Text(deref(u.Email)), Address: Text(deref(u.Address))}
	if n := Text(u.Name); n != nil {
		out.Name = *n
	}
	if out.Name == "" {
		return out, FieldError{"name", ErrRequired}
	}
	if utf8.RuneCountInString(out.Name) > 200 {
		return out, FieldError{"name", ErrTooLong}
	}
	if err := CheckLevel(out.Level); err != nil {
		return out, err
	}
	if p := deref(u.Phone); strings.TrimSpace(p) != "" {
		d := strings.NewReplacer(" ", "", ".", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(p))
		if strings.HasPrefix(d, "+84") {
			d = "0" + d[3:]
		}
		if !digitsRe.MatchString(d) {
			return out, FieldError{FieldPhone, ErrUnitPhone}
		}
		out.Phone = &d
	}
	if out.Email != nil {
		if err := CheckEmail(FieldEmail, *out.Email); err != nil {
			return out, err
		}
	}
	return out, checkLen("address", out.Address, 300)
}

// Changed trả TÊN trường khác nhau giữa u và b.
func (u UnitInput) Changed(b UnitInput) []string {
	var out []string
	if u.Name != b.Name {
		out = append(out, "name")
	}
	if u.Level != b.Level {
		out = append(out, FieldLevel)
	}
	if !eqp(u.Phone, b.Phone) {
		out = append(out, FieldPhone)
	}
	if !eqp(u.Email, b.Email) {
		out = append(out, FieldEmail)
	}
	if !eqp(u.Address, b.Address) {
		out = append(out, "address")
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Fold chuẩn hóa chuỗi để tìm: chữ thường, bỏ dấu tiếng Việt (đ → d), gộp khoảng trắng.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ':
			b.WriteRune('d')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Digits giữ lại chữ số; "+84…"/"84…" (10–11 số sau 84) → "0…" để so với số trong nước.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "84") && len(d) >= 11 {
		d = "0" + d[2:]
	}
	return d
}

// LocalPhone hiển thị SĐT dạng trong nước (kho lưu dạng 84…).
func LocalPhone(p string) string {
	if strings.HasPrefix(p, "84") && len(p) >= 11 {
		return "0" + p[2:]
	}
	return p
}

// Match cho biết từ khóa q khớp liên hệ: họ tên (không dấu, chứa) hoặc SĐT (chuỗi số ≥ 3, chứa). extra là các trường chỉ
// bộ SetFull được tìm (đơn vị, email, chức danh — FN-13 B2) — bộ SetBasic truyền nil để không suy ra trường bị ẩn.
func Match(q, fullName, phone string, extra ...string) bool {
	q = strings.TrimSpace(q)
	if q == "" {
		return true
	}
	if m := strings.Map(keepDigitsAndSeps, q); len(m) >= 3 && !strings.ContainsRune(m, 'x') {
		return strings.Contains(Digits(LocalPhone(phone)), Digits(q))
	}
	f := Fold(q)
	if strings.Contains(Fold(fullName), f) {
		return true
	}
	for _, x := range extra {
		if x != "" && strings.Contains(Fold(x), f) {
			return true
		}
	}
	return false
}

// keepDigitsAndSeps giữ chữ số; bỏ dấu cách, chấm, gạch, cộng — để nhận ra từ khóa là SĐT.
func keepDigitsAndSeps(r rune) rune {
	switch {
	case r >= '0' && r <= '9':
		return r
	case r == ' ' || r == '.' || r == '-' || r == '+':
		return -1
	}
	return 'x'
}
