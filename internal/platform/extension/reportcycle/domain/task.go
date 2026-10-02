package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Trạng thái lưu của công việc (bảng chiếu report_task.status).
const (
	TaskNotStarted          = "not_started"          // Chưa nhập
	TaskDraft               = "draft"                // Lưu nháp
	TaskSubmitted           = "submitted"            // Đã nộp
	TaskAdjustmentRequested = "adjustment_requested" // Yêu cầu điều chỉnh
	TaskConfirmed           = "confirmed"            // Đã xác nhận
)

// Trạng thái hiển thị bổ sung (suy ra lúc đọc — FN-XA-03.01 "trạng thái do hệ thống cập nhật").
const (
	TaskOverdue = "overdue" // Trễ hạn: chưa gửi và đã qua hạn — vẫn nộp được tới khi chốt kỳ
	TaskClosed  = "closed"  // Đã chốt
)

// DisplayStatuses là mọi trạng thái hiển thị (bộ lọc của màn Công việc cần làm).
var DisplayStatuses = []string{TaskNotStarted, TaskDraft, TaskSubmitted, TaskOverdue, TaskAdjustmentRequested, TaskConfirmed, TaskClosed}

// StatusLabel là nhãn tiếng Việt của trạng thái hiển thị.
var StatusLabel = map[string]string{
	TaskNotStarted: "Chưa nhập", TaskDraft: "Lưu nháp", TaskSubmitted: "Đã nộp", TaskOverdue: "Trễ hạn",
	TaskAdjustmentRequested: "Yêu cầu điều chỉnh", TaskConfirmed: "Đã xác nhận", TaskClosed: "Đã chốt",
}

// TaskState là phần của công việc cần để suy trạng thái.
type TaskState struct {
	Status        string
	DueAt         time.Time
	PeriodClosed  bool
	ReopenedUntil *time.Time
}

// Open cho biết xã còn nộp được (kỳ chưa chốt, hoặc đã mở lại cho xã/biểu này và chưa hết hạn mở).
func (s TaskState) Open(now time.Time) bool {
	if !s.PeriodClosed {
		return true
	}
	return s.ReopenedUntil != nil && now.Before(*s.ReopenedUntil)
}

// Display suy trạng thái hiển thị tại now.
func (s TaskState) Display(now time.Time) string {
	if !s.Open(now) {
		return TaskClosed
	}
	if (s.Status == TaskNotStarted || s.Status == TaskDraft) && !now.Before(s.DueAt) {
		return TaskOverdue
	}
	return s.Status
}

// Pending cho biết công việc còn phải gửi (dùng cho nhắc và "Xã chưa nộp").
func (s TaskState) Pending() bool {
	return s.Status == TaskNotStarted || s.Status == TaskDraft || s.Status == TaskAdjustmentRequested
}

// MinReason là độ dài tối thiểu của lý do (yêu cầu điều chỉnh, mở lại kỳ, thu hồi đợt, sửa hạn).
const MinReason = 10

// Reason chuẩn hóa và kiểm lý do bắt buộc (10–max ký tự).
func Reason(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= MinReason && n <= max
}

// Kind của bài nộp.
const (
	KindDraft     = "draft"
	KindSubmitted = "submitted"
)
