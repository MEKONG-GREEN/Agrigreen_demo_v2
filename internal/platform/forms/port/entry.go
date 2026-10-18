package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// W11.63: cổng nhập, rà soát bài nộp kỳ native cho module nghiệp vụ có màn nhập riêng (KN phiếu "Báo cáo cho KHTC" — lưới
// hạng mục theo Kế hoạch năm do pf/extension giữ, bài nộp cap/forms mang mã bảng đã niêm phong). Dùng lại Submit/Review
// của REST (cùng kiểm quyền, giới hạn lần gửi, khóa lạc quan, rà soát bốn mắt qua cap/workflow) — chỉ đổi hình dạng kết quả.
// Người gọi lấy từ ctx (ngữ cảnh phiên KN).
// ---------------------------------------------------------------------------

// SubmissionRef là một phiên bản bài nộp kèm rà soát.
type SubmissionRef struct {
	ID          uuid.UUID
	Unit        string
	Version     int64
	Values      map[string]string
	Submitter   uuid.UUID
	SubmittedAt time.Time
	Late        bool
	// Decision: rà soát của phiên bản ("" = chưa rà soát, accepted, returned).
	Decision   string
	ReviewNote string
	ReviewedAt *time.Time
	Reviewer   *uuid.UUID
}

// Entry là cổng nhập/rà soát bài nộp kỳ native.
type Entry interface {
	// Submit nộp phiên bản mới của đơn vị (cấp lá). baseVersion = số phiên bản người nhập đã thấy (khóa lạc quan —
	// ErrVersionConflict nếu đã có bản mới hơn). Kỳ không nhận bài → ErrPeriodClosed/ErrPeriodCancelled; đã chấp nhận →
	// ErrSubmissionLocked; đủ số lần → ErrSubmissionLimit; số liệu sai biểu/quy tắc → ErrInvalidValues.
	Submit(ctx context.Context, profile string, periodID uuid.UUID, unit string, values map[string]string, baseVersion int64,
		idem string) (SubmissionRef, error)
	// Submissions trả các phiên bản bài nộp của đơn vị, mới trước (đơn vị ngoài phạm vi → problem.Forbidden).
	Submissions(ctx context.Context, profile string, periodID uuid.UUID, unit string) ([]SubmissionRef, error)
	// Review rà soát phiên bản cuối (cấp review_by; decision accepted | returned — trả lại bắt buộc ghi chú). Người rà
	// soát trùng người nộp → problem.SelfApproval; đã đủ số lần gửi mà trả lại → ErrSubmissionLimit.
	Review(ctx context.Context, profile string, submissionID uuid.UUID, decision, note, idem string) (SubmissionRef, error)
}
