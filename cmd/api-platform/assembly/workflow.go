package assembly

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
)

// ---------------------------------------------------------------------------
// cap/workflow (W6.1): duyệt bốn mắt dùng chung cho pf/masterdata (phiên bản tham số) và pf/party (đề xuất cấp
// quyền chia sẻ). Ghi trong giao dịch của module gọi; pool riêng của pf/workflow (vai đăng nhập app_platform →
// r_workflow) chỉ cho giao dịch đọc của chính module.
// ---------------------------------------------------------------------------

// wireWorkflow dựng pf/workflow và nối vào pf/masterdata, pf/party.
func wireWorkflow(pool func(string) (*pgxpool.Pool, error), clk clock.Clock, md *masterdata.Module, pty *party.Module) (*workflow.Module, error) {
	p, err := pool(dbadmin.AppPlatform)
	if err != nil {
		return nil, err
	}
	wf := workflow.New(p, clk)
	md.UseApprovals(wf.Approvals())
	pty.UseFourEyes(FourEyes{Approvals: wf.Approvals()})
	return wf, nil
}

// FourEyes chuyển workflow/port.Approvals thành party/port.FourEyes (pf/party là kernel, không import cap/*).
type FourEyes struct{ Approvals wfport.Approvals }

var _ partyport.FourEyes = FourEyes{}

// Submit thực hiện party/port.FourEyes.
func (f FourEyes) Submit(ctx context.Context, kind string, subject, requester uuid.UUID, at time.Time) error {
	_, err := f.Approvals.Submit(ctx, wfport.SubmitInput{Kind: kind, Subject: subject, Requester: requester, At: at})
	return err
}

// Decide thực hiện party/port.FourEyes.
func (f FourEyes) Decide(ctx context.Context, kind string, subject, requester, decider uuid.UUID, outcome, note string, at time.Time) error {
	_, err := f.Approvals.Decide(ctx, wfport.DecideInput{Kind: kind, Subject: subject, Requester: requester, Decider: decider,
		Outcome: wfport.Outcome(outcome), Note: note, At: at})
	return err
}
