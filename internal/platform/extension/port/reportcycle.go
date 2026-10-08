package port

// Vòng đời báo cáo KN v7.7 (gói P4, docs/brd/kn-v7.7-plan.md §3 P4): phát hành biểu → công việc xã × biểu ×
// kỳ → bài nộp có phiên bản → rà soát → chốt/mở lại kỳ → "Báo cáo tổng hợp các xã" (lần tổng hợp bất biến)
// → dashboard Tỉnh/TW, báo cáo Chi cục. Hiện thực ở extension/reportcycle; module khác chỉ dùng kiểu ở đây.

import (
	"github.com/google/uuid"
)

// SystemActor là actor_ref của tác nhân hệ thống (tự chốt kỳ, tự tổng hợp lúc chốt, tự nhắc) — nhật ký hiện "Hệ thống".
var SystemActor = uuid.MustParse("00000000-0000-7000-8000-00000000c004")

// Sự kiện vòng đời báo cáo (schema trong contracts/events/).
const (
	EventReportBatchIssued             = "pf.extension.report_batch.issued"
	EventReportTaskReminded            = "pf.extension.report_task.reminded"
	EventSubmissionSubmitted           = "pf.extension.submission.submitted"
	EventSubmissionAdjustmentRequested = "pf.extension.submission.adjustment_requested"
	EventPeriodClosed                  = "pf.extension.period.closed"
	EventPeriodReopened                = "pf.extension.period.reopened"
	EventConsolidationCompleted        = "pf.extension.consolidation.completed"
)

// Entity đồng bộ ngoại tuyến của vòng đời báo cáo (KN v7.7 P7, docs/dev/sync-contract.md mục "Báo cáo KN v7.7").
const (
	EntityReportTask       = "report_task"       // chiều xuống: công việc của các xã phụ trách (Cấp Xã)
	EntityFormLayout       = "form_layout"       // chiều xuống: phiên bản mẫu của công việc (bất biến)
	EntityReportSubmission = "report_submission" // chiều xuống: bài nộp mới nhất của công việc; chiều lên: lưu nháp/gửi
)

// Chế độ nhắc.
const (
	ReminderManual      = "manual"       // Cấp Tỉnh "Gửi nhắc nhở"
	ReminderAutoBefore  = "auto_before"  // tự nhắc trước hạn
	ReminderAutoOverdue = "auto_overdue" // tự nhắc khi trễ hạn
)

// Khóa tham số (pf/masterdata) — TẠM, chờ khách (Q-15, Q-17).
const (
	// ParamReminderBeforeHours: tự nhắc khi còn ≤ n giờ tới hạn mà chưa gửi. Mặc định 24; 0 = tắt.
	ParamReminderBeforeHours = "extension.report_reminder_before_hours"
	// ParamReminderOverdueEveryHours: khi trễ hạn, nhắc lại mỗi n giờ tới khi chốt kỳ. Mặc định 24; 0 = tắt.
	ParamReminderOverdueEveryHours = "extension.report_reminder_overdue_every_hours"
	// ParamPeriodWeekNumbering: "season" (Tuần n của đợt, mặc định) hoặc "iso" (Tuần ISO, vd Tuần 36) — Q-17.
	ParamPeriodWeekNumbering = "extension.report_period_week_numbering"
	// ParamAttachmentLimitMB (pf/reports): giới hạn tổng dung lượng đính kèm email Chi cục. Mặc định 10.
	ParamAttachmentLimitMB = "reports.dispatch_attachment_limit_mb"
)

// TaskAssigneesV1 là cán bộ phụ trách một xã nhận công việc của đợt.
type TaskAssigneesV1 struct {
	CommuneCode string   `json:"commune_code"`
	OfficerRefs []string `json:"officer_refs"`
	TaskCount   int      `json:"task_count"`
}

// ReportBatchIssuedV1 là payload của pf.extension.report_batch.issued: đợt phát hành đã sinh công việc cho
// từng xã theo từng kỳ (FN-TI-02.02). Consumer pf/notify báo "có công việc mới" tới từng cán bộ phụ trách
// (một thông báo mỗi cán bộ, không phải mỗi công việc). Không PII.
//
//ag:event pf.extension.report_batch.issued v1
type ReportBatchIssuedV1 struct {
	BatchID        string            `json:"batch_id"`
	DepartmentCode string            `json:"department_code"`
	ProvinceCode   string            `json:"province_code"`
	ReportType     string            `json:"report_type"`
	SeasonLabel    string            `json:"season_label"`
	FormCodes      []string          `json:"form_codes"`
	PeriodCount    int               `json:"period_count"`
	TaskCount      int               `json:"task_count"`
	FirstDueAt     string            `json:"first_due_at"`
	Assignees      []TaskAssigneesV1 `json:"assignees"`
	IssuedBy       string            `json:"issued_by"`
	EventTime      string            `json:"event_time"`
}

// ReportTaskRemindedV1 là payload của pf.extension.report_task.reminded: nhắc xã chưa nộp/trễ hạn (thủ công
// hoặc tự động, FN-TI-02.03). Nội dung nhắc do Cấp Tỉnh soạn, không chứa PII theo hướng dẫn màn hình.
//
//ag:event pf.extension.report_task.reminded v1
type ReportTaskRemindedV1 struct {
	ReminderID    string   `json:"reminder_id"`
	BatchID       string   `json:"batch_id"`
	PeriodID      string   `json:"period_id"`
	PeriodLabel   string   `json:"period_label"`
	Mode          string   `json:"mode"`
	Message       string   `json:"message"`
	DueAt         string   `json:"due_at"`
	Communes      []string `json:"communes"`
	TaskIDs       []string `json:"task_ids"`
	RecipientRefs []string `json:"recipient_refs"`
	ActorRef      string   `json:"actor_ref"`
	EventTime     string   `json:"event_time"`
}

// SubmissionSubmittedV1 là payload của pf.extension.submission.submitted: một bài nộp "Đã gửi" (gửi mới, gửi
// lại hoặc gửi theo yêu cầu điều chỉnh). Bảng tổng hợp chỉ đổi sau "Cập nhật" (FN-TI-02.05) — sự kiện để hiện
// "Có n thay đổi mới". Không có giá trị ô.
//
//ag:event pf.extension.submission.submitted v1
type SubmissionSubmittedV1 struct {
	SubmissionID       string  `json:"submission_id"`
	TaskID             string  `json:"task_id"`
	PeriodID           string  `json:"period_id"`
	TemplateID         string  `json:"template_id"`
	FormCode           string  `json:"form_code"`
	CommuneCode        string  `json:"commune_code"`
	DepartmentCode     string  `json:"department_code"`
	ProvinceCode       string  `json:"province_code"`
	VersionNo          int     `json:"version_no"`
	AdjustmentResponse bool    `json:"adjustment_response"`
	OnBehalfOf         *string `json:"on_behalf_of"`
	ActorRef           string  `json:"actor_ref"`
	EventTime          string  `json:"event_time"`
}

// SubmissionAdjustmentRequestedV1 là payload của pf.extension.submission.adjustment_requested: Cấp Tỉnh yêu cầu
// xã điều chỉnh kèm lý do bắt buộc (FN-TI-02.04 AC). Consumer báo tới cán bộ phụ trách.
//
//ag:event pf.extension.submission.adjustment_requested v1
type SubmissionAdjustmentRequestedV1 struct {
	TaskID        string   `json:"task_id"`
	SubmissionID  string   `json:"submission_id"`
	FormCode      string   `json:"form_code"`
	CommuneCode   string   `json:"commune_code"`
	PeriodLabel   string   `json:"period_label"`
	Reason        string   `json:"reason"`
	RecipientRefs []string `json:"recipient_refs"`
	ActorRef      string   `json:"actor_ref"`
	EventTime     string   `json:"event_time"`
}

// PeriodClosedV1 là payload của pf.extension.period.closed: kỳ đã chốt (thủ công hoặc tự động theo lịch).
//
//ag:event pf.extension.period.closed v1
type PeriodClosedV1 struct {
	PeriodID       string `json:"period_id"`
	BatchID        string `json:"batch_id"`
	Label          string `json:"label"`
	Mode           string `json:"mode"`
	DepartmentCode string `json:"department_code"`
	ProvinceCode   string `json:"province_code"`
	ActorRef       string `json:"actor_ref"`
	EventTime      string `json:"event_time"`
}

// PeriodReopenedV1 là payload của pf.extension.period.reopened: mở lại kỳ đã chốt cho xã/biểu, kèm lý do và hạn
// mở (FN-TI-02.04). DispatchChanged = báo cáo Chi cục của kỳ đã gửi → cảnh báo "số liệu đã thay đổi".
//
//ag:event pf.extension.period.reopened v1
type PeriodReopenedV1 struct {
	PeriodID        string   `json:"period_id"`
	BatchID         string   `json:"batch_id"`
	Label           string   `json:"label"`
	Communes        []string `json:"communes"`
	FormCodes       []string `json:"form_codes"`
	Reason          string   `json:"reason"`
	ReopenUntil     string   `json:"reopen_until"`
	RecipientRefs   []string `json:"recipient_refs"`
	DispatchChanged bool     `json:"dispatch_changed"`
	ActorRef        string   `json:"actor_ref"`
	EventTime       string   `json:"event_time"`
}

// ConsolidationCompletedV1 là payload của pf.extension.consolidation.completed: một lần "Cập nhật" (người hoặc
// Hệ thống lúc chốt kỳ) đã ghi lần tổng hợp mới — nguồn của dashboard, báo cáo TW, Chi cục.
//
//ag:event pf.extension.consolidation.completed v1
type ConsolidationCompletedV1 struct {
	RunIDs         []string `json:"run_ids"`
	PeriodID       string   `json:"period_id"`
	BatchID        string   `json:"batch_id"`
	FormCodes      []string `json:"form_codes"`
	Trigger        string   `json:"trigger"`
	DepartmentCode string   `json:"department_code"`
	ProvinceCode   string   `json:"province_code"`
	CellsChanged   int      `json:"cells_changed"`
	EventTime      string   `json:"event_time"`
}
