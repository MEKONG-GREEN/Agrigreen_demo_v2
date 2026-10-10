package domain

// Lịch báo giá, giờ khóa, hạn phản hồi, nhắc nhập giá (W11.74 — BRD Báo giá v1.3 FN-AD-04.04, FN-AD-04.05,
// FN-XA-03.04; đối chiếu Phụ lục D BG4, Q-086). Quy tắc thuần: ngày truyền vào, giờ nghiệp vụ Asia/Ho_Chi_Minh.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// TimeOfDay là giờ trong ngày tính bằng phút từ 00:00 (giờ VN), 0…1439.
type TimeOfDay int

// ParseTimeOfDay đọc "HH:MM" (24 giờ).
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%w: giờ %q phải dạng HH:MM", ErrInvalid, s)
	}
	return TimeOfDay(t.Hour()*60 + t.Minute()), nil
}

// MustTimeOfDay là ParseTimeOfDay cho hằng (panic khi sai — chỉ dùng với giá trị cố định trong mã).
func MustTimeOfDay(s string) TimeOfDay {
	t, err := ParseTimeOfDay(s)
	if err != nil {
		panic(err)
	}
	return t
}

// String trả "HH:MM".
func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60) }

// Valid cho biết giờ nằm trong ngày.
func (t TimeOfDay) Valid() bool { return t >= 0 && t < 24*60 }

// On trả thời điểm (UTC) của giờ t trong ngày nghiệp vụ day (00:00 UTC của ngày VN — BusinessDay).
func (t TimeOfDay) On(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), int(t)/60, int(t)%60, 0, 0, clock.Zone()).UTC()
}

// Weekdays là tập ngày trong tuần, bit (1 << time.Weekday): Chủ nhật = bit 0 … Thứ Bảy = bit 6. Khớp cột smallint
// weekdays của pf_price.calendar_rule (pf 000146).
type Weekdays uint8

// WeekdaysOf dựng tập từ danh sách.
func WeekdaysOf(ds ...time.Weekday) Weekdays {
	var w Weekdays
	for _, d := range ds {
		if d >= time.Sunday && d <= time.Saturday {
			w |= 1 << uint(d)
		}
	}
	return w
}

// MonFri là lịch mặc định Thứ Hai – Thứ Sáu (BRD FN-AD-04.04 BR-04 "mẫu T2–T6", chờ Q-086).
var MonFri = WeekdaysOf(time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

// Has cho biết d thuộc tập.
func (w Weekdays) Has(d time.Weekday) bool { return w&(1<<uint(d)) != 0 }

// List trả các ngày theo thứ tự Thứ Hai → Chủ nhật.
func (w Weekdays) List() []time.Weekday {
	var out []time.Weekday
	for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday} {
		if w.Has(d) {
			out = append(out, d)
		}
	}
	return out
}

// Schedule là một phiên bản cấu hình lịch báo giá của một bên dùng: ngày báo giá trong tuần, giờ khóa, hạn phản hồi
// xác minh, hai giờ nhắc, kênh và nội dung mẫu nhắc. Có hiệu lực từ ValidFrom (ngày báo giá đầu tiên).
type Schedule struct {
	Weekdays  Weekdays
	LockAt    TimeOfDay // giờ khóa nhập (mặc định 12:00)
	RespondBy TimeOfDay // hạn phản hồi xác minh (mặc định 13:30)
	Remind1   TimeOfDay // nhắc lần 1 — buổi sáng ngày báo giá
	Remind2   TimeOfDay // nhắc lần 2 — trước giờ khóa, chỉ xã chưa gửi
	InApp     bool      // kênh trong ứng dụng (chuông)
	Email     bool      // kênh email
	Template  string    // nội dung mẫu, biến {ngày} {xã} {giờ khóa}
	ValidFrom time.Time // ngày nghiệp vụ; zero = cấu hình mặc định của Profile (chưa có phiên bản)
	Version   int64     // seq của chuỗi; 0 = mặc định của Profile
}

// DefaultReminderTemplate là nội dung mẫu nhắc mặc định (giả định — BRD chỉ nêu biến).
const DefaultReminderTemplate = "Nhắc nhập giá ngày {ngày} cho {xã}: vui lòng gửi báo giá trước {giờ khóa}."

// TemplateVars là biến được hỗ trợ trong nội dung mẫu nhắc (FN-AD-04.05 BR-02).
var TemplateVars = []string{"{ngày}", "{xã}", "{giờ khóa}"}

var reTemplateVar = regexp.MustCompile(`\{[^{}]*\}`)

// MaxTemplate là độ dài tối đa nội dung mẫu (khớp CHECK pf 000146).
const MaxTemplate = 500

// UnknownVars trả các biến không được hỗ trợ trong nội dung mẫu (theo thứ tự xuất hiện, không trùng).
func UnknownVars(tpl string) []string {
	var out []string
	for _, v := range reTemplateVar.FindAllString(tpl, -1) {
		ok := false
		for _, k := range TemplateVars {
			if v == k {
				ok = true
				break
			}
		}
		if !ok && !calendarHas(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func calendarHas(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Lỗi quy tắc lịch (app đổi sang mã problem riêng).
var (
	// ErrRespondBeforeLock: hạn phản hồi không sau giờ khóa (FN-AD-04.04 AC-05).
	ErrRespondBeforeLock = errors.New("price: hạn phản hồi phải sau giờ khóa")
	// ErrRemindOrder: nhắc lần 2 không trước giờ khóa, hoặc lần 1 không trước lần 2 (FN-AD-04.05 AC-01).
	ErrRemindOrder = errors.New("price: giờ nhắc lần 1 phải trước lần 2, lần 2 phải trước giờ khóa")
	// ErrTemplateVar: nội dung mẫu có biến không hỗ trợ (FN-AD-04.05 luồng ngoại lệ).
	ErrTemplateVar = errors.New("price: nội dung mẫu có biến không hỗ trợ")
)

// ValidateSchedule kiểm một phiên bản lịch (cùng ràng buộc CHECK của pf 000146).
func ValidateSchedule(s Schedule) error {
	if s.Weekdays == 0 || s.Weekdays > 127 {
		return fmt.Errorf("%w: phải chọn ít nhất một ngày báo giá trong tuần", ErrInvalid)
	}
	for _, t := range []TimeOfDay{s.LockAt, s.RespondBy, s.Remind1, s.Remind2} {
		if !t.Valid() {
			return fmt.Errorf("%w: giờ %d ngoài ngày", ErrInvalid, int(t))
		}
	}
	if s.RespondBy <= s.LockAt {
		return fmt.Errorf("%w (giờ khóa %s, hạn phản hồi %s)", ErrRespondBeforeLock, s.LockAt, s.RespondBy)
	}
	if s.Remind1 >= s.Remind2 || s.Remind2 >= s.LockAt {
		return fmt.Errorf("%w (lần 1 %s, lần 2 %s, giờ khóa %s)", ErrRemindOrder, s.Remind1, s.Remind2, s.LockAt)
	}
	if !s.InApp && !s.Email {
		return fmt.Errorf("%w: phải chọn ít nhất một kênh nhắc", ErrInvalid)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(s.Template)); n < 1 || n > MaxTemplate {
		return fmt.Errorf("%w: nội dung mẫu dài 1–%d ký tự", ErrInvalid, MaxTemplate)
	}
	if bad := UnknownVars(s.Template); len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrTemplateVar, strings.Join(bad, ", "))
	}
	return nil
}

// ScheduleAt chọn phiên bản có hiệu lực tại ngày day: ValidFrom lớn nhất ≤ day (cùng ngày lấy Version lớn nhất);
// không có → fallback (mặc định của Profile).
func ScheduleAt(versions []Schedule, day time.Time, fallback Schedule) Schedule {
	best, found := fallback, false
	for _, v := range versions {
		if v.ValidFrom.After(day) {
			continue
		}
		if !found || v.ValidFrom.After(best.ValidFrom) || (v.ValidFrom.Equal(best.ValidFrom) && v.Version > best.Version) {
			best, found = v, true
		}
	}
	return best
}

// Loại ngày ngoại lệ (FN-AD-04.04 B3).
const (
	ExceptionHoliday = "holiday" // ngày nghỉ: không sinh kỳ báo giá, không nhắc, không tính phiếu phải nộp
	ExceptionExtra   = "extra"   // ngày báo giá bổ sung (ngoài ngày trong tuần của lịch)
)

// Exception là một ngày ngoại lệ đang hiệu lực.
type Exception struct {
	Day     time.Time
	Kind    string
	Reason  string
	Version int64
}

// MaxReason là độ dài tối đa lý do ngoại lệ (khớp CHECK pf 000146).
const MaxReason = 300

// ValidateException kiểm một ngày ngoại lệ (chưa xét lịch — xem CheckException).
func ValidateException(e Exception) error {
	if e.Kind != ExceptionHoliday && e.Kind != ExceptionExtra {
		return fmt.Errorf("%w: loại ngoại lệ %q", ErrInvalid, e.Kind)
	}
	if e.Day.IsZero() {
		return fmt.Errorf("%w: thiếu ngày", ErrInvalid)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(e.Reason)); n < 1 || n > MaxReason {
		return fmt.Errorf("%w: lý do dài 1–%d ký tự", ErrInvalid, MaxReason)
	}
	return nil
}

// ErrExceptionMismatch: ngày nghỉ rơi vào ngày vốn không báo giá, hoặc ngày bổ sung rơi vào ngày vốn đã báo giá.
var ErrExceptionMismatch = errors.New("price: ngày ngoại lệ không khớp lịch")

// CheckException kiểm ngoại lệ với lịch có hiệu lực tại ngày đó.
func CheckException(e Exception, s Schedule) error {
	regular := s.Weekdays.Has(e.Day.Weekday())
	switch {
	case e.Kind == ExceptionHoliday && !regular:
		return fmt.Errorf("%w: %s vốn không phải ngày báo giá", ErrExceptionMismatch, e.Day.Format(time.DateOnly))
	case e.Kind == ExceptionExtra && regular:
		return fmt.Errorf("%w: %s đã là ngày báo giá", ErrExceptionMismatch, e.Day.Format(time.DateOnly))
	}
	return nil
}

// Loại ngày của kế hoạch.
const (
	DayRegular = "regular" // ngày báo giá theo lịch tuần
	DayExtra   = "extra"   // ngày báo giá bổ sung
	DayHoliday = "holiday" // ngày nghỉ theo lịch ngoại lệ
	DayOff     = "off"     // ngoài lịch
)

// DayPlan là kế hoạch của một ngày: có phải ngày báo giá (kỳ báo giá) không và các mốc giờ tuyệt đối (UTC).
type DayPlan struct {
	Day       time.Time
	Kind      string
	Quote     bool
	Reason    string
	LockAt    time.Time
	RespondBy time.Time
	Remind1   time.Time
	Remind2   time.Time
	Schedule  Schedule
}

// Plan dựng kế hoạch ngày day từ lịch có hiệu lực và ngoại lệ của ngày (nil nếu không có).
func Plan(day time.Time, s Schedule, exc *Exception) DayPlan {
	p := DayPlan{Day: day, Kind: DayOff, Schedule: s}
	if s.Weekdays.Has(day.Weekday()) {
		p.Kind = DayRegular
	}
	if exc != nil {
		switch {
		case exc.Kind == ExceptionHoliday && p.Kind == DayRegular:
			p.Kind, p.Reason = DayHoliday, exc.Reason
		case exc.Kind == ExceptionExtra && p.Kind == DayOff:
			p.Kind, p.Reason = DayExtra, exc.Reason
		}
	}
	p.Quote = p.Kind == DayRegular || p.Kind == DayExtra
	if p.Quote {
		p.LockAt, p.RespondBy = s.LockAt.On(day), s.RespondBy.On(day)
		p.Remind1, p.Remind2 = s.Remind1.On(day), s.Remind2.On(day)
	}
	return p
}

// Locked cho biết kỳ báo giá của ngày đã khóa tại now (FN-AD-04.04: sau giờ khóa Xã không còn quyền sửa). Ngày không
// báo giá không có kỳ — trả false (không có gì để nhập).
func (p DayPlan) Locked(now time.Time) bool { return p.Quote && !now.Before(p.LockAt) }

// Loại lần chạy của job lịch (pf_price.calendar_run.kind).
const (
	RunLock    = "lock"
	RunRemind1 = "remind1"
	RunRemind2 = "remind2"
)

// DueRuns trả các lần chạy đã đến hạn tại now của ngày (chưa xét đã chạy hay chưa): khóa khi now ≥ giờ khóa; nhắc
// khi giờ nhắc ≤ now < giờ khóa (lỡ giờ nhắc mà đã khóa thì bỏ — nhắc sau giờ khóa vô nghĩa). Ngày không báo giá
// không có lần chạy (FN-XA-03.04 BR-02).
func (p DayPlan) DueRuns(now time.Time) []string {
	if !p.Quote {
		return nil
	}
	if !now.Before(p.LockAt) {
		return []string{RunLock}
	}
	var out []string
	if !now.Before(p.Remind1) {
		out = append(out, RunRemind1)
	}
	if !now.Before(p.Remind2) {
		out = append(out, RunRemind2)
	}
	return out
}

// RenderReminder thay biến của nội dung mẫu: {ngày} → dd/mm/yyyy, {xã} → tên xã (rỗng → "xã"), {giờ khóa} → HH:MM.
func RenderReminder(tpl string, day time.Time, commune string, lockAt TimeOfDay) string {
	if strings.TrimSpace(commune) == "" {
		commune = "xã"
	}
	r := strings.NewReplacer("{ngày}", day.Format("02/01/2006"), "{xã}", commune, "{giờ khóa}", lockAt.String())
	return r.Replace(tpl)
}

// EarliestValidFrom trả ngày áp dụng sớm nhất của một thay đổi lịch tại now: hôm nay nếu hôm nay không phải ngày báo
// giá, ngày mai nếu hôm nay là ngày báo giá (FN-AD-04.04: "đổi giờ khóa trong ngày báo giá hiện tại chỉ áp dụng từ
// ngày báo giá kế tiếp"; FN-AD-04.05 BR-03).
func EarliestValidFrom(today DayPlan) time.Time {
	if today.Quote {
		return today.Day.AddDate(0, 0, 1)
	}
	return today.Day
}
