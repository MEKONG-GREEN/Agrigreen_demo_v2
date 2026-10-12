// Package domain là quy tắc thuần của pf/hrm (`dom/hrm`, ADR-0050) — W11.21 danh mục nhân sự (BRD HCTH v3.0 FN-19):
// loại danh mục, mã (chuẩn hóa mã ngạch — AC-03), thuộc tính riêng từng loại, bảng hệ số có hiệu lực, tham số nâng bậc
// và quy đổi thành tích có phiên bản. Không I/O, không đồng hồ (R-12: ngày truyền vào). Không float (R-CODE-01,
// agnofloat): hệ số, phần trăm là decimal.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// RuleVersion ghi vào mọi dòng sổ của module (R-13). Đổi khi đổi quy tắc ghi.
const RuleVersion = "hrm.v1"

// Kind là loại danh mục nhân sự (cột kind của pf_hrm.catalog_item).
type Kind string

// Danh mục FN-19. Bốn danh mục Admin cấu hình (menu Master data) và các danh sách khai báo sẵn khi triển khai.
const (
	KindPosition        Kind = "position"         // Chức vụ (cờ lãnh đạo, hệ số PCCV — HS-17, HS-19)
	KindGrade           Kind = "grade"            // Ngạch/CDNN (nhóm tương đương NS06 — HS-23); bảng hệ số ở GradeScale
	KindEducationLevel  Kind = "education_level"  // Trình độ chuyên môn (HS-24, HS-25; NS05, NS06)
	KindMajorGroup      Kind = "major_group"      // Nhóm chuyên ngành theo trình độ (NS05, FN-19 BR-03)
	KindMajor           Kind = "major"            // Chuyên ngành (gắn nhóm theo từng trình độ)
	KindEthnicity       Kind = "ethnicity"        // Dân tộc (TK-01; khác Kinh = DTTS)
	KindReligion        Kind = "religion"         // Tôn giáo (TK-02, có "Không")
	KindPoliticalTheory Kind = "political_theory" // Lý luận chính trị (HS-26)
	KindStateMgmt       Kind = "state_mgmt"       // Quản lý nhà nước (HS-27)
	KindForeignLanguage Kind = "foreign_language" // Ngoại ngữ — ngôn ngữ (HS-28)
	KindLanguageLevel   Kind = "language_level"   // Trình độ ngoại ngữ (HS-28)
	KindITLevel         Kind = "it_level"         // Tin học (HS-29)
	KindAwardForm       Kind = "award_form"       // Hình thức khen thưởng (HS-36; nguồn quy đổi thành tích)
	KindDisciplineForm  Kind = "discipline_form"  // Hình thức kỷ luật
	KindChangeType      Kind = "change_type"      // Loại biến động
	KindLeaveReason     Kind = "leave_reason"     // Lý do nghỉ việc (FN-08)
)

// Kinds là mọi loại theo thứ tự hiển thị (bốn danh mục chính trước).
var Kinds = []Kind{KindPosition, KindGrade, KindEducationLevel, KindMajor, KindMajorGroup, KindEthnicity, KindReligion,
	KindPoliticalTheory, KindStateMgmt, KindForeignLanguage, KindLanguageLevel, KindITLevel, KindAwardForm,
	KindDisciplineForm, KindChangeType, KindLeaveReason}

// ParseKind kiểm loại danh mục.
func ParseKind(s string) (Kind, bool) {
	for _, k := range Kinds {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Nhóm tương đương của ngạch/CDNN (FN-19 B3, NS06 cột 8–12).
const (
	EquivCVCC   = "CVCC"
	EquivCVC    = "CVC"
	EquivCV     = "CV"
	EquivCS     = "CS"
	EquivConLai = "CON_LAI"
)

// EquivGroups là năm nhóm tương đương theo thứ tự cột NS06.
var EquivGroups = []string{EquivCVCC, EquivCVC, EquivCV, EquivCS, EquivConLai}

// Loại ngạch: ngạch công chức hay chức danh nghề nghiệp viên chức (Ngạch / CDNN).
const (
	GradeTypeCongChuc = "cong_chuc"
	GradeTypeVienChuc = "vien_chuc"
)

var (
	reCode      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	reGradeCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
	reMgmtLevel = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// ErrInvalid: dữ liệu danh mục sai (app đổi sang problem.Validation).
var ErrInvalid = errors.New("hrm: danh mục không hợp lệ")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// NormalizeCode chuẩn hóa mã: bỏ mọi khoảng trắng, xuống dòng (FN-19 luồng ngoại lệ, AC-03: " V.03.04.12 " →
// "V.03.04.12"). Mã ngạch giữ chữ hoa (V.05.02.07); mã danh mục khác đưa về chữ thường.
func NormalizeCode(k Kind, s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	if k != KindGrade {
		s = strings.ToLower(s)
	}
	return s
}

// ValidCode kiểm mã đã chuẩn hóa của loại k.
func ValidCode(k Kind, code string) bool {
	if k == KindGrade {
		return reGradeCode.MatchString(code)
	}
	return reCode.MatchString(code)
}

// Item là một phiên bản mục danh mục cần ghi (đã chuẩn hóa mã).
type Item struct {
	Kind      Kind
	Code      string
	Name      string
	SortOrder int
	Active    bool
	Attrs     Attrs
}

// Attrs là thuộc tính riêng theo loại (cột attrs). Trường không thuộc loại phải rỗng. Số là decimal (chuỗi ở JSON).
type Attrs struct {
	// Chức vụ.
	Leader    bool             `json:"leader,omitempty"`     // chức vụ lãnh đạo, quản lý (FN-16, FN-17: mẫu MS06)
	Allowance *decimal.Decimal `json:"allowance,omitempty"`  // hệ số phụ cấp chức vụ gợi ý (HS-19), 2 chữ số thập phân
	MgmtLevel string           `json:"mgmt_level,omitempty"` // cấp quản lý tương ứng của tài khoản (pf/accounts MgmtLevels), tùy chọn
	// Ngạch/CDNN.
	EquivGroup  string `json:"equiv_group,omitempty"`  // CVCC | CVC | CV | CS | CON_LAI (NS06)
	GradeType   string `json:"grade_type,omitempty"`   // cong_chuc | vien_chuc
	SalaryClass string `json:"salary_class,omitempty"` // loại/nhóm bảng lương (A2.1, A1, B …) — thông tin
	// Trình độ chuyên môn: thứ hạng (lớn hơn = cao hơn) để chọn "trình độ cao nhất" (HS-25, NS05).
	Rank int `json:"rank,omitempty"`
	// Nhóm chuyên ngành: trình độ của nhóm (NS05 chia nhóm theo từng trình độ).
	Level string `json:"level,omitempty"`
	// Chuyên ngành: nhóm chuyên ngành theo từng trình độ (mã trình độ → mã nhóm) — FN-19 B4, BR-03.
	Groups map[string]string `json:"groups,omitempty"`
	// Dân tộc: thiểu số (mặc định mọi dân tộc khác Kinh — TK-01).
	Minority bool `json:"minority,omitempty"`
	// Tôn giáo: mục "Không" (không tính vào cột Tôn giáo của NS06).
	NoReligion bool `json:"no_religion,omitempty"`
}

// Validate kiểm một phiên bản mục danh mục (không kiểm tham chiếu chéo — app kiểm trong giao dịch).
func Validate(it Item) error {
	if _, ok := ParseKind(string(it.Kind)); !ok {
		return invalid("loại %q", it.Kind)
	}
	if !ValidCode(it.Kind, it.Code) {
		if it.Kind == KindGrade {
			return invalid("mã ngạch %q: chữ, số, dấu chấm, gạch; tối đa 40 ký tự", it.Code)
		}
		return invalid("mã %q: chữ thường không dấu, số, gạch dưới; bắt đầu bằng chữ; tối đa 40 ký tự", it.Code)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(it.Name)); n < 1 || n > 200 || n != utf8.RuneCountInString(it.Name) {
		return invalid("tên dài 1–200 ký tự, không khoảng trắng hai đầu")
	}
	if it.SortOrder < 0 || it.SortOrder > 100000 {
		return invalid("thứ tự %d", it.SortOrder)
	}
	return validateAttrs(it.Kind, it.Attrs)
}

func validateAttrs(k Kind, a Attrs) error {
	keep := Attrs{} // chỉ các trường của loại k
	switch k {
	case KindPosition:
		if a.Allowance != nil {
			if err := checkCoef("hệ số phụ cấp chức vụ", *a.Allowance, decimal.Zero, decimal.NewFromInt(10)); err != nil {
				return err
			}
			if !a.Leader {
				return invalid("hệ số phụ cấp chức vụ chỉ có ở chức vụ lãnh đạo, quản lý")
			}
		}
		if a.MgmtLevel != "" && !reMgmtLevel.MatchString(a.MgmtLevel) {
			return invalid("cấp quản lý %q", a.MgmtLevel)
		}
		keep.Leader, keep.Allowance, keep.MgmtLevel = a.Leader, a.Allowance, a.MgmtLevel
	case KindGrade:
		if !contains(EquivGroups, a.EquivGroup) {
			return invalid("nhóm tương đương phải là một trong %v", EquivGroups)
		}
		if a.GradeType != GradeTypeCongChuc && a.GradeType != GradeTypeVienChuc {
			return invalid("loại ngạch: %s | %s", GradeTypeCongChuc, GradeTypeVienChuc)
		}
		if utf8.RuneCountInString(a.SalaryClass) > 20 {
			return invalid("loại bảng lương dài tối đa 20 ký tự")
		}
		keep.EquivGroup, keep.GradeType, keep.SalaryClass = a.EquivGroup, a.GradeType, a.SalaryClass
	case KindEducationLevel:
		if a.Rank < 1 || a.Rank > 100 {
			return invalid("thứ hạng trình độ 1–100")
		}
		keep.Rank = a.Rank
	case KindMajorGroup:
		if !reCode.MatchString(a.Level) {
			return invalid("nhóm chuyên ngành phải gắn một trình độ")
		}
		keep.Level = a.Level
	case KindMajor:
		for lv, g := range a.Groups {
			if !reCode.MatchString(lv) || !reCode.MatchString(g) {
				return invalid("nhóm theo trình độ %q → %q", lv, g)
			}
		}
		keep.Groups = a.Groups
	case KindEthnicity:
		keep.Minority = a.Minority
	case KindReligion:
		keep.NoReligion = a.NoReligion
	}
	if !sameAttrs(a, keep) {
		return invalid("thuộc tính không thuộc loại %s", k)
	}
	return nil
}

func sameAttrs(a, b Attrs) bool {
	if a.Leader != b.Leader || a.MgmtLevel != b.MgmtLevel || a.EquivGroup != b.EquivGroup || a.GradeType != b.GradeType ||
		a.SalaryClass != b.SalaryClass || a.Rank != b.Rank || a.Level != b.Level || a.Minority != b.Minority ||
		a.NoReligion != b.NoReligion || len(a.Groups) != len(b.Groups) {
		return false
	}
	if (a.Allowance == nil) != (b.Allowance == nil) {
		return false
	}
	return true
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// checkCoef kiểm hệ số: lo < c ≤ hi (lo = 0 cho phép 0 < c), tối đa 2 chữ số thập phân (numeric(5,2)).
func checkCoef(what string, c, lo, hi decimal.Decimal) error {
	if !c.GreaterThan(lo) || c.GreaterThan(hi) {
		return invalid("%s %s ngoài khoảng (%s; %s]", what, c, lo, hi)
	}
	if !c.Equal(c.Round(2)) {
		return invalid("%s %s: tối đa 2 chữ số thập phân", what, c)
	}
	return nil
}

// GradeScale là một phiên bản bảng hệ số của một ngạch/CDNN (FN-19 B3): số bậc, hệ số từng bậc (bậc 1..n), thời gian
// giữ bậc (tháng — NLTX), ngày hiệu lực.
type GradeScale struct {
	GradeCode    string
	ValidFrom    time.Time // ngày (00:00 UTC của ngày giờ VN)
	Coefficients []decimal.Decimal
	HoldMonths   int
	Note         string
}

// Steps là số bậc.
func (s GradeScale) Steps() int { return len(s.Coefficients) }

// Coefficient trả hệ số của bậc step (1..n); ok = false nếu ngoài khoảng.
func (s GradeScale) Coefficient(step int) (decimal.Decimal, bool) {
	if step < 1 || step > len(s.Coefficients) {
		return decimal.Decimal{}, false
	}
	return s.Coefficients[step-1], true
}

// ValidateScale kiểm bảng hệ số: 1–20 bậc, hệ số 0 < c ≤ 99,99, tăng dần nghiêm ngặt, 2 chữ số thập phân; giữ bậc
// 1–120 tháng; có ngày hiệu lực.
func ValidateScale(s GradeScale) error {
	if !reGradeCode.MatchString(s.GradeCode) {
		return invalid("mã ngạch %q", s.GradeCode)
	}
	if s.ValidFrom.IsZero() {
		return invalid("thiếu ngày hiệu lực")
	}
	if n := len(s.Coefficients); n < 1 || n > 20 {
		return invalid("số bậc 1–20 (có %d)", n)
	}
	max := decimal.RequireFromString("99.99")
	for i, c := range s.Coefficients {
		if err := checkCoef(fmt.Sprintf("hệ số bậc %d", i+1), c, decimal.Zero, max); err != nil {
			return err
		}
		if i > 0 && !c.GreaterThan(s.Coefficients[i-1]) {
			return invalid("hệ số bậc %d (%s) phải lớn hơn bậc %d (%s)", i+1, c, i, s.Coefficients[i-1])
		}
	}
	if s.HoldMonths < 1 || s.HoldMonths > 120 {
		return invalid("thời gian giữ bậc 1–120 tháng")
	}
	if utf8.RuneCountInString(s.Note) > 500 {
		return invalid("ghi chú tối đa 500 ký tự")
	}
	return nil
}

// BusinessDay trả ngày nghiệp vụ (giờ VN) của thời điểm t, dạng 00:00 UTC của ngày đó — khớp cột date của DB.
func BusinessDay(t time.Time) time.Time {
	y, m, d := t.In(clock.Zone()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
