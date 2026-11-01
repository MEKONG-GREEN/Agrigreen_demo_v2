package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// W11.62: cổng điều khiển kỳ native cho module nghiệp vụ có vòng đời kỳ của riêng mình (KN Kỳ KHTC — kỳ KHTC là một kỳ
// native profile kn_khtc, đơn vị lá = phòng ban). Bên gọi đọc tiến độ từng đơn vị, chốt và hủy kỳ mà không gọi REST.
// Người gọi lấy từ ctx (ngữ cảnh phiên KN) — kiểm quyền như route /v1/forms (close_by của Nút phát hành).
// ---------------------------------------------------------------------------

// UnitProgress là tiến độ thô của một đơn vị trong kỳ (bên gọi tự chiếu trạng thái nghiệp vụ của mình).
type UnitProgress struct {
	// Internal immutable identity; not added to existing JSON responses.
	LastSubmissionID *uuid.UUID `json:"-"`
	Unit             string
	// Status: trạng thái hiển thị của cap/forms (not_submitted, overdue, submitted, accepted, returned; khi kỳ đã chốt
	// với đơn vị: missed, closed_unadjusted, awaiting_reopen) — hạn nộp tính theo due_at của kỳ native.
	Status string
	// Versions: số lần đã gửi (phiên bản cuối); 0 = chưa gửi.
	Versions int64
	// Decision: rà soát của phiên bản cuối ("" = chưa rà soát, accepted, returned).
	Decision        string
	ReviewNote      string
	LastSubmittedAt *time.Time
	ReviewedAt      *time.Time
	Late            bool // phiên bản cuối gửi sau hạn (sổ phụ submission_meta)
	// Parent: xã cha của đơn vị (cơ sở/hộ → mã xã; = Unit với xã) — W11.43 (nhắc nộp, trễ liên tiếp).
	Parent string
}

// PeriodProgress là trạng thái kỳ native kèm tiến độ từng đơn vị trong phạm vi người gọi.
type PeriodProgress struct {
	ID             uuid.UUID
	Profile        string
	Label          string
	TemplateCode   string
	DueAt          time.Time
	Status         string // open | closed | reopened | cancelled
	MaxSubmissions int    // 0 = không giới hạn
	Units          []UnitProgress
	// W11.43: khóa kỳ và khoảng ngày theo lịch kỳ (rỗng/nil = kỳ không gắn lịch).
	PeriodKey string
	From, To  *time.Time
}

// PeriodControl là cổng điều khiển kỳ native (đọc tiến độ, chốt, hủy).
type PeriodControl interface {
	// Progress đọc kỳ (người gọi phải thấy kỳ — ErrPeriodNotFound nếu không).
	Progress(ctx context.Context, profile string, id uuid.UUID) (PeriodProgress, error)
	// ClosePeriod chốt kỳ kèm ảnh chụp đóng băng (ErrPeriodClosed nếu đã chốt, ErrPeriodCancelled nếu đã hủy).
	ClosePeriod(ctx context.Context, profile string, id uuid.UUID, idem string) error
	// CancelPeriod hủy kỳ đang mở, lý do 10–500 ký tự (ErrPeriodCancelled nếu đã hủy, ErrPeriodClosed nếu đã chốt).
	CancelPeriod(ctx context.Context, profile string, id uuid.UUID, reason, idem string) error
	// ChangeDue đổi hạn nộp của kỳ đang nhận bài (sổ phụ period_due, chỉ ghi thêm): cờ "Nộp trễ", trạng thái Trễ hạn và
	// hạn gửi xuống thiết bị theo hạn mới. Kỳ đã chốt/hủy → ErrPeriodClosed. reason tùy chọn (≤ 500 ký tự).
	ChangeDue(ctx context.Context, profile string, id uuid.UUID, due time.Time, reason, idem string) error
}
