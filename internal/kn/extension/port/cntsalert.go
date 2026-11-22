package port

import "github.com/google/uuid"

// W11.43 (D4 CN3, BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5): quy tắc liên biểu + 12 cảnh báo, vòng đời
// cảnh báo (nhiệm vụ SLA của cap/task cài ở pf/extension), nhắc nộp biểu kỳ theo cấu hình của biểu.

// CNTSAlertConsumer là consumer của worker sinh cảnh báo CNTS sau commit: bài nộp biểu kỳ (pf.forms.submission.recorded,
// profile kn_cnts) và ổ dịch mới (pf.extension.outbreak.reported).
const CNTSAlertConsumer = "pf.extension.cnts_alert"

// EventCNTSAlertChanged: cảnh báo CNTS mới / được giao / quá hạn xử lý — pf/notify báo người nhận.
const EventCNTSAlertChanged = "pf.extension.cnts_alert.changed"

// Hành động của pf.extension.cnts_alert.changed.
const (
	CNTSAlertRaised   = "raised"
	CNTSAlertAssigned = "assigned"
	CNTSAlertOverdue  = "overdue"
)

// CNTSAlertChangedV1 là payload của pf.extension.cnts_alert.changed. Không chứa dữ liệu cá nhân, không số liệu chi tiết
// (consumer đọc qua API có phân quyền); title là tóm tắt do hệ thống dựng (loại, biểu, kỳ, đối tượng).
//
//ag:event pf.extension.cnts_alert.changed v1
type CNTSAlertChangedV1 struct {
	AlertRef      uuid.UUID   `json:"alert_ref"`
	Action        string      `json:"action"`
	Kind          string      `json:"kind"`
	Level         string      `json:"level"`
	Title         string      `json:"title"`
	CommuneCode   *string     `json:"commune_code"`
	ProvinceCode  *string     `json:"province_code"`
	TemplateCode  *string     `json:"template_code"`
	PeriodKey     *string     `json:"period_key"`
	DueAt         string      `json:"due_at"`
	RecipientRefs []uuid.UUID `json:"recipient_refs"`
	Department    string      `json:"department"`
	EventTime     string      `json:"event_time"`
}

// EventCNTSFormReminded: nhắc nộp biểu kỳ CNTS (trước hạn, khi trễ, nhắc lặp) theo cấu hình nhắc của biểu.
const EventCNTSFormReminded = "pf.extension.cnts_form.reminded"

// Lượt nhắc.
const (
	CNTSReminderBefore  = "before"
	CNTSReminderOverdue = "overdue"
	CNTSReminderRepeat  = "repeat"
)

// CNTSFormRemindedV1 là payload của pf.extension.cnts_form.reminded (một sự kiện mỗi kỳ × đơn vị × lượt).
//
//ag:event pf.extension.cnts_form.reminded v1
type CNTSFormRemindedV1 struct {
	PeriodRef    uuid.UUID `json:"period_ref"`
	TemplateCode string    `json:"template_code"`
	PeriodLabel  string    `json:"period_label"`
	Unit         string    `json:"unit"`
	CommuneCode  string    `json:"commune_code"`
	Slot         string    `json:"slot"`
	DueAt        string    `json:"due_at"`
	Message      string    `json:"message"`
	Channels     []string  `json:"channels"`
	// ChannelsExplicit (BF-NOTIFY-KN-ADAPTER #121): Channels là lựa chọn kênh tường minh của cấu hình nhắc theo biểu (#108)
	// → pf/notify gửi đúng các kênh đó. false/thiếu (sự kiện cũ, nhắc đợt điều tra) → hành vi cũ: hộp thư + push, email
	// khi Channels có "email".
	ChannelsExplicit bool        `json:"channels_explicit,omitempty"`
	RecipientRefs    []uuid.UUID `json:"recipient_refs"`
	Department       string      `json:"department"`
	EventTime        string      `json:"event_time"`
}
