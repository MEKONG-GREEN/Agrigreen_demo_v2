package domain

// W11.25 (pf 20261021122821) — NÂNG BẬC LƯƠNG (BRD HCTH v3.0 FN-09 danh sách nâng bậc NL01–NL04, FN-10 sổ quyết định).
//
// Tính THUẦN (không DB): từ phép chiếu hồ sơ tại ngày xét (ngạch, bậc, mốc xếp bậc = step_since — Q-069), bảng hệ số có
// hiệu lực (số bậc, thời gian giữ bậc), xếp loại năm, kỷ luật (cờ nhập tay — Q-075), khen thưởng + bảng quy đổi thành
// tích và tham số nâng bậc có phiên bản (Q-070) → các KỲ NÂNG BẬC của một người:
//   - NLTX (Mẫu số 1): chưa ở bậc cuối; ngày đến kỳ = mốc xếp bậc + thời gian giữ bậc (+ kéo dài theo kỷ luật trong
//     thời gian giữ bậc). Điều kiện BR-02: xếp loại các năm xét từ mức tối thiểu trở lên, không đang thi hành kỷ luật.
//   - NLTH (Mẫu số 2): chỉ khi Phòng HCTH ĐỀ XUẤT từ một thành tích ghi trong hồ sơ (không tự đưa vào); ngày nâng = ngày
//     đến kỳ NLTX − số tháng quy đổi. Chặn xuất: thành tích đã dùng/không quy đổi/quá số năm, ngày nâng ngoài năm chỉ
//     tiêu (dạng "dự kiến"), vượt chỉ tiêu (app kiểm).
//   - TNVK (Mẫu số 4): đang ở bậc cuối; lần đầu sau tnvk_first_months tháng giữ bậc cuối (% khởi điểm), các lần sau mỗi
//     chu kỳ từ ngày hưởng của quyết định TNVK gần nhất (% + bước tăng). % TNVK không lưu trong hồ sơ (NL04-03).
// Hệ thống không tính tiền lương (BR-03, BR-04): cột 19 Mẫu số 1 để trống.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PromoKind là loại nâng bậc.
type PromoKind string

// Ba loại nâng bậc của FN-09.
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

// worse trả kết quả xấu hơn.
func worse(a, b Cond) Cond {
	rank := map[Cond]int{CondOK: 0, CondReview: 1, CondFail: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// PromoStatus là trạng thái xử lý của một kỳ (FN-09 B1).
type PromoStatus string

// Bốn trạng thái xử lý.
const (
	PromoPending  PromoStatus = "pending"  // Chưa xử lý
	PromoListed   PromoStatus = "listed"   // Đã đưa vào danh sách
	PromoExported PromoStatus = "exported" // Đã xuất danh sách
	PromoDecided  PromoStatus = "decided"  // Đã có quyết định
)

// PromoStatuses theo thứ tự.
var PromoStatuses = []PromoStatus{PromoPending, PromoListed, PromoExported, PromoDecided}

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
		return PromoListed, true
	case PromoActExclude:
		if k == PromoNLTH {
			return PromoListed, true
		}
		return PromoPending, true
	case PromoActWithdraw:
		return "", false
	case PromoActExport, PromoActUndecide:
		return PromoExported, true
	case PromoActDecide:
		return PromoDecided, true
	case PromoActReturn:
		if k == PromoNLTH {
			return PromoListed, true
		}
		return PromoPending, true
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
	d, err := decimal.NewFromString(strings.ReplaceAll(strings.TrimSpace(s), ",", "."))
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
	Key         string
	StaffID     uuid.UUID
	Kind        PromoKind
	Due         time.Time // ngày đến kỳ (NLTH: ngày nâng trước hạn)
	NormalDue   time.Time // NLTH: ngày đến kỳ thường xuyên
	NextDue     time.Time // thời gian tính nâng bậc / hưởng TNVK lần sau
	Grade       string
	Steps       int
	FromStep    int
	ToStep      int
	FromCoeff   decimal.Decimal
	ToCoeff     decimal.Decimal
	Diff        decimal.Decimal // hệ số chênh lệch (NL02-12, NL04-07)
	StepSince   time.Time
	Reserved    string // HS bảo lưu hiện có (Q-069)
	EarlyMonths int    // NLTH: số tháng nâng trước hạn
	Award       *PromoAward
	PctFrom     *decimal.Decimal // TNVK: % đang hưởng (nil = lần đầu)
	PctTo       decimal.Decimal
	PctSince    *time.Time
	Cond        Cond
	Why         []string // lý do khác Đủ
	Basis       []Basis
	Planned     int      // NLTH: năm dự kiến khi ngày nâng ngoài năm chỉ tiêu (0 = trong năm)
	Blocked     []string // NLTH: lý do chặn xuất Mẫu số 2
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

func ratingRank(r string) int {
	for i, x := range Ratings {
		if x == r {
			return i
		}
	}
	return len(Ratings)
}

// ratingBasis kiểm xếp loại các năm [y−n, y−1] (FN-09 BR-01, BR-02; AC-01).
func ratingBasis(ratings map[int]string, y int, rules PromotionRules) Basis {
	var years, vals, bad, miss []string
	for yr := y - rules.RatingYears; yr < y; yr++ {
		years = append(years, strconv.Itoa(yr))
		r := ratings[yr]
		switch {
		case r == "":
			vals = append(vals, "—")
			miss = append(miss, strconv.Itoa(yr))
		case ratingRank(r) > ratingRank(rules.MinRating):
			vals = append(vals, r)
			bad = append(bad, r+" "+strconv.Itoa(yr))
		default:
			vals = append(vals, r)
		}
	}
	b := Basis{Label: "Xếp loại " + strings.Join(years, " · "), Value: strings.Join(vals, " · "), Result: CondOK}
	switch {
	case len(bad) > 0:
		b.Result, b.Note = CondFail, strings.Join(bad, ", ")
	case len(miss) > 0:
		b.Result, b.Note = CondReview, "thiếu xếp loại "+strings.Join(miss, ", ")
	}
	return b
}

// disciplineBasis: đang thi hành kỷ luật tại ngày at → Chưa đủ (BR-02). Kỷ luật là cờ nhập tay (Q-075).
func disciplineBasis(ds []Event, at time.Time) Basis {
	for _, d := range ds {
		if !d.ValidFrom.After(at) && (d.ValidTo == nil || !d.ValidTo.Before(at)) {
			return Basis{Label: "Kỷ luật", Value: "Đang thi hành kỷ luật (" + d.Payload["form"] + ") từ " + fmtDay(d.ValidFrom), Result: CondFail,
				Note: "đang thi hành kỷ luật"}
		}
	}
	return Basis{Label: "Kỷ luật", Value: "Không có kỷ luật đang thi hành (cờ nhập tay trong hồ sơ)", Result: CondOK}
}

func (c *Candidate) add(b Basis) {
	c.Basis = append(c.Basis, b)
	c.Cond = worse(c.Cond, b.Result)
	if b.Result != CondOK && b.Note != "" {
		c.Why = append(c.Why, b.Note)
	}
}

// nltxDue: mốc + thời gian giữ bậc + kéo dài theo kỷ luật có hiệu lực trong thời gian giữ bậc (tham số Q-070).
func nltxDue(since time.Time, hold int, ds []Event, rules PromotionRules) (time.Time, int) {
	base := AddMonths(since, hold)
	ext := 0
	for _, d := range ds {
		if !d.ValidFrom.Before(since) && d.ValidFrom.Before(base) {
			ext += rules.DisciplineExtensionMonths[d.Payload["form"]]
		}
	}
	return AddMonths(since, hold+ext), ext
}

// Compute trả các kỳ nâng bậc của một viên chức đang công tác (rỗng khi thiếu ngạch, bậc, bảng hệ số hoặc mốc xếp bậc).
// quotaYear là năm chỉ tiêu NLTH (năm của ngày xét).
func Compute(sub PromoSubject, rules PromotionRules, quotaYear int) []Candidate {
	s := sub.Snap
	if s.Status != StatusActive || s.Grade == "" || s.Step < 1 || len(sub.Coefficients) == 0 || sub.HoldMonths < 1 {
		return nil
	}
	since, _, err := ParseDate(s.StepSince)
	if err != nil {
		return nil
	}
	steps := len(sub.Coefficients)
	coeff := func(step int) decimal.Decimal {
		if step < 1 || step > steps {
			return decimal.Zero
		}
		return sub.Coefficients[step-1]
	}
	base := Candidate{StaffID: sub.StaffID, Grade: s.Grade, Steps: steps, FromStep: s.Step, FromCoeff: coeff(s.Step), StepSince: since,
		Reserved: s.Reserved, Cond: CondOK}
	moc := Basis{Label: "Mốc xếp bậc hiện tại", Value: fmtDay(since), Result: CondOK}
	var out []Candidate
	if s.Step < steps {
		due, ext := nltxDue(since, sub.HoldMonths, sub.Disciplines, rules)
		c := base
		c.Kind, c.Due, c.NextDue, c.ToStep, c.ToCoeff = PromoNLTX, due, due, s.Step+1, coeff(s.Step+1)
		c.Diff = c.ToCoeff.Sub(c.FromCoeff)
		c.Key = CaseKey(sub.StaffID, PromoNLTX, c.ToStep, due, uuid.Nil)
		c.add(moc)
		hold := Basis{Label: "Thời gian giữ bậc", Value: fmt.Sprintf("%d tháng (%s → %s)", sub.HoldMonths, fmtDay(since), fmtDay(due)),
			Result: CondOK}
		if ext > 0 {
			hold.Value = fmt.Sprintf("%d tháng + kéo dài %d tháng do kỷ luật (%s → %s)", sub.HoldMonths, ext, fmtDay(since), fmtDay(due))
		}
		c.add(hold)
		c.add(ratingBasis(sub.Ratings, due.Year(), rules))
		c.add(disciplineBasis(sub.Disciplines, due))
		out = append(out, c)
		for _, aid := range sub.Proposals {
			var aw *PromoAward
			for i := range sub.Awards {
				if sub.Awards[i].EventID == aid {
					aw = &sub.Awards[i]
				}
			}
			if aw != nil {
				out = append(out, nlth(base, *aw, due, moc, sub, rules, quotaYear))
			}
		}
		return out
	}
	// Bậc cuối → TNVK.
	c := base
	c.Kind, c.ToStep, c.ToCoeff = PromoTNVK, s.Step, coeff(s.Step)
	c.add(moc)
	c.add(Basis{Label: "Ngạch, bậc", Value: fmt.Sprintf("%s · bậc cuối %d/%d", s.Grade, s.Step, steps), Result: CondOK})
	if sub.TNVK == nil {
		c.Due, c.PctTo = AddMonths(since, rules.TNVKFirstMonths), rules.TNVKStartPct
		c.add(Basis{Label: "Giữ bậc cuối lần đầu", Value: fmt.Sprintf("%d tháng (%s → %s)", rules.TNVKFirstMonths, fmtDay(since), fmtDay(c.Due)),
			Result: CondOK})
	} else {
		p, at := sub.TNVK.Pct, sub.TNVK.Since
		c.PctFrom, c.PctSince = &p, &at
		c.Due, c.PctTo = AddMonths(at, rules.TNVKCycleMonths), p.Add(rules.TNVKStepPct)
		c.add(Basis{Label: "Chu kỳ hưởng TNVK", Value: fmt.Sprintf("%d tháng (%s → %s), đang hưởng %s%%", rules.TNVKCycleMonths, fmtDay(at),
			fmtDay(c.Due), p.String()), Result: CondOK})
	}
	from := decimal.Zero
	if c.PctFrom != nil {
		from = *c.PctFrom
	}
	c.Diff = c.FromCoeff.Mul(c.PctTo.Sub(from)).Div(decimal.NewFromInt(100)).Round(2)
	c.NextDue = AddMonths(c.Due, rules.TNVKCycleMonths)
	c.Key = CaseKey(sub.StaffID, PromoTNVK, 0, c.Due, uuid.Nil)
	c.add(ratingBasis(sub.Ratings, c.Due.Year(), rules))
	c.add(disciplineBasis(sub.Disciplines, c.Due))
	return append(out, c)
}

// nlth dựng kỳ nâng bậc trước hạn từ một thành tích (NL03): ngày nâng = ngày đến kỳ thường xuyên − số tháng quy đổi.
func nlth(base Candidate, aw PromoAward, normal time.Time, moc Basis, sub PromoSubject, rules PromotionRules, quotaYear int) Candidate {
	c := base
	early := AddMonths(normal, -aw.Months)
	c.Kind, c.Due, c.NormalDue, c.NextDue, c.EarlyMonths = PromoNLTH, early, normal, early, aw.Months
	c.ToStep = base.FromStep + 1
	c.ToCoeff = sub.Coefficients[c.ToStep-1]
	c.Diff = c.ToCoeff.Sub(c.FromCoeff)
	a := aw
	c.Award = &a
	c.Key = CaseKey(sub.StaffID, PromoNLTH, 0, early, aw.EventID)
	c.add(moc)
	award := Basis{Label: fmt.Sprintf("Thành tích %d năm gần nhất", rules.AchievementYears), Value: fmt.Sprintf("%s (quy đổi %d tháng)",
		aw.Label(), aw.Months), Result: CondOK}
	switch {
	case aw.Used:
		award.Result, award.Note = CondFail, "thành tích đã dùng"
	case aw.Months == 0:
		award.Result, award.Note = CondFail, "thành tích không thuộc bảng quy đổi"
	case aw.Year < quotaYear-rules.AchievementYears:
		award.Result, award.Note = CondFail, fmt.Sprintf("thành tích quá %d năm", rules.AchievementYears)
	}
	c.add(award)
	if award.Result != CondOK {
		c.Blocked = append(c.Blocked, award.Note)
	}
	day := Basis{Label: "Ngày nâng trước hạn", Value: fmt.Sprintf("%s − %d tháng = %s", fmtDay(normal), aw.Months, fmtDay(early)), Result: CondOK}
	if early.Year() != quotaYear {
		c.Planned = early.Year()
		day.Note = fmt.Sprintf("dự kiến năm %d: chưa xuất Mẫu số 2 trước năm chỉ tiêu", early.Year())
		c.Blocked = append(c.Blocked, fmt.Sprintf("ngày nâng %s ngoài năm chỉ tiêu %d", fmtDay(early), quotaYear))
	}
	c.Basis = append(c.Basis, day)
	c.add(ratingBasis(sub.Ratings, early.Year(), rules))
	c.add(disciplineBasis(sub.Disciplines, early))
	return c
}

// NLTHQuota là chỉ tiêu NLTH của năm: ⌊tỷ lệ % × số viên chức đang công tác⌋ (Q-070: "% số người trong danh sách trả
// lương" — giả định tạm = viên chức đang công tác tại ngày xét).
func NLTHQuota(activeVienChuc int, pct decimal.Decimal) int {
	return int(decimal.NewFromInt(int64(activeVienChuc)).Mul(pct).Div(decimal.NewFromInt(100)).Floor().IntPart())
}

// ConversionMonths tra số tháng quy đổi của hình thức khen thưởng (0 = không thuộc bảng).
func (c AchievementConversion) ConversionMonths(form string) int {
	for _, r := range c.Rules {
		if r.AwardCode == form {
			return r.Months
		}
	}
	return 0
}
