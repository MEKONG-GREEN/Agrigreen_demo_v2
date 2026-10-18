package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// W11.43: bài nộp đã ghi cho quy tắc liên biểu + cảnh báo của bên dùng (KN CNTS — pf/extension). cap/forms phát sự kiện
// pf.forms.submission.recorded cùng giao dịch nộp bài; bên dùng (consumer inbox) đọc số liệu và các kỳ trước của đơn vị
// qua SubmissionFeed. Đọc của hệ thống (job, consumer): KHÔNG kiểm người gọi — tenant truyền tường minh.
// ---------------------------------------------------------------------------

// EventSubmissionRecorded: một phiên bản bài nộp đã ghi (kỳ đang nhận bài; bài giữ "Chờ mở kỳ" KHÔNG phát).
const EventSubmissionRecorded = "pf.forms.submission.recorded"

// SubmissionRecordedV1 là payload của pf.forms.submission.recorded. Không mang số liệu (consumer đọc qua SubmissionFeed).
//
//ag:event pf.forms.submission.recorded v1
type SubmissionRecordedV1 struct {
	SubmissionID string `json:"submission_id"`
	PeriodRef    string `json:"period_ref"`
	Profile      string `json:"profile"`
	TemplateCode string `json:"template_code"`
	Unit         string `json:"unit"`
	Version      int64  `json:"version"`
	NilReport    bool   `json:"nil_report"`
	Late         bool   `json:"late"`
	ActorRef     string `json:"actor_ref"`
	EventTime    string `json:"event_time"`
}

// RecordedSubmission là một phiên bản bài nộp kèm kỳ và đơn vị.
type RecordedSubmission struct {
	ID           uuid.UUID
	PeriodID     uuid.UUID
	Profile      string
	TemplateCode string
	PeriodLabel  string
	PeriodKind   string
	PeriodKey    string
	From, To     *time.Time // khoảng ngày của kỳ (nil = kỳ không gắn lịch)
	DueAt        time.Time
	UnitKind     string
	Unit         string
	Parent       string // mã xã cha (cơ sở/hộ); = Unit với xã
	Version      int64
	Values       map[string]string
	NilReport    bool
	Submitter    uuid.UUID
	EventTime    time.Time
}

// SubmissionQuery lọc bài nộp CUỐI của từng (kỳ, đơn vị) của một biểu: đơn vị trong Units HOẶC có xã cha trong Parents
// (cả hai rỗng = mọi đơn vị), kỳ có ngày đầu trong [From, To] (bỏ trống = không chặn). Mới trước (ngày đầu kỳ giảm dần).
type SubmissionQuery struct {
	Profile      string
	TemplateCode string
	Units        []string
	Parents      []string
	From, To     *time.Time
	Limit        int // 0 = 200
}

// SubmissionFeed là cổng đọc của hệ thống cho bên dùng có quy tắc liên biểu (W11.43).
type SubmissionFeed interface {
	// Recorded đọc một phiên bản bài nộp (nil nếu không có).
	Recorded(ctx context.Context, tenantID, submissionID uuid.UUID) (*RecordedSubmission, error)
	// Latest trả bài nộp cuối của từng (kỳ, đơn vị) khớp q.
	Latest(ctx context.Context, tenantID uuid.UUID, q SubmissionQuery) ([]RecordedSubmission, error)
	// DuePeriods trả kỳ của profile (kỳ đang nhận bài hoặc đã chốt, không kỳ hủy) có hạn nộp trong [dueFrom, dueTo]
	// kèm tiến độ MỌI đơn vị (nhắc nộp, trễ liên tiếp).
	DuePeriods(ctx context.Context, tenantID uuid.UUID, profile string, dueFrom, dueTo time.Time) ([]PeriodProgress, error)
}
