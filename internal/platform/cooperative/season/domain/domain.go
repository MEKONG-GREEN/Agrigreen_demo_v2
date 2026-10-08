// Package domain: quy tắc nghiệp vụ của pf/cropcycle — SOP, vụ mùa, nhật ký canh
// tác, GPS, sản lượng. Không phụ thuộc hạ tầng; không đọc đồng hồ (R-12): thời
// điểm "hôm nay" luôn do app truyền vào.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc ghi vào cột rule_version của các sổ.
const RuleVersion = "cropcycle/v1"

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

// Hoạt động canh tác (HTX US-LOG-01).
const (
	ActLamDat    = "lam_dat"
	ActGieoSa    = "gieo_sa"
	ActTuoiNuoc  = "tuoi_nuoc"
	ActBonPhan   = "bon_phan"
	ActPhunThuoc = "phun_thuoc"
	ActKiemTra   = "kiem_tra"
	ActThuHoach  = "thu_hoach"
)

var activities = map[string]string{
	ActLamDat: "Làm đất", ActGieoSa: "Gieo sạ", ActTuoiNuoc: "Tưới nước", ActBonPhan: "Bón phân",
	ActPhunThuoc: "Phun thuốc", ActKiemTra: "Kiểm tra", ActThuHoach: "Thu hoạch",
}

// KnownActivity cho biết mã hoạt động có trong danh mục.
func KnownActivity(code string) bool { _, ok := activities[code]; return ok }

// ActivityLabel trả nhãn tiếng Việt của hoạt động.
func ActivityLabel(code string) string { return activities[code] }

// Loại bằng chứng của bước SOP (nguyên mẫu production.ts EVIDENCE_KINDS).
var evidenceKinds = map[string]bool{
	"anh_hien_truong": true, "hoa_don_vat_tu": true, "phieu_kiem_nghiem": true, "ghi_chu": true, "khac": true,
}

// KnownEvidenceKind cho biết loại bằng chứng hợp lệ.
func KnownEvidenceKind(k string) bool { return evidenceKinds[k] }

var (
	codeRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	sopCodeRe  = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_.-]{1,39}$`)
	catCodeRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	seasonRe   = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
	spaceRe    = regexp.MustCompile(`\s+`)
	phoneRe    = regexp.MustCompile(`(?:\+?84|\b0)[\s.-]?(?:\d[\s.-]?){8,9}\d\b`)
	nationalRe = regexp.MustCompile(`\b\d{12}\b`)
)

// ValidSOPCode kiểm mã mẫu SOP (chữ hoa, số, _ . -).
func ValidSOPCode(s string) bool { return sopCodeRe.MatchString(s) }

// ValidCode kiểm mã dạng snake_case (cây trồng, chuẩn, giai đoạn).
func ValidCode(s string) bool { return codeRe.MatchString(s) }

// ValidCatalogCode kiểm mã mục danh mục masterdata (giống lúa, máy).
func ValidCatalogCode(s string) bool { return catCodeRe.MatchString(s) }

// ValidSeasonCode kiểm mã vụ (DX, HT, TD...).
func ValidSeasonCode(s string) bool { return seasonRe.MatchString(s) }

// NameKey chuẩn hóa tên để chặn trùng: cắt, gộp khoảng trắng, chữ thường
// (HTX US-CAT-01 AC-6, US-CAT-03 AC-3: "Tên đã tồn tại").
func NameKey(name string) string {
	return strings.ToLower(spaceRe.ReplaceAllString(strings.TrimSpace(name), " "))
}

// ValidReason: lý do bản ghi bù ≥ 10 ký tự (khớp CHECK của sổ).
func ValidReason(s string) bool { return len([]rune(strings.TrimSpace(s))) >= 10 }

// ContainsPII phát hiện SĐT Việt Nam hoặc số CCCD 12 chữ số trong văn bản tự do
// (ghi chú nhật ký không được chứa dữ liệu cá nhân — R-04, R-14).
func ContainsPII(s string) bool { return phoneRe.MatchString(s) || nationalRe.MatchString(s) }

// CleanText cắt khoảng trắng và kiểm độ dài/ký tự điều khiển của văn bản tự do.
func CleanText(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > max {
		return "", FieldError{field, "tối đa " + itoa(max) + " ký tự"}
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", FieldError{field, "chứa ký tự điều khiển"}
		}
	}
	return s, nil
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

// ---------------------------------------------------------------------------
// Ngày (theo giờ Việt Nam, biểu diễn bằng time.Time 00:00 UTC)
// ---------------------------------------------------------------------------

// Day chuẩn hóa về 00:00 UTC của ngày dương lịch (bỏ giờ).
func Day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ParseDay đọc YYYY-MM-DD.
func ParseDay(s string) (time.Time, error) { return time.Parse(time.DateOnly, strings.TrimSpace(s)) }

// DaysBetween trả số ngày từ a tới b (b − a).
func DaysBetween(a, b time.Time) int { return int(b.Sub(a).Hours() / 24) }

// AddDays cộng ngày.
func AddDays(d time.Time, n int) time.Time { return d.AddDate(0, 0, n) }

// FormatDay trả YYYY-MM-DD.
func FormatDay(d time.Time) string { return d.Format(time.DateOnly) }

// Cửa sổ ngày gieo sạ (HTX US-SEASON-01 AC-2).
const (
	SowingPastDays   = 15
	SowingFutureDays = 30
)

// MsgSowingWindow là thông báo của HTX US-SEASON-01 AC-2.
const MsgSowingWindow = "Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại."

// ErrSowingWindow: ngày gieo sạ ngoài khoảng cho phép.
var ErrSowingWindow = errors.New("cropcycle: ngày gieo sạ ngoài cửa sổ cho phép")

// CheckSowingDate kiểm ngày gieo sạ trong [today − 15, today + 30] (ngày nghiệp vụ VN).
func CheckSowingDate(sowing, today time.Time) error {
	d := DaysBetween(today, sowing)
	if d < -SowingPastDays || d > SowingFutureDays {
		return ErrSowingWindow
	}
	return nil
}
