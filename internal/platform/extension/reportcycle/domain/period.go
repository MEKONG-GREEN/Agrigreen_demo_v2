// Package domain: quy tắc thuần của vòng đời báo cáo KN v7.7 (gói P4) — chia kỳ, hạn nộp, trạng thái công
// việc, so khác lần tổng hợp, chỉ số dashboard. Không DB, không time.Now() (R-12), không float (R-10).
//
// # Lịch kỳ hệ thống dùng chung (W11.03, QĐ-6, Q-045)
//
// Quy tắc chia kỳ là PeriodRule có phiên bản; chuỗi Tag() ghi vào đợt (cột period_rule) nên kỳ đã lưu giữ nguyên
// danh tính khi quy tắc đổi — đọc lại bằng ParseRule(tag), KHÔNG đánh số lại kỳ cũ.
//
//   - RuleVersionV1 "kn.period_rule/v1-tam" (LegacyRule, Q-17 cũ): tuần thứ Hai–Chủ nhật, tháng dương lịch,
//     năm = một kỳ; chỉ week/month/season/year/adhoc. Giữ để đọc lại/tái tạo đợt đã phát hành.
//   - RuleVersionV2 "kn.period_rule/v2" (DefaultRule, đề xuất QĐ-6): tuần báo cáo Thứ Tư–Thứ Ba (WeekStart cấu
//     hình được). Số tuần "year" = tuần chứa 01/01 là tuần 1 (KHTC II.4, năm có 52/53 tuần); "iso" = năm của ngày
//     thứ 4 trong tuần (với tuần bắt đầu thứ Hai đúng là ISO 8601); "season" = Tuần n của đợt. Tháng/quý/năm với
//     MonthAlign "week_end" = các tuần báo cáo có ngày cuối tuần (Thứ Ba) thuộc kỳ (KHTC BR-DL-01, CNTS "tuần vắt
//     quý tính theo ngày cuối tuần"), "calendar" = dương lịch. Thêm kỳ ngày và tuần KSGM 4 tuần/tháng (mặc định
//     1–7, 8–14, 15–21, 22–cuối tháng; Cấp Tỉnh sửa ranh giới từng tháng — CNTS H04).
//
// Tham số (pf/masterdata, có phiên bản, tra lúc 00:00 giờ VN của NGÀY ĐẦU đợt/kỳ nên quy tắc mới chỉ áp dụng cho
// đợt/kỳ bắt đầu từ ngày hiệu lực của phiên bản tham số): port.ParamPeriodRule (Text = Tag(); EffectiveRule:
// vắng → LegacyRule) — dữ liệu nền masterdata seeddata/params.json nạp sẵn DefaultRule hiệu lực từ 2026-11-04
// (Thứ Tư đầu tiên sau hạn chốt QĐ-6 31/10/2026); và port.ParamKSGMWeekStarts + ":YYYY-MM" (Text "1,10,16,23",
// vắng → 1,8,15,22).
//
// Mọi ngày là ngày dương lịch 00:00 UTC (Date/ParseDate), không mang giờ nên không phụ thuộc múi giờ
// (Asia/Ho_Chi_Minh không có giờ mùa hè); giờ Việt Nam chỉ dùng khi tính hạn nộp (Deadline.DueAt). API:
//
//	ParseRule(tag string) (PeriodRule, error)          // đọc quy tắc của đợt/kỳ đã lưu (cột period_rule)
//	EffectiveRule(paramText string) PeriodRule         // giá trị tham số → quy tắc; vắng/sai → LegacyRule
//	(r PeriodRule) Tag() string; (r PeriodRule) Validate() error
//	(r PeriodRule) WeekOf(d time.Time) Week            // tuần báo cáo chứa ngày d
//	(r PeriodRule) Week(year, no int) (Week, error)    // tuần thứ no của năm
//	(r PeriodRule) WeeksInYear(year int) int           // 52 hoặc 53
//	(r PeriodRule) PeriodOf(pt string, d time.Time, ksgm KSGMStarts) (Span, error) // kỳ đầy đủ chứa ngày d
//	(r PeriodRule) DefaultDeadline(pt string) Deadline
//	KSGMWeeks(year int, m time.Month, starts []int) ([]Span, error)
//	ParseKSGMStarts(s string) ([]int, error)
//	Split(in SplitInput) ([]Period, error)             // chia [From, To] của đợt thành các kỳ (cắt hai đầu)
package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Loại kỳ (FN-TI-02.02 B3; day, ksgm_week, quarter chỉ có ở quy tắc v2).
const (
	PeriodDay      = "day"
	PeriodWeek     = "week"
	PeriodKSGMWeek = "ksgm_week"
	PeriodMonth    = "month"
	PeriodQuarter  = "quarter"
	PeriodSeason   = "season"
	PeriodYear     = "year"
	PeriodAdhoc    = "adhoc"
)

// Loại báo cáo (khớp reports/port.Report*) và loại kỳ mặc định của nó.
var ReportPeriodType = map[string]string{
	"weekly": PeriodWeek, "monthly": PeriodMonth, "seasonal": PeriodSeason, "annual": PeriodYear, "adhoc": PeriodAdhoc,
}

// ReportPeriodTypes là các loại kỳ Cấp Tỉnh được chọn khi phát hành mỗi loại báo cáo (W11.05b, giả định chờ Q-045:
// loại báo cáo của pf/reports chưa có "ngày"/"quý" nên kỳ ngày đi với báo cáo tuần/đột xuất, tuần KSGM với báo cáo
// tuần, quý với báo cáo tháng/năm). Phần tử đầu = ReportPeriodType (mặc định). Ngày/quý/tuần KSGM chỉ chia được với
// quy tắc v2 (PeriodOf báo lỗi period_type với v1).
var ReportPeriodTypes = map[string][]string{
	"weekly":   {PeriodWeek, PeriodKSGMWeek, PeriodDay},
	"monthly":  {PeriodMonth, PeriodQuarter},
	"seasonal": {PeriodSeason},
	"annual":   {PeriodYear, PeriodQuarter},
	"adhoc":    {PeriodAdhoc, PeriodDay},
}

// ResolvePeriodType trả loại kỳ của đợt: rỗng → mặc định của loại báo cáo; khác → phải thuộc ReportPeriodTypes.
func ResolvePeriodType(reportType, periodType string) (string, error) {
	allowed, ok := ReportPeriodTypes[reportType]
	if !ok {
		return "", ErrPeriod{"report_type", "weekly | monthly | seasonal | annual | adhoc"}
	}
	if periodType == "" {
		return allowed[0], nil
	}
	for _, pt := range allowed {
		if pt == periodType {
			return pt, nil
		}
	}
	return "", ErrPeriod{"period_type", "loại báo cáo " + reportType + " chia kỳ " + strings.Join(allowed, " | ")}
}

// Cách đánh số tuần: "season" = Tuần n của đợt (cột Tuần 1, Tuần 2 của PL 01); "year" = tuần của năm, tuần 1 là
// tuần chứa 01/01 (KHTC II.4); "iso" = tuần của năm theo ngày thứ 4 của tuần (ISO 8601 khi tuần bắt đầu thứ Hai).
const (
	WeekNumberingSeason = "season"
	WeekNumberingYear   = "year"
	WeekNumberingISO    = "iso"
)

// Cách ghép ngày vào tháng/quý/năm.
const (
	MonthAlignCalendar = "calendar" // dương lịch
	MonthAlignWeekEnd  = "week_end" // các tuần báo cáo có ngày cuối tuần thuộc kỳ (KHTC BR-DL-01)
)

// Phiên bản quy tắc chia kỳ.
const (
	RuleVersionV1 = "kn.period_rule/v1-tam"
	RuleVersionV2 = "kn.period_rule/v2"
)

// PeriodRule là quy tắc chia kỳ có phiên bản (Tag ghi vào đợt phát hành để kỳ đã chia không đổi khi quy tắc đổi).
type PeriodRule struct {
	Version       string
	WeekStart     time.Weekday // v1 luôn thứ Hai
	WeekNumbering string
	MonthAlign    string // v1 luôn calendar
}

// LegacyRule là quy tắc v1 tạm (Q-17) — quy tắc của đợt cũ và của ngày trước phiên bản tham số đầu tiên.
var LegacyRule = PeriodRule{Version: RuleVersionV1, WeekStart: time.Monday, WeekNumbering: WeekNumberingSeason, MonthAlign: MonthAlignCalendar}

// DefaultRule là quy tắc v2 theo đề xuất QĐ-6 (chờ PO chốt Q-045): tuần Thứ Tư–Thứ Ba, số tuần theo năm,
// tháng/quý/năm theo Thứ Ba cuối. Là giá trị tham số nạp sẵn và giá trị mặc định khi ParseRule gặp khóa vắng.
var DefaultRule = PeriodRule{Version: RuleVersionV2, WeekStart: time.Wednesday, WeekNumbering: WeekNumberingYear, MonthAlign: MonthAlignWeekEnd}

var weekdayCode = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Tag là chuỗi ghi vào cột period_rule của đợt. v1 giữ đúng định dạng cũ "kn.period_rule/v1-tam;week=season".
func (r PeriodRule) Tag() string {
	if r.Version == RuleVersionV1 {
		return r.Version + ";week=" + r.WeekNumbering
	}
	return r.Version + ";week_start=" + weekdayCode[(int(r.WeekStart)%7+7)%7] + ";week=" + r.WeekNumbering + ";month=" + r.MonthAlign
}

// Validate kiểm tra quy tắc.
func (r PeriodRule) Validate() error {
	switch r.Version {
	case RuleVersionV1:
		if r.WeekStart != time.Monday || r.MonthAlign != MonthAlignCalendar || (r.WeekNumbering != WeekNumberingSeason && r.WeekNumbering != WeekNumberingISO) {
			return errors.New("quy tắc v1: tuần thứ Hai, tháng dương lịch, week=season|iso")
		}
	case RuleVersionV2:
		if r.WeekStart < time.Sunday || r.WeekStart > time.Saturday {
			return errors.New("week_start: sun..sat")
		}
		if r.WeekNumbering != WeekNumberingSeason && r.WeekNumbering != WeekNumberingYear && r.WeekNumbering != WeekNumberingISO {
			return errors.New("week: season | year | iso")
		}
		if r.MonthAlign != MonthAlignCalendar && r.MonthAlign != MonthAlignWeekEnd {
			return errors.New("month: calendar | week_end")
		}
	default:
		return errors.New("phiên bản quy tắc chia kỳ không hỗ trợ: " + r.Version)
	}
	return nil
}

// ParseRule đọc chuỗi Tag(); khóa vắng lấy mặc định của phiên bản (v1: LegacyRule, v2: DefaultRule).
func ParseRule(tag string) (PeriodRule, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return PeriodRule{}, errors.New("quy tắc chia kỳ trống")
	}
	parts := strings.Split(tag, ";")
	var r PeriodRule
	switch strings.TrimSpace(parts[0]) {
	case RuleVersionV1:
		r = LegacyRule
	case RuleVersionV2:
		r = DefaultRule
	default:
		return PeriodRule{}, errors.New("phiên bản quy tắc chia kỳ không hỗ trợ: " + parts[0])
	}
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok {
			return PeriodRule{}, errors.New("quy tắc chia kỳ sai dạng: " + p)
		}
		switch k {
		case "week":
			r.WeekNumbering = v
		case "month":
			r.MonthAlign = v
		case "week_start":
			found := false
			for i, c := range weekdayCode {
				if c == v {
					r.WeekStart, found = time.Weekday(i), true
				}
			}
			if !found {
				return PeriodRule{}, errors.New("week_start: sun..sat")
			}
		default:
			return PeriodRule{}, errors.New("khóa quy tắc chia kỳ lạ: " + k)
		}
	}
	return r, r.Validate()
}

// EffectiveRule trả quy tắc từ giá trị tham số port.ParamPeriodRule tra tại ngày đầu đợt/kỳ: rỗng (chưa có phiên
// bản hiệu lực) hoặc sai dạng → LegacyRule, để ngày trước phiên bản tham số đầu tiên giữ đúng lịch cũ.
func EffectiveRule(paramText string) PeriodRule {
	r, err := ParseRule(paramText)
	if err != nil {
		return LegacyRule
	}
	return r
}

// Deadline là quy tắc hạn nộp mỗi kỳ: ngày cuối kỳ + OffsetDays, lúc Time (giờ Việt Nam).
type Deadline struct {
	OffsetDays int
	Time       string // "HH:MM"
}

// DefaultDeadline trả hạn mặc định của quy tắc v1 (Q-17 tạm) — giữ cho đợt cũ; mới dùng PeriodRule.DefaultDeadline.
func DefaultDeadline(periodType string) Deadline { return LegacyRule.DefaultDeadline(periodType) }

// DefaultDeadline trả hạn mặc định theo loại kỳ. v1: tuần → +2 ngày 17:00 (Thứ Ba tuần sau); tháng → ngày 10
// tháng sau 17:00 (UR-XA-09); khác → +7 ngày. v2 (giả định, chờ Q-045): tuần/tuần KSGM → 12:00 ngày sau ngày cuối
// kỳ (CNTS H04); ngày → 09:00 hôm sau (CN-06); tháng/quý/năm theo Thứ Ba cuối → 10:00 Thứ Tư kế tiếp (KHTC II.4),
// theo dương lịch → ngày 10 tháng sau 17:00; vụ/đột xuất → +7 ngày 17:00.
func (r PeriodRule) DefaultDeadline(periodType string) Deadline {
	if r.Version == RuleVersionV1 {
		switch periodType {
		case PeriodWeek:
			return Deadline{OffsetDays: 2, Time: "17:00"}
		case PeriodMonth:
			return Deadline{OffsetDays: 10, Time: "17:00"}
		}
		return Deadline{OffsetDays: 7, Time: "17:00"}
	}
	switch periodType {
	case PeriodWeek, PeriodKSGMWeek:
		return Deadline{OffsetDays: 1, Time: "12:00"}
	case PeriodDay:
		return Deadline{OffsetDays: 1, Time: "09:00"}
	case PeriodMonth, PeriodQuarter, PeriodYear:
		if r.MonthAlign == MonthAlignWeekEnd {
			return Deadline{OffsetDays: 1, Time: "10:00"}
		}
		return Deadline{OffsetDays: 10, Time: "17:00"}
	}
	return Deadline{OffsetDays: 7, Time: "17:00"}
}

// DefaultCloseAfterDays: số ngày chốt kỳ sau hạn (tạm).
const DefaultCloseAfterDays = 3

// MaxPeriods giới hạn số kỳ một đợt (vụ dài nhất ~ 26 tuần; năm 12 tháng); kỳ ngày dùng MaxDayPeriods.
const (
	MaxPeriods    = 60
	MaxDayPeriods = 366
)

// Week là một tuần báo cáo đủ 7 ngày.
type Week struct {
	Year, No int
	From, To time.Time
}

// Span là một kỳ đầy đủ theo lịch (chưa cắt theo khoảng của đợt). Key là danh tính ổn định trong một quy tắc:
// "2026-10-10" (ngày), "2026-W41" (tuần), "2026-10-K2" (tuần KSGM), "2026-10", "2026-Q4", "2026".
type Span struct {
	Key, Label string
	From, To   time.Time
	WeekYear   int // chỉ kỳ tuần
	WeekNo     int // chỉ kỳ tuần
}

// KSGMStarts trả ngày bắt đầu 4 tuần KSGM của một tháng (đã sửa ranh giới); hàm nil hoặc trả nil → 1, 8, 15, 22.
type KSGMStarts func(year int, m time.Month) []int

// Period là một kỳ đã chia (cắt theo khoảng của đợt).
type Period struct {
	Slot        int
	Key         string // Span.Key; rỗng với vụ/đột xuất (và năm ở v1)
	Label       string
	RangeLabel  string
	ISOWeek     int // số tuần của năm theo quy tắc (v1: ISO 8601; v2: theo WeekNumbering); 0 nếu không phải kỳ tuần
	WeekYear    int // năm của ISOWeek; 0 nếu không phải kỳ tuần
	From, To    time.Time
	DueAt       time.Time // UTC
	AutoCloseAt time.Time // UTC
}

// Date chuẩn hóa một ngày dương lịch về 00:00 UTC.
func Date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ParseDate đọc "YYYY-MM-DD".
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, errors.New("ngày phải dạng YYYY-MM-DD")
	}
	return t, nil
}

// ParseClock đọc "HH:MM".
func ParseClock(s string) (int, int, error) {
	h, m, ok := strings.Cut(s, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if !ok || e1 != nil || e2 != nil || len(h) != 2 || len(m) != 2 || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, errors.New("giờ phải dạng HH:MM")
	}
	return hh, mm, nil
}

// DueAt tính hạn nộp UTC của kỳ kết thúc ngày to theo quy tắc d (giờ Việt Nam loc).
func (d Deadline) DueAt(to time.Time, loc *time.Location) (time.Time, error) {
	hh, mm, err := ParseClock(d.Time)
	if err != nil {
		return time.Time{}, err
	}
	day := to.AddDate(0, 0, d.OffsetDays)
	return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc).UTC(), nil
}

// ErrPeriod là lỗi tham số chia kỳ (field, thông báo).
type ErrPeriod struct{ Field, Msg string }

func (e ErrPeriod) Error() string { return e.Field + ": " + e.Msg }

func dateOf(t time.Time) time.Time { return Date(t.Year(), t.Month(), t.Day()) }

func (r PeriodRule) v2() bool { return r.Version != RuleVersionV1 }

func (r PeriodRule) weekStart() time.Weekday {
	if !r.v2() {
		return time.Monday
	}
	return r.WeekStart
}

func (r PeriodRule) startOf(d time.Time) time.Time {
	d = dateOf(d)
	return d.AddDate(0, 0, -((int(d.Weekday()) - int(r.weekStart()) + 7) % 7))
}

// isoNumbering: năm của tuần theo ngày thứ 4 (v1 luôn ISO để giữ iso_week của kỳ cũ).
func (r PeriodRule) isoNumbering() bool { return !r.v2() || r.WeekNumbering == WeekNumberingISO }

// week1 là ngày đầu tuần 1 của năm y.
func (r PeriodRule) week1(y int) time.Time {
	if r.isoNumbering() {
		return r.startOf(Date(y, 1, 4))
	}
	return r.startOf(Date(y, 1, 1))
}

func days(a, b time.Time) int { return int(b.Sub(a) / (24 * time.Hour)) }

// WeekOf trả tuần báo cáo chứa ngày d.
func (r PeriodRule) WeekOf(d time.Time) Week {
	from := r.startOf(d)
	to := from.AddDate(0, 0, 6)
	y := to.Year() // "year"/"season": tuần chứa 01/01 thuộc năm của ngày cuối tuần
	if r.isoNumbering() {
		y = from.AddDate(0, 0, 3).Year()
	}
	return Week{Year: y, No: days(r.week1(y), from)/7 + 1, From: from, To: to}
}

// WeeksInYear trả số tuần của năm (52 hoặc 53).
func (r PeriodRule) WeeksInYear(y int) int { return days(r.week1(y), r.week1(y+1)) / 7 }

// Week trả tuần thứ no của năm.
func (r PeriodRule) Week(y, no int) (Week, error) {
	if n := r.WeeksInYear(y); no < 1 || no > n {
		return Week{}, ErrPeriod{"week", fmt.Sprintf("năm %d có %d tuần", y, n)}
	}
	return r.WeekOf(r.week1(y).AddDate(0, 0, 7*(no-1))), nil
}

func (r PeriodRule) weekEndAligned() bool { return r.v2() && r.MonthAlign == MonthAlignWeekEnd }

// anchor là ngày dùng để xếp d vào tháng/quý/năm.
func (r PeriodRule) anchor(d time.Time) time.Time {
	if r.weekEndAligned() {
		return r.WeekOf(d).To
	}
	return dateOf(d)
}

// boundary trả ngày đầu của kỳ tháng/quý/năm có ngày dương lịch đầu tiên là first.
func (r PeriodRule) boundary(first time.Time) time.Time {
	if r.weekEndAligned() {
		return r.WeekOf(first).From
	}
	return first
}

var romanQ = [...]string{"I", "II", "III", "IV"}

// PeriodOf trả kỳ đầy đủ chứa ngày d theo loại kỳ (vụ/đột xuất không có lịch → lỗi).
func (r PeriodRule) PeriodOf(pt string, d time.Time, ksgm KSGMStarts) (Span, error) {
	if err := r.Validate(); err != nil {
		return Span{}, ErrPeriod{"period_rule", err.Error()}
	}
	d = dateOf(d)
	span := func(key, label string, first, next time.Time) Span {
		return Span{Key: key, Label: label, From: r.boundary(first), To: r.boundary(next).AddDate(0, 0, -1)}
	}
	switch {
	case pt == PeriodWeek:
		w := r.WeekOf(d)
		return Span{Key: fmt.Sprintf("%d-W%02d", w.Year, w.No), Label: fmt.Sprintf("Tuần %d/%d", w.No, w.Year), From: w.From, To: w.To, WeekYear: w.Year, WeekNo: w.No}, nil
	case pt == PeriodMonth:
		a := r.anchor(d)
		first := Date(a.Year(), a.Month(), 1)
		return span(first.Format("2006-01"), fmt.Sprintf("Tháng %d/%d", a.Month(), a.Year()), first, first.AddDate(0, 1, 0)), nil
	case pt == PeriodDay && r.v2():
		return Span{Key: d.Format(time.DateOnly), Label: "Ngày " + d.Format("02/01/2006"), From: d, To: d}, nil
	case pt == PeriodQuarter && r.v2():
		a := r.anchor(d)
		q := (int(a.Month()) - 1) / 3
		first := Date(a.Year(), time.Month(q*3+1), 1)
		return span(fmt.Sprintf("%d-Q%d", a.Year(), q+1), fmt.Sprintf("Quý %s/%d", romanQ[q], a.Year()), first, first.AddDate(0, 3, 0)), nil
	case pt == PeriodYear && r.v2():
		a := r.anchor(d)
		first := Date(a.Year(), 1, 1)
		return span(strconv.Itoa(a.Year()), "Năm "+strconv.Itoa(a.Year()), first, first.AddDate(1, 0, 0)), nil
	case pt == PeriodKSGMWeek && r.v2():
		var starts []int
		if ksgm != nil {
			starts = ksgm(d.Year(), d.Month())
		}
		ws, err := KSGMWeeks(d.Year(), d.Month(), starts)
		if err != nil {
			return Span{}, err
		}
		for _, w := range ws {
			if !d.After(w.To) {
				return w, nil
			}
		}
	}
	if r.v2() {
		return Span{}, ErrPeriod{"period_type", "day | week | ksgm_week | month | quarter | season | year | adhoc"}
	}
	return Span{}, ErrPeriod{"period_type", "week | month | season | year | adhoc"}
}

// DefaultKSGMStarts là ranh giới mặc định 4 tuần KSGM của tháng (CNTS H04).
var DefaultKSGMStarts = []int{1, 8, 15, 22}

// KSGMWeeks chia tháng thành 4 tuần KSGM theo ngày bắt đầu starts (4 số tăng dần, starts[0] = 1, tuần 4 tới hết
// tháng); starts rỗng → DefaultKSGMStarts.
func KSGMWeeks(y int, m time.Month, starts []int) ([]Span, error) {
	if len(starts) == 0 {
		starts = DefaultKSGMStarts
	}
	last := Date(y, m, 1).AddDate(0, 1, -1).Day()
	if len(starts) != 4 || starts[0] != 1 {
		return nil, ErrPeriod{"ksgm_week_starts", "4 ngày bắt đầu, tuần 1 từ ngày 1"}
	}
	for i := 1; i < 4; i++ {
		if starts[i] <= starts[i-1] || starts[i] > last {
			return nil, ErrPeriod{"ksgm_week_starts", fmt.Sprintf("ngày bắt đầu tăng dần, không quá %d", last)}
		}
	}
	out := make([]Span, 4)
	for i := range 4 {
		end := last
		if i < 3 {
			end = starts[i+1] - 1
		}
		out[i] = Span{Key: fmt.Sprintf("%d-%02d-K%d", y, m, i+1), Label: fmt.Sprintf("Tuần KSGM %d tháng %d/%d", i+1, m, y),
			From: Date(y, m, starts[i]), To: Date(y, m, end)}
	}
	return out, nil
}

// ParseKSGMStarts đọc "1,10,16,23" (giá trị tham số port.ParamKSGMWeekStarts).
func ParseKSGMStarts(s string) ([]int, error) {
	var out []int
	for p := range strings.SplitSeq(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, ErrPeriod{"ksgm_week_starts", "dạng 1,8,15,22"}
		}
		out = append(out, n)
	}
	return out, nil
}

// SplitInput là đầu vào chia kỳ một đợt. From/To là ngày dương lịch (Date/ParseDate); Loc là giờ Việt Nam để tính
// hạn; KSGM chỉ dùng cho ksgm_week (nil → mặc định).
type SplitInput struct {
	PeriodType     string
	From, To       time.Time
	SeasonLabel    string
	Rule           PeriodRule
	Deadline       Deadline
	CloseAfterDays int
	Loc            *time.Location
	KSGM           KSGMStarts
}

// maxSplitDays chặn vòng lặp theo ngày (10 năm) trước khi đếm kỳ.
const maxSplitDays = 3660

// Split tự chia [From, To] thành các kỳ theo loại kỳ (FN-TI-02.02 "hệ thống tự chia các kỳ"): các ngày liền nhau
// cùng PeriodOf(...).Key thành một kỳ, kỳ đầu/cuối cắt theo khoảng của đợt. Vụ/đột xuất (và năm ở v1) là một kỳ
// [From, To], nhãn = tên vụ/đợt. Nhãn tuần: "Tuần n" của đợt (season), v1 iso "Tuần 37", v2 "Tuần 41/2026".
//
// Hạn nộp = ngày cuối kỳ + OffsetDays lúc Time; giờ chốt tự động = hạn + CloseAfterDays ngày.
func Split(in SplitInput) ([]Period, error) {
	r, from, to := in.Rule, dateOf(in.From), dateOf(in.To)
	if err := r.Validate(); err != nil {
		return nil, ErrPeriod{"period_rule", err.Error()}
	}
	if to.Before(from) {
		return nil, ErrPeriod{"to_date", "phải không trước from_date"}
	}
	if in.CloseAfterDays < 0 || in.CloseAfterDays > 60 {
		return nil, ErrPeriod{"close_after_days", "0–60 ngày"}
	}
	if in.Deadline.OffsetDays < 0 || in.Deadline.OffsetDays > 60 {
		return nil, ErrPeriod{"deadline.offset_days", "0–60 ngày"}
	}
	if _, _, err := ParseClock(in.Deadline.Time); err != nil {
		return nil, ErrPeriod{"deadline.time", err.Error()}
	}
	limit := MaxPeriods
	if in.PeriodType == PeriodDay {
		limit = MaxDayPeriods
	}
	tooMany := ErrPeriod{"to_date", fmt.Sprintf("tối đa %d kỳ mỗi đợt", limit)}
	var out []Period
	add := func(f, t time.Time, s Span) error {
		if len(out) >= limit {
			return tooMany
		}
		due, err := in.Deadline.DueAt(t, in.Loc)
		if err != nil {
			return ErrPeriod{"deadline.time", err.Error()}
		}
		out = append(out, Period{Slot: len(out) + 1, Key: s.Key, Label: s.Label, RangeLabel: rangeLabel(f, t), ISOWeek: s.WeekNo, WeekYear: s.WeekYear,
			From: f, To: t, DueAt: due, AutoCloseAt: due.AddDate(0, 0, in.CloseAfterDays)})
		return nil
	}
	if in.PeriodType == PeriodSeason || in.PeriodType == PeriodAdhoc || (in.PeriodType == PeriodYear && !r.v2()) {
		label := in.SeasonLabel
		if label == "" && in.PeriodType == PeriodYear {
			label = "Năm " + strconv.Itoa(from.Year())
		}
		if err := add(from, to, Span{Label: label}); err != nil {
			return nil, err
		}
		return out, nil
	}
	if _, err := r.PeriodOf(in.PeriodType, from, in.KSGM); err != nil {
		return nil, err
	}
	if days(from, to) > maxSplitDays {
		return nil, tooMany
	}
	for d := from; !d.After(to); {
		s, err := r.PeriodOf(in.PeriodType, d, in.KSGM)
		if err != nil {
			return nil, err
		}
		t := minT(s.To, to)
		if in.PeriodType == PeriodWeek {
			switch {
			case r.WeekNumbering == WeekNumberingSeason:
				s.Label = "Tuần " + strconv.Itoa(len(out)+1)
			case !r.v2():
				s.Label = "Tuần " + strconv.Itoa(s.WeekNo) // nhãn v1 iso không kèm năm (giữ như cũ)
			}
		}
		if err := add(d, t, s); err != nil {
			return nil, err
		}
		d = t.AddDate(0, 0, 1)
	}
	return out, nil
}

func rangeLabel(f, t time.Time) string {
	if f.Year() == t.Year() {
		return f.Format("02/01") + "–" + t.Format("02/01/2006")
	}
	return f.Format("02/01/2006") + "–" + t.Format("02/01/2006")
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
