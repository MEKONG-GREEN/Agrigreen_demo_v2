package port

// Lịch báo giá, giờ khóa, nhắc, ngưỡng (W11.74 — BRD Báo giá v1.3 FN-AD-04.04…06, FN-XA-03.04; ADR-0049 mục 2, 4).

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ScheduleDefaults là cấu hình lịch + ngưỡng MẶC ĐỊNH của một bên dùng (Profile.Schedule) — dùng khi tenant chưa ghi
// phiên bản nào ở pf_price.calendar_rule / pf_price.alert_threshold. Giờ dạng "HH:MM" (giờ VN). Trường rỗng/zero
// lấy giá trị BRD: T2–T6 (Q-086 tạm), khóa 12:00, hạn phản hồi 13:30, nhắc 07:30 và 11:00 (giả định), cả hai kênh,
// ngưỡng 30 %, N = 7, đi ngang 1 %, phiếu điều tra 40 %.
type ScheduleDefaults struct {
	Weekdays     []time.Weekday
	LockAt       string
	RespondBy    string
	Remind1      string
	Remind2      string
	Channels     []string // ChannelInApp, ChannelEmail
	Template     string
	ThresholdPct decimal.Decimal
	LookbackDays int
	FlatPct      decimal.Decimal
	SurveyPct    decimal.Decimal
	PreviewDays  int // số ngày của "Lịch khóa nhập sắp tới" (mặc định 30)
}

// Kênh nhắc (FN-AD-04.05 B2).
const (
	ChannelInApp = "in_app"
	ChannelEmail = "email"
)

// ReminderTarget là một nhóm người nhận nhắc nhập giá: một xã báo giá (tên dùng cho biến {xã}) và tài khoản CTV.
type ReminderTarget struct {
	CommuneCode   string
	CommuneName   string
	RecipientRefs []uuid.UUID
}

// ReminderTargets cho job nhắc biết NHẮC AI (FN-XA-03.04). round = 1 | 2; lần 2 chỉ xã chưa gửi (BR-01).
// W11.72 (xã báo giá + phân công) và W11.75 (phiếu) cài đặt; nil → nhắc chủ đề tới mọi cán bộ xã (vai kn_officer) của
// tenant, một thông báo (đủ cho vận hành thử trước khi có danh sách xã).
type ReminderTargets interface {
	Targets(ctx context.Context, tenantID uuid.UUID, profile string, day time.Time, round int) ([]ReminderTarget, error)
}

// DayPlan là kế hoạch một ngày báo giá cho bên dùng (port.Calendar).
type DayPlan struct {
	Day       time.Time // 00:00 UTC của ngày VN
	Kind      string    // regular | extra | holiday | off
	Quote     bool      // là kỳ báo giá
	Reason    string    // lý do ngoại lệ
	LockAt    time.Time // giờ khóa (UTC); zero nếu không phải kỳ báo giá
	RespondBy time.Time // hạn phản hồi xác minh
}

// Locked cho biết kỳ của ngày đã khóa tại now (Xã không còn sửa) — W11.75 chặn ghi bằng hàm này, không đợi job.
func (p DayPlan) Locked(now time.Time) bool { return p.Quote && !now.Before(p.LockAt) }

// Calendar là lịch báo giá của tenant người gọi (tenant từ ctx). profile rỗng = bên dùng đầu tiên.
type Calendar interface {
	Day(ctx context.Context, profile string, day time.Time) (DayPlan, error)
}

// Thresholds là ngưỡng cảnh báo đang hiệu lực (FN-AD-04.06): W11.75 đọc khi Xã gửi phiếu.
type Thresholds interface {
	// Threshold trả ngưỡng ±% áp cho mặt hàng item thuộc nhóm group (mặt hàng → nhóm → mặc định) và phạm vi đã dùng.
	Threshold(ctx context.Context, profile, group, item string) (decimal.Decimal, string, error)
	// AlertParams trả N, ngưỡng đi ngang, ngưỡng phiếu điều tra.
	AlertParams(ctx context.Context, profile string) (lookbackDays int, flatPct, surveyPct decimal.Decimal, err error)
}

// JobCalendar là tên job lịch báo giá của cmd/worker (-jobs): mỗi phút, ghi mốc khóa 12:00 (±1 phút) và gửi nhắc.
const JobCalendar = "price_calendar"

// Sự kiện module phát ra.
const (
	EventProducerName   = "pf/price"
	EventReminderIssued = "pf.price.reminder.issued"
)

// ReminderIssuedV1 là payload của pf.price.reminder.issued: một lần nhắc nhập giá (job lịch, hoặc "Gửi thử" — test).
// pf/notify dựng thông báo: recipient_refs có → mỗi người một thông báo; rỗng → thông báo chủ đề tới cán bộ xã
// (communes rỗng = toàn tenant). Không chứa dữ liệu cá nhân (tiêu đề/nội dung từ mẫu do Cấp Tỉnh soạn).
//
//ag:event pf.price.reminder.issued v1
type ReminderIssuedV1 struct {
	Profile       string      `json:"profile"`
	Day           string      `json:"day"`
	Round         int         `json:"round"`
	LockAt        string      `json:"lock_at"`
	Title         string      `json:"title"`
	Body          string      `json:"body"`
	Communes      []string    `json:"communes"`
	RecipientRefs []uuid.UUID `json:"recipient_refs"`
	InApp         bool        `json:"in_app"`
	Email         bool        `json:"email"`
	Test          bool        `json:"test"`
}

// Lỗi lịch/ngưỡng (docs/errors.md, mục pf/price).
var (
	ErrRespondBeforeLock = problem.New(http.StatusUnprocessableEntity, "price.respond_before_lock",
		"Hạn phản hồi xác minh phải sau giờ khóa")
	ErrRemindOrder = problem.New(http.StatusUnprocessableEntity, "price.remind_order",
		"Giờ nhắc lần 1 phải trước lần 2, lần 2 phải trước giờ khóa")
	ErrTemplateVar = problem.New(http.StatusUnprocessableEntity, "price.template_var",
		"Nội dung mẫu có biến không hỗ trợ — chỉ {ngày}, {xã}, {giờ khóa}")
	ErrValidFrom = problem.New(http.StatusUnprocessableEntity, "price.valid_from",
		"Ngày áp dụng không được trong quá khứ; đổi lịch trong ngày báo giá chỉ áp dụng từ ngày kế tiếp")
	ErrExceptionMismatch = problem.New(http.StatusUnprocessableEntity, "price.exception_mismatch",
		"Ngày nghỉ phải là ngày báo giá theo lịch; ngày bổ sung phải là ngày ngoài lịch")
	ErrExceptionExists = problem.New(http.StatusConflict, "price.exception_exists",
		"Ngày này đã có ngoại lệ — xóa ngoại lệ cũ trước")
	ErrDefaultThreshold = problem.New(http.StatusUnprocessableEntity, "price.default_threshold",
		"Không xóa được ngưỡng mặc định")
	ErrThresholdNotFound = problem.New(http.StatusNotFound, "price.threshold_not_found", "Không có ngưỡng này")
	ErrExceptionNotFound = problem.New(http.StatusNotFound, "price.exception_not_found", "Không có ngoại lệ cho ngày này")
)
