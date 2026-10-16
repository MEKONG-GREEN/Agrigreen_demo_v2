package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Kênh nhắc (UR-TINH-08 "kênh nhắc (ứng dụng, đẩy thông báo Mobile, email)").
const (
	ChannelInApp = "in_app"
	ChannelPush  = "push"
	ChannelEmail = "email"
)

// Channels là các kênh hợp lệ.
var Channels = []string{ChannelInApp, ChannelPush, ChannelEmail}

// Phạm vi cấu hình (sổ cnts_alert_config).
const (
	ScopeForm  = "form"
	ScopeAlert = "alert"
)

// FormReminder là cấu hình nhắc nhở của một biểu (UR-TINH-08, Hình 17a).
type FormReminder struct {
	// BeforeDueHours: nhắc trước hạn bao nhiêu giờ (0 = không nhắc trước). Mặc định 24 ("trước hạn 1 ngày").
	BeforeDueHours int `json:"before_due_hours"`
	// OnOverdue: nhắc ngay khi trễ hạn (mặc định có).
	OnOverdue bool `json:"on_overdue"`
	// RepeatDaily: nhắc lặp mỗi ngày lúc RepeatAt đến khi gửi hoặc chốt kỳ (mặc định có).
	RepeatDaily bool   `json:"repeat_daily"`
	RepeatAt    string `json:"repeat_at"` // "HH:MM" giờ Việt Nam
	// LateStreak: trễ liên tiếp từ n kỳ → cảnh báo người phụ trách biểu và Lãnh đạo phòng (0 = tắt). Mặc định 2 (BM-04,
	// BM-17, BM-05), 3 (BM-01).
	LateStreak int      `json:"late_streak"`
	Channels   []string `json:"channels"`
	// Message: nội dung mẫu (≤ 500 ký tự); {bieu}, {ky}, {han} được thay khi gửi.
	Message string `json:"message"`
}

// DefaultMessage là nội dung nhắc mặc định.
const DefaultMessage = "Nhắc nộp {bieu} kỳ {ky}, hạn {han}."

// DefaultFormReminder trả cấu hình mặc định của biểu (UR-TINH-08 Quy tắc).
func DefaultFormReminder(code string) FormReminder {
	r := FormReminder{BeforeDueHours: 24, OnOverdue: true, RepeatDaily: true, RepeatAt: "08:00", LateStreak: 2,
		Channels: []string{ChannelInApp, ChannelPush}, Message: DefaultMessage}
	if code == FormBM01 {
		r.LateStreak = 3
	}
	return r
}

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Normalize kiểm và chuẩn hóa cấu hình nhắc (trả lỗi theo trường).
func (r *FormReminder) Normalize() Errors {
	var es Errors
	r.Message = strings.TrimSpace(r.Message)
	r.RepeatAt = strings.TrimSpace(r.RepeatAt)
	if r.BeforeDueHours < 0 || r.BeforeDueHours > 168 {
		es = append(es, FieldError{"before_due_hours", "0–168 giờ"})
	}
	if r.RepeatDaily && !hhmm.MatchString(r.RepeatAt) {
		es = append(es, FieldError{"repeat_at", "giờ dạng HH:MM"})
	}
	if !r.RepeatDaily && r.RepeatAt != "" && !hhmm.MatchString(r.RepeatAt) {
		es = append(es, FieldError{"repeat_at", "giờ dạng HH:MM"})
	}
	if r.LateStreak < 0 || r.LateStreak > 12 {
		es = append(es, FieldError{"late_streak", "0–12 kỳ"})
	}
	seen := []string{}
	for _, c := range r.Channels {
		if !slices.Contains(Channels, c) {
			es = append(es, FieldError{"channels", "kênh " + c + " không hỗ trợ (in_app, push, email)"})
			continue
		}
		if !slices.Contains(seen, c) {
			seen = append(seen, c)
		}
	}
	if !slices.Contains(seen, ChannelInApp) {
		seen = append([]string{ChannelInApp}, seen...) // thông báo trong ứng dụng luôn có (hộp thư là nguồn của đẩy/email)
	}
	r.Channels = seen
	if r.Message == "" {
		r.Message = DefaultMessage
	}
	if utf8.RuneCountInString(r.Message) > 500 {
		es = append(es, FieldError{"message", "tối đa 500 ký tự"})
	}
	return es
}

// Render thay {bieu}, {ky}, {han} trong nội dung mẫu.
func (r FormReminder) Render(form, period string, due time.Time, loc *time.Location) string {
	s := strings.NewReplacer("{bieu}", form, "{ky}", period, "{han}", due.In(loc).Format("15:04 02/01/2006")).Replace(r.Message)
	if rs := []rune(s); len(rs) > 500 {
		s = string(rs[:500])
	}
	return s
}

// RepeatTime trả giờ nhắc lặp trong ngày day (giờ Việt Nam).
func (r FormReminder) RepeatTime(day time.Time, loc *time.Location) (time.Time, bool) {
	if !r.RepeatDaily || !hhmm.MatchString(r.RepeatAt) {
		return time.Time{}, false
	}
	var h, m int
	if _, err := fmt.Sscanf(r.RepeatAt, "%d:%d", &h, &m); err != nil {
		return time.Time{}, false
	}
	d := day.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc), true
}

// AlertSetting là cấu hình xử lý một loại cảnh báo (UR-TINH-13: "hạn xử lý do Cấp Tỉnh đặt"; dòng 6 "mức Khẩn do Cấp
// Tỉnh cấu hình").
type AlertSetting struct {
	SLAHours int  `json:"sla_hours"`
	Urgent   bool `json:"urgent"` // chỉ loại LevelConfigurable
}

// DefaultSLAHours: hạn xử lý mặc định (giờ) — Khẩn 24 giờ, Cần chú ý 72 giờ (giả định W11.43).
func DefaultAlertSetting(kind string) AlertSetting {
	k, _ := KindOf(kind)
	if k.Level == LevelUrgent {
		return AlertSetting{SLAHours: 24}
	}
	return AlertSetting{SLAHours: 72}
}

// Normalize kiểm cấu hình xử lý.
func (a *AlertSetting) Normalize(kind string) Errors {
	var es Errors
	k, ok := KindOf(kind)
	if !ok {
		return Errors{{"kind", "loại cảnh báo không có"}}
	}
	if a.SLAHours < 1 || a.SLAHours > 720 {
		es = append(es, FieldError{"sla_hours", "1–720 giờ"})
	}
	if a.Urgent && !k.LevelConfigurable {
		es = append(es, FieldError{"urgent", "mức của loại này cố định theo Mục III.5"})
	}
	return es
}

// LevelFor trả mức hiệu lực của loại theo cấu hình.
func LevelFor(kind string, s AlertSetting) string {
	k, _ := KindOf(kind)
	if k.LevelConfigurable && s.Urgent {
		return LevelUrgent
	}
	if k.Level == "" {
		return LevelAttention
	}
	return k.Level
}

// FieldError là lỗi của một trường.
type FieldError struct{ Field, Reason string }

// Errors là danh sách lỗi theo trường.
type Errors []FieldError

func (es Errors) Error() string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, e.Field+": "+e.Reason)
	}
	return strings.Join(parts, "; ")
}
