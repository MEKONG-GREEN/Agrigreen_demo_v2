package domain

// # Lịch tuần theo phòng ban (D8 — quyết định CEO 13/10/2026)
//
// Mỗi phòng ban giữ lịch tuần RIÊNG; hệ thống tự quy đổi số liệu cho KHTC (gói PFPUB). Gói này cung cấp lịch:
//
//   - Lịch = (calendar_id, calendar_version) + PeriodRule. calendar_id = mã phòng ban (party/port.Dept*), vd
//     "chan_nuoi_thuy_san"; calendar_version là số nguyên tăng dần dùng chung một dãy cho mọi phòng:
//     1 = quy tắc v1-tam (thứ Hai–Chủ nhật, trước 04/11/2026), 2 = quy tắc v2 mặc định (Thứ Tư–Thứ Ba, KHTC v2.2
//     GD-05), ≥ 3 = lịch riêng của phòng (3 = CNTS Thứ Ba–Thứ Hai). Khóa kỳ của PFPUB:
//     (calendar_id, calendar_version, native_key) với native_key = Span.Key ("2026-W39", "2026-10", …).
//   - Quy tắc v2 mới ghi kèm danh tính lịch trong tag (cột period_rule, pf_forms.period_spec.rule):
//     "kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3". Tag cũ không có
//     "cal=" → CalendarOf suy ra (phòng của kỳ, 1 nếu v1, 2 nếu v2) — không cần nạp ngược dữ liệu.
//   - Chọn lịch của phòng tại ngày đầu đợt/kỳ (ResolveCalendar): tham số theo phòng
//     "extension.report_period_rule:<mã phòng>" → lịch nạp sẵn trong mã (BuiltinCalendars, hiệu lực ngay) →
//     tham số chung "extension.report_period_rule" → LegacyRule (v1).
//
// BRD (scratchpad analysis/src, đọc 13/10/2026): CNTS v3.7 — BM-04/BM-17 "tuần từ thứ Ba đến thứ Hai tuần sau (ví dụ
// tuần 39: 22/09–28/09/2026); hạn nộp 12:00 thứ Ba", BM-05 tuần làm việc thứ Hai–thứ Sáu (thẻ riêng
// CNTSRuleWorkWeek, KHÔNG thuộc lịch tuần), KSGM 4 tuần/tháng. KHTC v2.2 — tuần báo cáo Thứ Tư–Thứ Ba, 52/53 tuần,
// tháng/quý/năm theo Thứ Ba cuối (BR-DL-01/06). Trồng trọt Final — báo cáo "hằng tuần" không nêu ngày bắt đầu →
// mặc định v2. Báo giá v1.3 — "kỳ tuần/tháng" không nêu ngày bắt đầu → mặc định v2. HCTH v3.0 — không có kỳ báo cáo
// tuần (lịch làm việc thứ Hai–thứ Sáu là nghiệp vụ khác) → mặc định v2.
//
// API cho PFPUB (thuần, không DB):
//
//	ResolveCalendar(dept, deptParam, globalParam string) Calendar // lịch dùng khi PHÁT HÀNH kỳ mới
//	CalendarOf(dept, storedTag string) (Calendar, error)          // lịch của kỳ ĐÃ LƯU (period_rule)
//	(c Calendar) Ref() string                                     // "chan_nuoi_thuy_san:v3"
//	CalendarRef(id, v) / ParseCalendarRef(s)
//	SpanOfKey(r PeriodRule, kind, key string, ksgm KSGMStarts) (Span, error) // native_key → khoảng ngày
//	SameGrid(a, b PeriodRule) bool  // cùng khóa + khoảng ngày mọi loại kỳ → không cần quy đổi
//	SameWeeks(a, b PeriodRule) bool // cùng ranh giới tuần (số tuần có thể khác)

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mã phòng ban có lịch đã biết (khớp party/port.Dept*; domain thuần nên không import party).
const (
	DeptTrongTrot       = "trong_trot"
	DeptChanNuoiThuySan = "chan_nuoi_thuy_san"
	DeptKeHoachTaiChinh = "ke_hoach_tai_chinh"
)

// Phiên bản lịch (một dãy dùng chung cho mọi phòng — xem đầu tệp).
const (
	CalendarVersionLegacy  = 1 // tag "kn.period_rule/v1-tam…"
	CalendarVersionDefault = 2 // tag v2 không có "cal=" (Thứ Tư–Thứ Ba mặc định)
	CalendarVersionCNTS    = 3 // CNTS Thứ Ba–Thứ Hai (D8)
)

// Nguồn của lịch đã chọn (hiển thị ở màn phát hành).
const (
	CalendarSourceDepartmentParam = "department_param" // tham số extension.report_period_rule:<phòng>
	CalendarSourceBuiltin         = "builtin"          // BuiltinCalendars
	CalendarSourceGlobalParam     = "global_param"     // tham số extension.report_period_rule
	CalendarSourceLegacy          = "legacy"           // chưa có phiên bản tham số → v1
)

// CNTSRule là lịch tuần của Phòng Chăn nuôi - Thủy sản (D8, BRD CNTS v3.7): Thứ Ba–Thứ Hai, tuần 1 = tuần chứa 01/01
// (năm 52/53 tuần), tháng/quý/năm theo ngày cuối tuần (Thứ Hai — "tuần vắt quý tính theo ngày cuối tuần").
var CNTSRule = PeriodRule{Version: RuleVersionV2, WeekStart: time.Tuesday, WeekNumbering: WeekNumberingYear, MonthAlign: MonthAlignWeekEnd,
	CalendarID: DeptChanNuoiThuySan, CalendarVersion: CalendarVersionCNTS}

// BuiltinCalendars là lịch riêng nạp sẵn trong mã, có hiệu lực NGAY với mọi ngày (CNTS chưa có số liệu thật nên không
// cần ngày hiệu lực); tham số theo phòng (nếu có phiên bản hiệu lực) đè lên. Phòng không có ở đây theo tham số chung
// (KHTC, Trồng trọt, Báo giá, HCTH: v2 Thứ Tư–Thứ Ba từ 04/11/2026).
var BuiltinCalendars = map[string]PeriodRule{DeptChanNuoiThuySan: CNTSRule}

// Calendar là lịch tuần của một phòng tại một thời điểm.
type Calendar struct {
	ID      string     // calendar_id = mã phòng ban
	Version int        // calendar_version
	Rule    PeriodRule // v2: Rule.CalendarID/Version = ID/Version (Tag() mang "cal="); v1: tag cũ
	Source  string     // CalendarSource* (chỉ ResolveCalendar điền)
}

// Ref trả "<id>:v<n>".
func (c Calendar) Ref() string { return CalendarRef(c.ID, c.Version) }

// CalendarRef trả "<id>:v<n>".
func CalendarRef(id string, v int) string { return id + ":v" + strconv.Itoa(v) }

var calendarIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func validCalendarID(s string) bool { return calendarIDRe.MatchString(s) }

// ParseCalendarRef đọc "<id>:v<n>".
func ParseCalendarRef(s string) (string, int, error) {
	id, v, ok := strings.Cut(strings.TrimSpace(s), ":v")
	n, err := strconv.Atoi(v)
	if !ok || err != nil || !validCalendarID(id) || n < 1 || n > 9999 {
		return "", 0, errors.New("cal: <mã phòng>:v<n> (mã [a-z][a-z0-9_]{1,39}, n 1–9999)")
	}
	return id, n, nil
}

// stamp gắn danh tính lịch của phòng dept vào quy tắc r: v1 → phiên bản 1 (tag cũ); v2 chưa mang cal → phiên bản 2.
func stamp(dept string, r PeriodRule, src string) Calendar {
	if !r.v2() {
		return Calendar{ID: dept, Version: CalendarVersionLegacy, Rule: r, Source: src}
	}
	if r.CalendarID == "" && validCalendarID(dept) {
		r.CalendarID, r.CalendarVersion = dept, CalendarVersionDefault
	}
	if r.CalendarID == "" {
		return Calendar{ID: dept, Version: CalendarVersionDefault, Rule: r, Source: src}
	}
	return Calendar{ID: r.CalendarID, Version: r.CalendarVersion, Rule: r, Source: src}
}

// ResolveCalendar chọn lịch của phòng dept từ giá trị tham số đã tra tại 00:00 giờ VN ngày đầu đợt/kỳ:
// deptParam = "extension.report_period_rule:<dept>", globalParam = "extension.report_period_rule" ("" = chưa có
// phiên bản hiệu lực). Thứ tự: tham số theo phòng hợp lệ (cal vắng hoặc đúng phòng) → BuiltinCalendars → tham số
// chung (bỏ "cal=" nếu có) → LegacyRule. Quy tắc v2 trả về luôn mang cal (Tag() ghi vào kỳ).
func ResolveCalendar(dept, deptParam, globalParam string) Calendar {
	if r, err := ParseRule(deptParam); err == nil && (r.CalendarID == "" || r.CalendarID == dept) {
		return stamp(dept, r, CalendarSourceDepartmentParam)
	}
	if r, ok := BuiltinCalendars[dept]; ok {
		return stamp(dept, r, CalendarSourceBuiltin)
	}
	if r, err := ParseRule(globalParam); err == nil {
		return stamp(dept, r.withoutCalendar(), CalendarSourceGlobalParam)
	}
	return stamp(dept, LegacyRule, CalendarSourceLegacy)
}

// ErrNotCalendar: tag không phải quy tắc lịch kỳ (vd tuần làm việc BM-05 "cnts.work_week/v1") — kỳ không quy đổi.
var ErrNotCalendar = errors.New("kỳ không theo lịch tuần của phòng")

// CalendarOf trả lịch của một kỳ đã lưu: dept = department_code của kỳ/đợt, storedTag = period_rule đã ghi. Tag có
// "cal=" → lấy nguyên; tag cũ → (dept, 1) với v1, (dept, 2) với v2. Tag lạ → ErrNotCalendar.
func CalendarOf(dept, storedTag string) (Calendar, error) {
	r, err := ParseRule(storedTag)
	if err != nil {
		return Calendar{}, fmt.Errorf("%w: %w", ErrNotCalendar, err)
	}
	if r.CalendarID != "" {
		return Calendar{ID: r.CalendarID, Version: r.CalendarVersion, Rule: r}, nil
	}
	c := stamp(dept, r, "")
	c.Rule = r // giữ tag đúng như đã lưu
	return c, nil
}

// SameGrid: hai quy tắc chia mọi loại kỳ thành cùng khóa + cùng khoảng ngày (bỏ qua danh tính lịch) → số liệu dùng
// thẳng, không quy đổi.
func SameGrid(a, b PeriodRule) bool { return a.withoutCalendar() == b.withoutCalendar() }

// SameWeeks: hai quy tắc có cùng ranh giới tuần (ngày bắt đầu tuần), số tuần/khóa có thể khác.
func SameWeeks(a, b PeriodRule) bool { return a.weekStart() == b.weekStart() }

var (
	weekKeyRe    = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)
	monthKeyRe   = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
	quarterKeyRe = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)
	yearKeyRe    = regexp.MustCompile(`^(\d{4})$`)
	ksgmKeyRe    = regexp.MustCompile(`^(\d{4})-(\d{2})-K([1-4])$`)
)

// SpanOfKey trả kỳ đầy đủ (khoảng ngày) của khóa native_key theo quy tắc r — ngược của PeriodOf(...).Key. kind là
// loại kỳ (Period*); ksgm chỉ dùng cho tuần KSGM (nil → 1, 8, 15, 22; truyền ranh giới đã tra lúc phát hành).
func SpanOfKey(r PeriodRule, kind, key string, ksgm KSGMStarts) (Span, error) {
	bad := ErrPeriod{"period_key", "khóa " + key + " không hợp lệ với loại kỳ " + kind}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	var (
		d time.Time
		m []string
	)
	switch kind {
	case PeriodWeek:
		if m = weekKeyRe.FindStringSubmatch(key); m == nil {
			return Span{}, bad
		}
		w, err := r.Week(atoi(m[1]), atoi(m[2]))
		if err != nil {
			return Span{}, err
		}
		d = w.From
	case PeriodDay:
		t, err := ParseDate(key)
		if err != nil {
			return Span{}, bad
		}
		d = t
	case PeriodMonth:
		if m = monthKeyRe.FindStringSubmatch(key); m == nil || atoi(m[2]) < 1 || atoi(m[2]) > 12 {
			return Span{}, bad
		}
		d = Date(atoi(m[1]), time.Month(atoi(m[2])), 15) // giữa tháng: tuần chứa ngày 15 luôn kết thúc trong tháng
	case PeriodQuarter:
		if m = quarterKeyRe.FindStringSubmatch(key); m == nil {
			return Span{}, bad
		}
		d = Date(atoi(m[1]), time.Month(atoi(m[2])*3-1), 15)
	case PeriodYear:
		if m = yearKeyRe.FindStringSubmatch(key); m == nil {
			return Span{}, bad
		}
		d = Date(atoi(m[1]), 7, 1)
	case PeriodKSGMWeek:
		if m = ksgmKeyRe.FindStringSubmatch(key); m == nil || atoi(m[2]) < 1 || atoi(m[2]) > 12 {
			return Span{}, bad
		}
		y, mo := atoi(m[1]), time.Month(atoi(m[2]))
		var starts []int
		if ksgm != nil {
			starts = ksgm(y, mo)
		}
		ws, err := KSGMWeeks(y, mo, starts)
		if err != nil {
			return Span{}, err
		}
		d = ws[atoi(m[3])-1].From
	default:
		return Span{}, bad
	}
	s, err := r.PeriodOf(kind, d, ksgm)
	if err != nil {
		return Span{}, err
	}
	if s.Key != key {
		return Span{}, bad
	}
	return s, nil
}
