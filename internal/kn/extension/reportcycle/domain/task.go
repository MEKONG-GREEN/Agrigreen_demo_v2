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

// Trạng thái hiển thị của vòng đời mở rộng (W11.05, CNTS v3.7 III.3.0 — bảng vòng đời dòng 10, 11, 13; khớp
// cap/forms domain.SubMissed/SubClosedUnadjusted/SubAwaitingReopen). Chỉ phòng ban có ExtendedLifecycle; Trồng trọt
// giữ "Đã chốt" như v7.7.
const (
	TaskMissed           = "missed"            // Không nộp: kỳ chốt khi chưa gửi (Chưa nhập, Lưu nháp, Trễ hạn) — tính 0
	TaskClosedUnadjusted = "closed_unadjusted" // Đã chốt – chưa điều chỉnh: kỳ chốt khi đang Yêu cầu điều chỉnh
	TaskAwaitingReopen   = "awaiting_reopen"   // Chờ mở kỳ: bản offline đến sau chốt (ghi nhận ở W11.42, CN2)
)

// DisplayStatuses là mọi trạng thái hiển thị (bộ lọc của màn Công việc cần làm — Trồng trọt).
var DisplayStatuses = []string{TaskNotStarted, TaskDraft, TaskSubmitted, TaskOverdue, TaskAdjustmentRequested, TaskConfirmed, TaskClosed}

// ExtendedDisplayStatuses là trạng thái hiển thị của phòng ban có vòng đời mở rộng.
var ExtendedDisplayStatuses = []string{TaskNotStarted, TaskDraft, TaskSubmitted, TaskOverdue, TaskAdjustmentRequested, TaskConfirmed, TaskClosed,
	TaskClosedUnadjusted, TaskMissed, TaskAwaitingReopen}

// StatusLabel là nhãn tiếng Việt của trạng thái hiển thị.
var StatusLabel = map[string]string{
	TaskNotStarted: "Chưa nhập", TaskDraft: "Lưu nháp", TaskSubmitted: "Đã nộp", TaskOverdue: "Trễ hạn",
	TaskAdjustmentRequested: "Yêu cầu điều chỉnh", TaskConfirmed: "Đã xác nhận", TaskClosed: "Đã chốt",
	TaskClosedUnadjusted: "Đã chốt – chưa điều chỉnh", TaskMissed: "Không nộp", TaskAwaitingReopen: "Chờ mở kỳ",
}

// extendedLifecycle là phòng ban dùng vòng đời mở rộng (giả định W11.05: CNTS — BRD v3.7 III.3.0; Trồng trọt Final
// không có ba trạng thái này). Thêm phòng ban ở đây khi BRD của phòng đó yêu cầu.
var extendedLifecycle = map[string]bool{"chan_nuoi_thuy_san": true}

// ExtendedLifecycle cho biết phòng ban dùng vòng đời mở rộng (Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ).
func ExtendedLifecycle(department string) bool { return extendedLifecycle[department] }

// TaskState là phần của công việc cần để suy trạng thái.
type TaskState struct {
	Status        string
	DueAt         time.Time
	PeriodClosed  bool
	ReopenedUntil *time.Time
	// Extended: vòng đời mở rộng (ExtendedLifecycle của phòng ban) — kỳ chốt thì chưa gửi → Không nộp, đang yêu cầu
	// điều chỉnh → Đã chốt – chưa điều chỉnh; false giữ "Đã chốt" (v7.7).
	Extended bool
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
		if s.Extended {
			switch s.Status {
			case TaskNotStarted, TaskDraft:
				return TaskMissed
			case TaskAdjustmentRequested:
				return TaskClosedUnadjusted
			}
		}
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
