// Package port là giao diện công khai DUY NHẤT của pf/workflow (`cap/workflow`, kiến trúc v4.1 §26) mà module
// khác được import (R-02): yêu cầu duyệt (sổ request) và quyết định (sổ decision) với quy tắc bốn mắt — người
// duyệt khác người lập, khác người được hưởng (R-56), kiểm ở app/ và bằng CHECK/trigger ở DB (pf 000118).
//
// Module không biết đối tượng nghiệp vụ là gì: bên gọi đặt Kind ('<module>.<đối tượng>') và Subject (mã đối
// tượng của mình). Một đối tượng một yêu cầu, một yêu cầu một quyết định.
package port

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Outcome là kết quả của một quyết định.
type Outcome string

// Các kết quả. Cancelled: bên lập rút hoặc đối tượng bị thu hồi trước khi duyệt — không cần bốn mắt.
const (
	OutcomeApproved  Outcome = "approved"
	OutcomeRejected  Outcome = "rejected"
	OutcomeCancelled Outcome = "cancelled"
)

// SubmitInput là dữ liệu lập yêu cầu duyệt.
type SubmitInput struct {
	Kind        string     // '<module>.<đối tượng>', vd masterdata.param_version
	Subject     uuid.UUID  // mã đối tượng của module chủ
	Requester   uuid.UUID  // người lập
	Beneficiary *uuid.UUID // người được hưởng (nếu là một người)
	Actor       uuid.UUID  // người ghi; Nil = Requester
	Summary     string     // tóm tắt ngắn (≤ 200 ký tự, không dữ liệu cá nhân); rỗng = không có
	At          time.Time  // thời điểm nghiệp vụ (R-13)
}

// Request là một yêu cầu duyệt đã ghi.
type Request struct {
	ID          uuid.UUID
	Kind        string
	Subject     uuid.UUID
	Requester   uuid.UUID
	Beneficiary *uuid.UUID
	Seq         int64
	Replayed    bool // đã có từ trước (gọi lặp)
}

// DecideInput là dữ liệu quyết định một yêu cầu. Requester/Beneficiary dùng để lập bù yêu cầu khi đối tượng
// được gửi duyệt trước khi có pf/workflow (chạy song song — nguyên tắc 3 của kế hoạch v4.1); đã có yêu cầu thì
// phải khớp, không thì ErrRequestMismatch.
type DecideInput struct {
	Kind        string
	Subject     uuid.UUID
	Requester   uuid.UUID
	Beneficiary *uuid.UUID
	Decider     uuid.UUID
	Outcome     Outcome
	Note        string // bắt buộc với rejected, cancelled (≤ 1000 ký tự)
	At          time.Time
}

// Decision là một quyết định đã ghi.
type Decision struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	Outcome   Outcome
	Decider   uuid.UUID
	At        time.Time
	Replayed  bool // cùng người quyết định, cùng kết quả đã ghi trước đó
}

// Approvals ghi yêu cầu và quyết định TRONG GIAO DỊCH CỦA BÊN GỌI: ctx phải mang giao dịch đang mở của bên gọi
// (db.JoinTx — hợp đồng port không import driver DB), đã đặt tenant (tenant.WithTenantTx / db.WithContextTx);
// thiếu → ErrNoCallerTx. Thay đổi trạng thái nghiệp vụ và sổ duyệt cùng commit hoặc cùng rollback (R-21).
// Bên gọi không cần quyền bảng pf_workflow: lối ghi là hàm DB của module (pf 000118) — vai nhóm của bên gọi
// cần EXECUTE do migration của pf/workflow cấp.
type Approvals interface {
	Submit(ctx context.Context, in SubmitInput) (Request, error)
	Decide(ctx context.Context, in DecideInput) (Decision, error)
}

// Status là trạng thái một yêu cầu: chưa quyết định khi Decision = nil.
type Status struct {
	Request  Request
	Decision *Decision
}

// Requests đọc trạng thái yêu cầu trong giao dịch riêng của module (vai r_workflow), tenant lấy từ ctx.
type Requests interface {
	Status(ctx context.Context, kind string, subject uuid.UUID) (Status, error)
}

// Mã lỗi của module (docs/errors.md, mục pf/workflow). Vi phạm bốn mắt trả problem.SelfApproval
// (self_approval_forbidden) như mọi luồng duyệt khác.
var (
	// ErrNoCallerTx: gọi Approvals mà ctx không mang giao dịch của bên gọi (lỗi lắp ráp/lập trình, không phải của người dùng).
	ErrNoCallerTx      = errors.New("workflow: ctx không mang giao dịch của bên gọi (db.JoinTx)")
	ErrRequestNotFound = problem.New(http.StatusNotFound, "workflow.request_not_found", "Không có yêu cầu duyệt cho đối tượng này")
	ErrAlreadyDecided  = problem.New(http.StatusConflict, "workflow.already_decided", "Yêu cầu đã có quyết định khác")
	ErrRequestMismatch = problem.New(http.StatusConflict, "workflow.request_mismatch",
		"Đối tượng đã có yêu cầu duyệt với người lập hoặc người được hưởng khác")
)
