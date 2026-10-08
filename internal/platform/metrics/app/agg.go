package app

// Job tổng hợp vượt đơn vị qua vai r_agg (W6.4, ADR-0031 §3–§5, R-DB-11, R-SEC-03).
//
//   - Danh mục có phiên bản (pf_metrics.agg_job, sổ chỉ ghi thêm ở tenant quản trị): Propose ghi đề xuất + yêu cầu
//     bốn mắt cap/workflow (kind agg.job, subject = id đề xuất) CÙNG giao dịch; Decide ghi quyết định workflow +
//     dòng approved/rejected CÙNG giao dịch. Người quyết định ≠ người đề xuất (app, CHECK decision_four_eyes ở
//     pf_workflow, CHECK agg_job_four_eyes ở pf_metrics).
//   - Run: định nghĩa job nằm trong mã (domain.AggJob); chỉ khi đúng job + phiên bản + băm là phiên bản ĐÃ DUYỆT MỚI
//     NHẤT trong danh mục mới đổi sang vai tổng hợp (db.WithAggTx — kiểm trong cùng giao dịch chỉ đọc, trước khi đổi
//     vai). k áp = max(k job, k sàn publicdata.k_min); k xin thấp hơn → refused. Gộp nhóm + ẩn nhóm < k trong tiến
//     trình; bản ghi chi tiết không rời bộ chạy. Mỗi lần chạy (kể cả bị từ chối, lỗi) ghi MỘT dòng pf_metrics.agg_run;
//     không ghi được nhật ký → không trả đầu ra.
//
// Chưa nối vào cmd/worker: bật production chờ C-4 (ngưỡng k đã ký, ADR-0031 §7).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// AggJobKind là kind yêu cầu duyệt của cap/workflow cho thêm/sửa job tổng hợp.
const AggJobKind = "agg.job"

// ParamKMin là tham số ngưỡng k sàn (K-8, C-4, Q5-3) — tạm 10 (domain.KFloorDefault) tới khi ký.
const ParamKMin = "publicdata.k_min"

// aggOwner là module giữ danh mục job (cap/analytics): giao dịch kiểm duyệt trước khi đổi vai chạy dưới vai của nó.
var aggOwner = db.Module("pf/metrics")

// Mã lỗi job tổng hợp (docs/errors.md, mục pf/metrics).
var (
	ErrAggInvalid          = problem.New(http.StatusUnprocessableEntity, "metrics.agg_job_invalid", "Định nghĩa job tổng hợp không hợp lệ")
	ErrAggKBelowFloor      = problem.New(http.StatusUnprocessableEntity, "metrics.agg_k_below_floor", "Ngưỡng k nhỏ hơn ngưỡng k sàn")
	ErrAggVersionNotNewer  = problem.New(http.StatusConflict, "metrics.agg_version_not_newer", "Phiên bản job phải lớn hơn mọi phiên bản đã đề xuất")
	ErrAggProposalNotFound = problem.New(http.StatusNotFound, "metrics.agg_proposal_not_found", "Không có đề xuất job tổng hợp")
	ErrAggAlreadyDecided   = problem.New(http.StatusConflict, "metrics.agg_already_decided", "Đề xuất job đã có quyết định khác")
	ErrAggNotApproved      = problem.New(http.StatusForbidden, "metrics.agg_job_not_approved",
		"Job tổng hợp chưa khai, chưa duyệt, không phải phiên bản duyệt mới nhất hoặc định nghĩa đã đổi")
)

// KFloorSource đọc ngưỡng k sàn (publicdata.k_min) của tenant quản trị tại thời điểm at — interface của bên dùng,
// gốc lắp ráp nối với tham số masterdata. ok = false: tham số chưa đặt → domain.KFloorDefault.
type KFloorSource interface {
	KMin(ctx context.Context, governance uuid.UUID, at time.Time) (k int, ok bool, err error)
}

// Agg là bộ quản lý danh mục và bộ chạy job tổng hợp.
type Agg struct {
	DB         tenant.Beginner // pool vai đăng nhập của đơn vị chạy job (app_worker), đã bọc db.ForModule(pf/metrics)
	Clock      clock.Clock
	Approvals  wfport.Approvals // cap/workflow
	Governance uuid.UUID        // tenant quản trị: danh mục, quyết định, nhật ký chạy
	KFloor     KFloorSource     // nil → domain.KFloorDefault
}

// AggProposal là một phiên bản job đã đề xuất hoặc đã quyết định.
type AggProposal struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	JobID     string
	Version   int
	Action    string
	DefHash   string
	Replayed  bool
}

// AggResult là kết quả một lần chạy.
type AggResult struct {
	RunID            uuid.UUID
	JobID            string
	Version          int
	KUsed            int
	Tenants          int
	Rows             int64
	Groups           []domain.AggGroup
	GroupsSuppressed int
}

func (a *Agg) now() time.Time {
	if a.Clock == nil {
		return clock.System{}.Now()
	}
	return a.Clock.Now()
}

func (a *Agg) kFloor(ctx context.Context, at time.Time) (int, error) {
	if a.KFloor == nil {
		return domain.KFloorDefault, nil
	}
	k, ok, err := a.KFloor.KMin(ctx, a.Governance, at)
	if err != nil {
		return 0, ErrDependency.WithCause(err)
	}
	if !ok {
		return domain.KFloorDefault, nil
	}
	return max(k, 2), nil
}

func (a *Agg) ready() error {
	if a.DB == nil || a.Governance == uuid.Nil {
		return ErrDependency
	}
	return nil
}

func validateJob(j domain.AggJob, floor int) error {
	if err := domain.ValidateAggJob(j, floor); err != nil {
		if errors.Is(err, domain.ErrKBelowFloor) {
			return ErrAggKBelowFloor.WithDetail(err.Error())
		}
		return ErrAggInvalid.WithDetail(err.Error())
	}
	return nil
}

// Propose đề xuất một phiên bản job: kiểm định nghĩa + k sàn, phiên bản lớn hơn mọi phiên bản đã đề xuất, ghi dòng
// đề xuất và yêu cầu duyệt bốn mắt cùng giao dịch.
func (a *Agg) Propose(ctx context.Context, j domain.AggJob, requester uuid.UUID) (AggProposal, error) {
	if err := a.ready(); err != nil {
		return AggProposal{}, err
	}
	if a.Approvals == nil {
		return AggProposal{}, ErrDependency
	}
	if requester == uuid.Nil {
		return AggProposal{}, problem.Validation.WithField("requester", "bắt buộc")
	}
	at := a.now()
	floor, err := a.kFloor(ctx, at)
	if err != nil {
		return AggProposal{}, err
	}
	if err := validateJob(j, floor); err != nil {
		return AggProposal{}, err
	}
	var out AggProposal
	err = db.WithModuleTenantTx(ctx, a.DB, aggOwner, a.Governance, func(tx pgx.Tx) error {
		maxV, err := store.AggMaxVersion(ctx, tx, j.ID)
		if err != nil {
			return err
		}
		if j.Version <= maxV {
			return ErrAggVersionNotNewer.WithDetail(fmt.Sprintf("%s: đã có v%d", j.ID, maxV))
		}
		id := ids.New()
		req, err := a.Approvals.Submit(db.JoinTx(ctx, tx), wfport.SubmitInput{Kind: AggJobKind, Subject: id, Requester: requester,
			Summary: fmt.Sprintf("job tổng hợp %s v%d", j.ID, j.Version), At: at})
		if err != nil {
			return err
		}
		row := store.AggJobRow{ID: id, JobID: j.ID, Version: j.Version, DefHash: j.Hash(), Views: j.SortedViews(),
			QuerySQL: strings.TrimSpace(j.Query), Purpose: strings.TrimSpace(j.Purpose), KMin: j.KMin, RequestRef: req.ID,
			Requester: requester, EventTime: at}
		if err := store.InsertAggProposal(ctx, tx, a.Governance, row, domain.AggRuleVersion); err != nil {
			return classifyAgg(err)
		}
		out = AggProposal{ID: id, RequestID: req.ID, JobID: j.ID, Version: j.Version, Action: "proposed", DefHash: row.DefHash}
		return nil
	})
	return out, err
}

// Decide duyệt (approved) hoặc từ chối (rejected, ghi chú bắt buộc) một đề xuất: quyết định cap/workflow + dòng
// danh mục cùng giao dịch. Gọi lặp cùng người, cùng kết quả → trả lại (Replayed).
func (a *Agg) Decide(ctx context.Context, proposal, decider uuid.UUID, outcome wfport.Outcome, note string) (AggProposal, error) {
	if err := a.ready(); err != nil {
		return AggProposal{}, err
	}
	if a.Approvals == nil {
		return AggProposal{}, ErrDependency
	}
	if outcome != wfport.OutcomeApproved && outcome != wfport.OutcomeRejected {
		return AggProposal{}, problem.Validation.WithField("outcome", "approved hoặc rejected")
	}
	note = strings.TrimSpace(note)
	at := a.now()
	var out AggProposal
	err := db.WithModuleTenantTx(ctx, a.DB, aggOwner, a.Governance, func(tx pgx.Tx) error {
		p, err := store.AggProposal(ctx, tx, proposal)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAggProposalNotFound
		}
		if err != nil {
			return err
		}
		out = AggProposal{ID: p.ID, RequestID: p.RequestRef, JobID: p.JobID, Version: p.Version, Action: string(outcome), DefHash: p.DefHash}
		prev, err := store.AggDecisionOf(ctx, tx, proposal)
		if err != nil {
			return err
		}
		if prev != nil {
			if prev.Action == string(outcome) && prev.Decider != nil && *prev.Decider == decider {
				out.Replayed = true
				return nil
			}
			return ErrAggAlreadyDecided
		}
		if _, err := a.Approvals.Decide(db.JoinTx(ctx, tx), wfport.DecideInput{Kind: AggJobKind, Subject: p.ID,
			Requester: p.Requester, Decider: decider, Outcome: outcome, Note: note, At: at}); err != nil {
			return err
		}
		var n *string
		if note != "" {
			n = &note
		}
		return classifyAgg(store.InsertAggDecision(ctx, tx, a.Governance, ids.New(), p.ID, p, string(outcome), decider, n, at, domain.AggRuleVersion))
	})
	return out, err
}

// classifyAgg đổi lỗi ràng buộc của sổ danh mục sang mã lỗi của module.
func classifyAgg(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "agg_job_four_eyes":
		return problem.SelfApproval.WithCause(err)
	case "agg_job_stream":
		return ErrAggVersionNotNewer.WithCause(err)
	}
	return err
}

// Run chạy một job đã khai trong mã. k = 0 → k áp = max(k job, k sàn). Mọi lần chạy ghi đúng một dòng nhật ký.
func (a *Agg) Run(ctx context.Context, j domain.AggJob, actor uuid.UUID, k int) (AggResult, error) {
	if err := a.ready(); err != nil {
		return AggResult{}, err
	}
	if actor == uuid.Nil {
		return AggResult{}, problem.Validation.WithField("actor", "bắt buộc")
	}
	started := a.now()
	res := AggResult{RunID: ids.New(), JobID: j.ID, Version: j.Version}
	run := store.AggRunRow{ID: res.RunID, JobID: j.ID, Version: j.Version, DefHash: j.Hash(), StartedAt: started, Actor: actor}

	floor, err := a.kFloor(ctx, started)
	if err != nil {
		return res, err
	}
	kUsed := domain.KUsed(j, floor)
	if k == 0 {
		k = kUsed
	}
	runErr := validateJob(j, floor)
	if runErr == nil {
		if err := domain.CheckK(k, kUsed); err != nil {
			runErr = ErrAggKBelowFloor.WithDetail(err.Error())
		}
	}
	acc := domain.NewAggAccumulator(j)
	var approved *store.AggJobRow
	if runErr == nil {
		authorize := func(tx pgx.Tx) error {
			r, err := store.AggLatestApproved(ctx, tx, j.ID)
			if err != nil {
				return err
			}
			if r == nil || r.Version != j.Version || r.DefHash != run.DefHash {
				return ErrAggNotApproved
			}
			approved = r
			return nil
		}
		runErr = db.WithAggTx(ctx, a.DB, aggOwner, a.Governance, authorize, func(at db.AggTx) error {
			return a.read(ctx, at, j, acc, &res)
		})
	}

	run.FinishedAt = a.now()
	run.TenantsRead, run.RowsRead = res.Tenants, acc.Rows
	switch {
	case runErr == nil:
		res.KUsed, res.Rows = k, acc.Rows
		res.Groups, res.GroupsSuppressed = acc.Suppress(k)
		run.Outcome, run.JobRef, run.KUsed = "succeeded", &approved.ID, &k
		run.GroupsOut, run.GroupsSuppressed = len(res.Groups), res.GroupsSuppressed
	case IsProblem(runErr, ErrAggNotApproved) || IsProblem(runErr, ErrAggKBelowFloor) || IsProblem(runErr, ErrAggInvalid):
		run.Outcome, run.ErrorCode = "refused", errCode(runErr)
	default:
		run.Outcome, run.ErrorCode = "failed", errCode(runErr)
	}
	if err := db.WithModuleTenantTx(ctx, a.DB, aggOwner, a.Governance, func(tx pgx.Tx) error {
		return store.InsertAggRun(ctx, tx, a.Governance, run, domain.AggRuleVersion)
	}); err != nil {
		// ADR-0031 §5: không ghi được nhật ký → job thất bại, không công bố đầu ra.
		return AggResult{RunID: res.RunID, JobID: j.ID, Version: j.Version}, ErrDependency.WithCause(fmt.Errorf("nhật ký chạy job tổng hợp: %w", err))
	}
	if runErr != nil {
		return AggResult{RunID: res.RunID, JobID: j.ID, Version: j.Version}, runErr
	}
	return res, nil
}

// read liệt kê tenant góp dữ liệu (k_ref.org, vai tổng hợp đọc vượt RLS) rồi đọc lát của từng tenant qua view _pub.
func (a *Agg) read(ctx context.Context, at db.AggTx, j domain.AggJob, acc *domain.AggAccumulator, res *AggResult) error {
	rows, err := at.Tx().Query(ctx, `SELECT DISTINCT tenant_id FROM k_ref.org ORDER BY 1`)
	if err != nil {
		return err
	}
	tids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	nd, nm := len(j.Dims), len(j.Measures)
	for _, tid := range tids {
		res.Tenants++
		err := at.InTenant(ctx, tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, j.Query)
			if err != nil {
				return err
			}
			defer rows.Close()
			vals := make([]*string, nd+nm)
			dest := make([]any, nd+nm)
			for i := range vals {
				dest[i] = &vals[i]
			}
			for rows.Next() {
				if err := rows.Scan(dest...); err != nil {
					return err
				}
				dims, ms := make([]string, nd), make([]string, nm)
				for i := range nd {
					if vals[i] != nil {
						dims[i] = *vals[i]
					}
				}
				for i := range nm {
					ms[i] = "0"
					if vals[nd+i] != nil {
						ms[i] = *vals[nd+i]
					}
				}
				if err := acc.Add(tid.String(), dims, ms); err != nil {
					return err
				}
			}
			return rows.Err()
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// IsProblem cho biết err mang problem cùng mã với want (WithDetail/WithCause tạo bản sao — errors.Is không khớp).
func IsProblem(err error, want *problem.Problem) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == want.Code
}

// errCode trả mã lỗi ổn định cho nhật ký chạy (mã problem, SQLSTATE hoặc "error").
func errCode(err error) *string {
	code := "error"
	var p *problem.Problem
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &p):
		code = p.Code
	case errors.As(err, &pg):
		code = "sqlstate:" + pg.Code
	}
	return &code
}
