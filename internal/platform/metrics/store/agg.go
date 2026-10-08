package store

// Sổ danh mục job tổng hợp và nhật ký chạy (W6.4, pf 000129): pf_metrics.agg_job, pf_metrics.agg_run — chỉ INSERT,
// trong giao dịch của module (vai r_metrics) ở tenant quản trị.

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AggJobRow là một dòng danh mục job.
type AggJobRow struct {
	ID         uuid.UUID
	JobID      string
	Version    int
	Action     string // proposed | approved | rejected
	DefHash    string
	Views      []string
	QuerySQL   string
	Purpose    string
	KMin       int
	ProposalID *uuid.UUID
	RequestRef uuid.UUID
	Requester  uuid.UUID
	Decider    *uuid.UUID
	Note       *string
	EventTime  time.Time
	RecordedAt time.Time
}

// aggStream là khóa chuỗi của một phiên bản job: 'job:<job_id>:v<version>' (khớp CHECK agg_job_stream).
func aggStream(jobID string, version int) string {
	return "job:" + jobID + ":v" + strconv.Itoa(version)
}

const aggJobCols = `id, job_id, version, action, def_hash, views, query_sql, purpose, k_min, proposal_id, request_ref,
	requester_ref, decider_ref, note, event_time, recorded_at`

func scanAggJob(row pgx.Row) (AggJobRow, error) {
	var r AggJobRow
	err := row.Scan(&r.ID, &r.JobID, &r.Version, &r.Action, &r.DefHash, &r.Views, &r.QuerySQL, &r.Purpose, &r.KMin,
		&r.ProposalID, &r.RequestRef, &r.Requester, &r.Decider, &r.Note, &r.EventTime, &r.RecordedAt)
	return r, err
}

// InsertAggProposal ghi dòng đề xuất (seq 1 của chuỗi job:<job_id>:v<version>).
func InsertAggProposal(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r AggJobRow, ruleVersion string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metrics.agg_job (id, tenant_id, stream_key, job_id, version, action, def_hash, views,
	    query_sql, purpose, k_min, request_ref, requester_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $14, $3, $4, 'proposed', $5, $6, $7, $8, $9, $10, $11, $12, $11, $15, $13)`,
		r.ID, tid, r.JobID, r.Version, r.DefHash, r.Views, r.QuerySQL, r.Purpose, r.KMin, r.RequestRef, r.Requester,
		r.EventTime, ruleVersion, aggStream(r.JobID, r.Version), "propose:"+aggStream(r.JobID, r.Version)[4:])
	return err
}

// InsertAggDecision ghi dòng quyết định (seq 2): định nghĩa, người đề xuất chép từ đề xuất ở DB (trg_bind_proposal);
// giá trị tạm truyền vào chỉ để qua NOT NULL trước trigger.
func InsertAggDecision(ctx context.Context, tx pgx.Tx, tid, id, proposal uuid.UUID, p AggJobRow, action string,
	decider uuid.UUID, note *string, at time.Time, ruleVersion string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metrics.agg_job (id, tenant_id, stream_key, job_id, version, action, def_hash, views,
	    query_sql, purpose, k_min, proposal_id, request_ref, requester_ref, decider_ref, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, '', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $14, $18, $17)`,
		id, tid, p.JobID, p.Version, action, p.DefHash, p.Views, p.QuerySQL, p.Purpose, p.KMin, proposal, p.RequestRef,
		p.Requester, decider, note, at, ruleVersion, "decide:"+proposal.String())
	return err
}

// AggProposal đọc dòng đề xuất theo id (pgx.ErrNoRows nếu không có).
func AggProposal(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AggJobRow, error) {
	return scanAggJob(tx.QueryRow(ctx, `SELECT `+aggJobCols+` FROM pf_metrics.agg_job WHERE id = $1 AND action = 'proposed'`, id))
}

// AggDecisionOf trả dòng quyết định của đề xuất (nil nếu chưa có).
func AggDecisionOf(ctx context.Context, tx pgx.Tx, proposal uuid.UUID) (*AggJobRow, error) {
	r, err := scanAggJob(tx.QueryRow(ctx, `SELECT `+aggJobCols+` FROM pf_metrics.agg_job WHERE proposal_id = $1`, proposal))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// AggMaxVersion trả phiên bản lớn nhất đã đề xuất của job (0 nếu chưa có).
func AggMaxVersion(ctx context.Context, tx pgx.Tx, jobID string) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM pf_metrics.agg_job WHERE job_id = $1`, jobID).Scan(&v)
	return v, err
}

// AggLatestApproved trả dòng 'approved' có phiên bản lớn nhất của job (nil nếu chưa có phiên bản nào được duyệt).
func AggLatestApproved(ctx context.Context, tx pgx.Tx, jobID string) (*AggJobRow, error) {
	r, err := scanAggJob(tx.QueryRow(ctx, `SELECT `+aggJobCols+` FROM pf_metrics.agg_job
	    WHERE job_id = $1 AND action = 'approved' ORDER BY version DESC LIMIT 1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// AggRunRow là một dòng nhật ký chạy.
type AggRunRow struct {
	ID               uuid.UUID
	JobID            string
	Version          int
	DefHash          string
	JobRef           *uuid.UUID
	KUsed            *int
	StartedAt        time.Time
	FinishedAt       time.Time
	TenantsRead      int
	RowsRead         int64
	GroupsOut        int
	GroupsSuppressed int
	Outcome          string // succeeded | failed | refused
	ErrorCode        *string
	Actor            uuid.UUID
}

// InsertAggRun ghi một dòng nhật ký chạy.
func InsertAggRun(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r AggRunRow, ruleVersion string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metrics.agg_run (id, tenant_id, stream_key, job_id, version, def_hash, job_ref, k_used,
	    started_at, finished_at, tenants_read, rows_read, groups_out, groups_suppressed, outcome, error_code, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $18, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $9, $16, $19, $17)`,
		r.ID, tid, r.JobID, r.Version, r.DefHash, r.JobRef, r.KUsed, r.StartedAt, r.FinishedAt, r.TenantsRead, r.RowsRead,
		r.GroupsOut, r.GroupsSuppressed, r.Outcome, r.ErrorCode, r.Actor, ruleVersion, "run:"+r.JobID, "run:"+r.ID.String())
	return err
}

// AggRuns trả nhật ký chạy của job, cũ trước.
func AggRuns(ctx context.Context, tx pgx.Tx, jobID string) ([]AggRunRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, job_id, version, def_hash, job_ref, k_used, started_at, finished_at, tenants_read,
	    rows_read, groups_out, groups_suppressed, outcome, error_code, actor_ref
	  FROM pf_metrics.agg_run WHERE job_id = $1 ORDER BY seq`, jobID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AggRunRow, error) {
		var r AggRunRow
		err := row.Scan(&r.ID, &r.JobID, &r.Version, &r.DefHash, &r.JobRef, &r.KUsed, &r.StartedAt, &r.FinishedAt,
			&r.TenantsRead, &r.RowsRead, &r.GroupsOut, &r.GroupsSuppressed, &r.Outcome, &r.ErrorCode, &r.Actor)
		return r, err
	})
}
