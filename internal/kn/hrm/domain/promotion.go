package domain

// FN-09 HCTH v4.0: nâng lương thường xuyên từ hồ sơ hiện hành.
// Loại NLTH/TNVK và bố cục cũ chỉ còn dùng ở FN-10 và khi đọc lại lịch sử xuất.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PromoKind là loại nâng bậc.
type PromoKind string

// Ba loại quyết định FN-10; FN-09 chỉ tự nhận diện PromoNLTX.
const (
	PromoNLTX PromoKind = "nltx" // nâng bậc thường xuyên — Mẫu số 1 (NL02)
	PromoNLTH PromoKind = "nlth" // nâng bậc trước thời hạn — Mẫu số 2 (NL03)
	PromoTNVK PromoKind = "tnvk" // phụ cấp thâm niên vượt khung — Mẫu số 4 (NL04)
)

// PromoKinds theo thứ tự hiển thị.
var PromoKinds = []PromoKind{PromoNLTX, PromoTNVK, PromoNLTH}

// ParsePromoKind kiểm loại nâng bậc.
func ParsePromoKind(s string) (PromoKind, bool) {
	for _, k := range PromoKinds {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// Form là mẫu xuất của loại: mau1 | mau2 | mau4.
func (k PromoKind) Form() string {
	switch k {
	case PromoNLTH:
		return "mau2"
	case PromoTNVK:
		return "mau4"
	}
	return "mau1"
}

// FormKind đổi mẫu xuất → loại.
func FormKind(form string) (PromoKind, bool) {
	for _, k := range PromoKinds {
		if k.Form() == form {
			return k, true
		}
	}
	return "", false
}

// Label là tên loại.
func (k PromoKind) Label() string {
	switch k {
	case PromoNLTH:
		return "Nâng bậc trước thời hạn"
	case PromoTNVK:
		return "Phụ cấp thâm niên vượt khung"
	}
	return "Nâng bậc thường xuyên"
}

// Cond là kết quả kiểm tra điều kiện của một kỳ.
type Cond string

// Kết quả điều kiện (FN-09 B2: Đủ / Chưa đủ kèm lý do; luồng ngoại lệ: thiếu xếp loại → "Cần xem xét").
const (
	CondOK     Cond = "ok"     // Đủ
	CondReview Cond = "review" // Cần xem xét
	CondFail   Cond = "fail"   // Chưa đủ
)

// PromoStatus là trạng thái xử lý của một kỳ (FN-09 B1).
type PromoStatus string

// Ba trạng thái xử lý FN-09; PromoListed chỉ để đọc sổ cũ.
const (
	PromoPending  PromoStatus = "pending"  // Chưa xử lý
	PromoListed   PromoStatus = "listed"   // Đã đưa vào danh sách
	PromoExported PromoStatus = "exported" // Đã xuất danh sách
	PromoDecided  PromoStatus = "decided"  // Đã có quyết định
)

// PromoStatuses theo thứ tự.
var PromoStatuses = []PromoStatus{PromoPending, PromoExported, PromoDecided}

// Hành động của sổ promotion_event và trạng thái sau hành động.
const (
	PromoActInclude  = "include"  // đưa vào danh sách (kèm lý do khi chưa đủ)
	PromoActExclude  = "exclude"  // bỏ khỏi danh sách
	PromoActPropose  = "propose"  // đề xuất NLTH từ thành tích
	PromoActWithdraw = "withdraw" // gỡ khỏi danh sách NLTH
	PromoActExport   = "export"   // đã xuất danh sách
	PromoActDecide   = "decide"   // đã có quyết định
	PromoActUndecide = "undecide" // quyết định bị hủy (sự kiện bù) → về "Đã xuất danh sách"
	PromoActReturn   = "return"   // không có tên trong quyết định → về bảng theo dõi kèm ghi chú (FN-10 BR-02)
)

// StatusAfter trả trạng thái sau hành động act của một kỳ loại k; ok = false khi hành động làm kỳ NLTH biến mất (gỡ).
func StatusAfter(k PromoKind, act string) (PromoStatus, bool) {
	switch act {
	case PromoActInclude, PromoActPropose:
		return PromoPending, true
	case PromoActExclude:
		if k == PromoNLTH {
			return PromoPending, true
		}
		return PromoPending, true
	case PromoActWithdraw:
		return "", false
	case PromoActExport, PromoActUndecide:
		return PromoExported, true
	case PromoActDecide:
		return PromoDecided, true
	case PromoActReturn:
		return PromoExported, true
	}
	return PromoPending, true
}

// AddMonths cộng m tháng (âm = trừ); ngày vượt cuối tháng → ngày cuối tháng (31/01 + 1 tháng = 28/02).
func AddMonths(t time.Time, m int) time.Time {
	y, mo, d := t.Date()
	first := time.Date(y, mo, 1, 0, 0, 0, 0, time.UTC).AddDate(0, m, 0)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(d, last), 0, 0, 0, 0, time.UTC)
}

// MonthsEnjoyed là số tháng được hưởng bậc mới trong tháng xét gợi ý (NL02-13: 1 khi hưởng từ ngày 01–15, 0,5 khi từ
// ngày 16 — mẫu).
func MonthsEnjoyed(due time.Time) string {
	if due.Day() <= 15 {
		return "1"
	}
	return "0.5"
}

// ValidMonthsEnjoyed kiểm số tháng nhập tay: số ≥ 0, ≤ 12, một chữ số thập phân.
func ValidMonthsEnjoyed(s string) (string, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if len(s) > 4 || !promoDecimalSyntax.MatchString(s) {
		return "", false
	}
	d, err := decimal.NewFromString(s)
	if err != nil || d.IsNegative() || d.GreaterThan(decimal.NewFromInt(12)) || !d.Equal(d.Round(1)) {
		return "", false
	}
	return d.String(), true
}

// Basis là một dòng "Căn cứ điều kiện" (FN-09 B3).
type Basis struct {
	Label  string
	Value  string
	Result Cond
	Note   string // lý do khi khác Đủ
}

// PromoAward là một khen thưởng ghi trong hồ sơ (HS-36) dùng cho NLTH.
type PromoAward struct {
	EventID uuid.UUID
	Form    string // mã hình thức khen thưởng
	Name    string // tên hình thức (danh mục)
	Title   string
	Year    int
	Months  int  // số tháng quy đổi (0 = không thuộc bảng quy đổi)
	Used    bool // đã dùng trong một quyết định NLTH còn hiệu lực
}

// Label là nhãn thành tích (NL03-05: "Bằng khen UBND tỉnh 2024").
func (a PromoAward) Label() string {
	n := a.Name
	if a.Title != "" {
		n = a.Title
	}
	return fmt.Sprintf("%s %d", n, a.Year)
}

// TNVKState là lần hưởng TNVK gần nhất (quyết định TNVK còn hiệu lực).
type TNVKState struct {
	Pct   decimal.Decimal
	Since time.Time
}

// PromoSubject là dữ liệu của một viên chức để tính kỳ nâng bậc.
type PromoSubject struct {
	StaffID      uuid.UUID
	Snap         Snapshot
	Coefficients []decimal.Decimal // bảng hệ số của ngạch tại ngày xét (nil = chưa có bảng)
	HoldMonths   int
	Ratings      map[int]string // năm → xếp loại (hiệu lực)
	Disciplines  []Event        // sự kiện kỷ luật hiệu lực
	Awards       []PromoAward
	TNVK         *TNVKState
	Proposals    []uuid.UUID // dòng khen thưởng đã được đề xuất NLTH (chưa gỡ)
}

// Candidate là một kỳ nâng bậc.
type Candidate struct {
	Key             string
	StaffID         uuid.UUID
	Kind            PromoKind
	Due             time.Time // ngày đến kỳ (NLTH: ngày nâng trước hạn)
	NormalDue       time.Time // NLTH: ngày đến kỳ thường xuyên
	NextDue         time.Time // thời gian tính nâng bậc / hưởng TNVK lần sau
	Grade           string
	Steps           int
	FromStep        int
	ToStep          int
	FromCoeff       decimal.Decimal
	ToCoeff         decimal.Decimal
	Diff            decimal.Decimal // hệ số chênh lệch (NL02-12, NL04-07)
	PromotionMonths int
	StepSince       time.Time
	Reserved        string // HS bảo lưu hiện có (Q-069)
	EarlyMonths     int    // NLTH: số tháng nâng trước hạn
	Award           *PromoAward
	PctFrom         *decimal.Decimal // TNVK: % đang hưởng (nil = lần đầu)
	PctTo           decimal.Decimal
	PctSince        *time.Time
	Cond            Cond
	Why             []string // lý do khác Đủ
	Basis           []Basis
	Planned         int      // NLTH: năm dự kiến khi ngày nâng ngoài năm chỉ tiêu (0 = trong năm)
	Blocked         []string // NLTH: lý do chặn xuất Mẫu số 2
}

// CaseKey dựng khóa kỳ: '<staff>:nltx:b<bậc mới>-<YYYYMMDD>', '<staff>:tnvk:v-<YYYYMMDD>', '<staff>:nlth:<award>'.
func CaseKey(staff uuid.UUID, k PromoKind, toStep int, due time.Time, award uuid.UUID) string {
	switch k {
	case PromoNLTH:
		return staff.String() + ":nlth:" + award.String()
	case PromoTNVK:
		return staff.String() + ":tnvk:v-" + due.Format("20060102")
	}
	return staff.String() + ":nltx:b" + strconv.Itoa(toStep) + "-" + due.Format("20060102")
}

// ParseCaseKey tách khóa kỳ → hồ sơ, loại.
func ParseCaseKey(s string) (uuid.UUID, PromoKind, bool) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[2] == "" || len(parts[2]) > 40 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(parts[0])
	k, ok := ParsePromoKind(parts[1])
	if err != nil || !ok || id.String() != parts[0] {
		return uuid.Nil, "", false
	}
	return id, k, true
}

func fmtDay(t time.Time) string { return t.Format("02/01/2006") }

var promoIntegerSyntax = regexp.MustCompile(`^[0-9]{1,3}$`)
var promoDecimalSyntax = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)

// ValidPromotionMonths bounds both syntax and length before converting NB-07.
func ValidPromotionMonths(s string) (int, bool) {
	if len(s) > 3 || !promoIntegerSyntax.MatchString(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= 120
}

// Compute only derives FN-09 regular promotion from the person's NB-06/NB-07.
// Ratings, disciplines, awards and legacy grade hold months never change this result.
func Compute(sub PromoSubject, _ PromotionRules, _ int) []Candidate {
	s := sub.Snap
	steps := len(sub.Coefficients)
	if s.Status != StatusActive || s.Grade == "" || s.Step < 1 || s.Step >= steps || s.PromotionMonths < 1 || s.PromotionMonths > 120 {
		return nil
	}
	since, precision, err := ParseDate(s.StepSince)
	if err != nil || precision != PrecisionDay {
		return nil
	}
	due := AddMonths(since, s.PromotionMonths)
	c := Candidate{StaffID: sub.StaffID, Kind: PromoNLTX, Grade: s.Grade, Steps: steps, FromStep: s.Step, ToStep: s.Step + 1,
		FromCoeff: sub.Coefficients[s.Step-1], ToCoeff: sub.Coefficients[s.Step], StepSince: since, Due: due, NextDue: due,
		Reserved: s.Reserved, Cond: CondOK, PromotionMonths: s.PromotionMonths}
	c.Diff = c.ToCoeff.Sub(c.FromCoeff)
	c.Key = CaseKey(sub.StaffID, PromoNLTX, c.ToStep, due, uuid.Nil)
	c.Basis = []Basis{{Label: "Ngày hưởng bậc hiện tại", Value: fmtDay(since), Result: CondOK},
		{Label: "Thời gian xét nâng lương", Value: fmt.Sprintf("%d tháng (%s → %s)", s.PromotionMonths, fmtDay(since), fmtDay(due)), Result: CondOK}}
	return []Candidate{c}
}
