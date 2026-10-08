// Package app là phần cài đặt của pf/workflow (xem tài liệu gói gốc internal/platform/workflow).
package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_workflow (ADR-0028). Module mới không có vai đăng
// nhập cũ <module>_app: pool là vai đăng nhập của đơn vị (app_platform), là thành viên r_workflow.
var DBModule = db.Module("pf/workflow")

// Service cài đặt port.Approvals và port.Requests.
type Service struct {
	DB    tenant.Beginner // đã bọc db.ForModule(…, DBModule); nil → Status trả lỗi
	Clock clock.Clock
}

var (
	_ port.Approvals = (*Service)(nil)
	_ port.Requests  = (*Service)(nil)
)

// errNoPool: module dựng không có pool (chỉ dùng Approvals trong giao dịch bên gọi).
var errNoPool = errors.New("workflow: chưa nối pool cho giao dịch riêng của module")

// Submit lập yêu cầu duyệt trong giao dịch của bên gọi (ctx mang giao dịch — db.JoinTx) (idempotent theo kind + subject).
func (s *Service) Submit(ctx context.Context, in port.SubmitInput) (port.Request, error) {
	tx, ok := db.JoinedTx(ctx)
	if !ok {
		return port.Request{}, port.ErrNoCallerTx
	}
	in.Summary = strings.TrimSpace(in.Summary)
	if err := domain.ValidateSubmit(in); err != nil {
		return port.Request{}, problem.Validation.WithDetail(err.Error())
	}
	if in.At.IsZero() {
		in.At = s.Clock.Now()
	}
	if in.Actor == uuid.Nil {
		in.Actor = in.Requester
	}
	r, err := store.Submit(ctx, tx, ids.New(), in.Kind, in.Subject, in.Requester, in.Beneficiary, in.Actor, in.Summary, in.At, domain.RuleVersion)
	if err != nil {
		return port.Request{}, classify(err, false)
	}
	return port.Request{ID: r.ID, Kind: r.Kind, Subject: r.Subject, Requester: r.Requester, Beneficiary: r.Beneficiary, Seq: r.Seq, Replayed: r.Replayed}, nil
}

// Decide ghi quyết định trong giao dịch của bên gọi (ctx mang giao dịch — db.JoinTx). Bốn mắt kiểm ở đây (domain.FourEyes) rồi ở DB
// (CHECK decision_four_eyes, người lập/người hưởng chép từ yêu cầu đã ghi — không tin giá trị bên gọi khai).
func (s *Service) Decide(ctx context.Context, in port.DecideInput) (port.Decision, error) {
	tx, ok := db.JoinedTx(ctx)
	if !ok {
		return port.Decision{}, port.ErrNoCallerTx
	}
	in.Note = strings.TrimSpace(in.Note)
	if err := domain.ValidateDecide(in); err != nil {
		if domain.IsFourEyes(err) {
			return port.Decision{}, problem.SelfApproval.WithDetail(err.Error()).WithCause(err)
		}
		return port.Decision{}, problem.Validation.WithDetail(err.Error())
	}
	if in.At.IsZero() {
		in.At = s.Clock.Now()
	}
	d, err := store.Decide(ctx, tx, ids.New(), ids.New(), in.Kind, in.Subject, in.Requester, in.Beneficiary, in.Decider,
		string(in.Outcome), in.Note, in.At, domain.RuleVersion)
	if err != nil {
		return port.Decision{}, classify(err, true)
	}
	return port.Decision{ID: d.ID, RequestID: d.RequestID, Outcome: port.Outcome(d.Outcome), Decider: d.Decider, At: d.At, Replayed: d.Replayed}, nil
}

// Status đọc yêu cầu của một đối tượng và quyết định của nó, trong giao dịch riêng (r_workflow, tenant từ ctx).
func (s *Service) Status(ctx context.Context, kind string, subject uuid.UUID) (port.Status, error) {
	if err := domain.ValidateKind(kind); err != nil {
		return port.Status{}, problem.Validation.WithDetail(err.Error())
	}
	if s.DB == nil {
		return port.Status{}, errNoPool
	}
	var out port.Status
	err := db.WithContextTx(ctx, s.DB, DBModule, func(tx pgx.Tx) error {
		r, d, err := store.RequestBySubject(ctx, tx, kind, subject)
		if err != nil {
			return err
		}
		if r == nil {
			return port.ErrRequestNotFound
		}
		out.Request = port.Request{ID: r.ID, Kind: r.Kind, Subject: r.Subject, Requester: r.Requester, Beneficiary: r.Beneficiary, Seq: r.Seq}
		if d != nil {
			out.Decision = &port.Decision{ID: d.ID, RequestID: d.RequestID, Outcome: port.Outcome(d.Outcome), Decider: d.Decider, At: d.At}
		}
		return nil
	})
	return out, err
}

// classify đổi lỗi DB của pf_workflow.submit/decide sang mã lỗi của module. 23505 (ledger_idem): hai lần ghi
// song song cùng đối tượng — quyết định → workflow.already_decided, lập yêu cầu → conflict.
func classify(err error, deciding bool) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch {
	case pg.Code == "AGW01":
		return port.ErrRequestMismatch.WithCause(err)
	case pg.Code == "AGW02", pg.Code == "23505" && deciding:
		return port.ErrAlreadyDecided.WithCause(err)
	case pg.Code == "23505":
		return problem.Conflict.WithCause(err)
	case pg.Code == "23514" && pg.ConstraintName == "decision_four_eyes":
		return problem.SelfApproval.WithCause(err)
	}
	return err
}
