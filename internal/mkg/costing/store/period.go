package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// Period là kỳ theo bảng chiếu period_current.
type Period struct {
	ID         uuid.UUID
	Code       string
	From, To   time.Time
	Status     port.PeriodStatus
	OpenedAt   time.Time
	CloseRunID *uuid.UUID
	ClosedBy   *uuid.UUID
	ClosedAt   *time.Time
	JournalRef *uuid.UUID
	PostedAt   *time.Time
	LastSeq    int64
}

const periodCols = `period_id, code, from_at, to_at, status, opened_at, close_run_id, closed_by, closed_at, journal_ref, posted_at, last_seq`

func scanPeriod(r pgx.Row) (Period, error) {
	var p Period
	var st string
	err := r.Scan(&p.ID, &p.Code, &p.From, &p.To, &st, &p.OpenedAt, &p.CloseRunID, &p.ClosedBy, &p.ClosedAt, &p.JournalRef, &p.PostedAt, &p.LastSeq)
	p.Status = port.PeriodStatus(st)
	return p, err
}

func lockClause(forUpdate bool) string {
	if forUpdate {
		return " FOR UPDATE"
	}
	return ""
}

// PeriodByCode đọc kỳ theo mã.
func PeriodByCode(ctx context.Context, tx pgx.Tx, code string, forUpdate bool) (*Period, error) {
	return one(scanPeriod(tx.QueryRow(ctx, "SELECT "+periodCols+" FROM mkg_costing.period_current WHERE code = $1"+lockClause(forUpdate), code)))
}

// PeriodByID đọc kỳ theo id.
func PeriodByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Period, error) {
	return one(scanPeriod(tx.QueryRow(ctx, "SELECT "+periodCols+" FROM mkg_costing.period_current WHERE period_id = $1"+lockClause(forUpdate), id)))
}

// ListPeriods liệt kê kỳ (mới trước), before = mã kỳ con trỏ.
func ListPeriods(ctx context.Context, tx pgx.Tx, before string, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+periodCols+` FROM mkg_costing.period_current
	  WHERE ($1 = '' OR code < $1) ORDER BY code DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// PeriodEvent là một bản ghi sổ vòng đời kỳ.
type PeriodEvent struct {
	ID          uuid.UUID
	PeriodID    uuid.UUID
	Code        string
	Action      string // opened | close_requested | closed | posted
	From, To    time.Time
	CloseRunID  *uuid.UUID
	JournalRef  *uuid.UUID
	ExternalRef *string
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
	Seq         int64
	RecordedAt  time.Time
}

// InsertPeriodEvent ghi sổ kỳ và áp vào bảng chiếu cùng giao dịch (R-21).
func InsertPeriodEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *PeriodEvent) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_costing.period_event
	  (id, tenant_id, stream_key, period_id, period_code, action, from_at, to_at, close_run_id, journal_ref, external_ref,
	   event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING seq, recorded_at`,
		e.ID, tid, "period:"+e.PeriodID.String(), e.PeriodID, e.Code, e.Action, e.From, e.To, e.CloseRunID, e.JournalRef, e.ExternalRef,
		e.EventTime, e.ActorRef, e.IdemKey, domain.RulePeriod).Scan(&e.Seq, &e.RecordedAt)
	if err != nil {
		return ledger.Classify(err)
	}
	return applyPeriodEvent(ctx, tx, tid, *e)
}

// applyPeriodEvent là hàm chiếu DUY NHẤT của period_current (dùng cả khi ghi và khi dựng lại).
func applyPeriodEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e PeriodEvent) error {
	var err error
	switch e.Action {
	case "opened":
		_, err = tx.Exec(ctx, `INSERT INTO mkg_costing.period_current
		  (tenant_id, period_id, code, from_at, to_at, status, opened_at, last_seq)
		  VALUES ($1, $2, $3, $4, $5, 'open', $6, $7)`, tid, e.PeriodID, e.Code, e.From, e.To, e.EventTime, e.Seq)
	case "close_requested":
		_, err = tx.Exec(ctx, `UPDATE mkg_costing.period_current SET status = 'closing', close_run_id = $2, last_seq = $3, updated_at = now()
		  WHERE period_id = $1`, e.PeriodID, e.CloseRunID, e.Seq)
	case "closed":
		_, err = tx.Exec(ctx, `UPDATE mkg_costing.period_current SET status = 'closed', close_run_id = $2, closed_by = $3, closed_at = $4,
		  last_seq = $5, updated_at = now() WHERE period_id = $1`, e.PeriodID, e.CloseRunID, e.ActorRef, e.EventTime, e.Seq)
	case "posted":
		_, err = tx.Exec(ctx, `UPDATE mkg_costing.period_current SET status = 'posted', journal_ref = $2, posted_at = $3, last_seq = $4,
		  updated_at = now() WHERE period_id = $1`, e.PeriodID, e.JournalRef, e.EventTime, e.Seq)
	}
	return err
}

// ---------------------------------------------------------------------------
// Job đóng kỳ
// ---------------------------------------------------------------------------

// CloseJob là job đóng kỳ (mẫu mục 12).
type CloseJob struct {
	ID               uuid.UUID
	PeriodID         uuid.UUID
	PeriodCode       string
	Status           string // queued | completed | failed
	RequestedBy      uuid.UUID
	RequestedAt      time.Time
	AsOf             time.Time
	Snapshot         domain.Snapshot
	SnapshotSHA256   string
	AdjustingAuthors []uuid.UUID
	IdemKey          string
	ErrorCode        *string
	CompletedAt      *time.Time
}

const jobCols = `id, period_id, period_code, status, requested_by, requested_at, as_of, snapshot::text, input_snapshot_sha256,
  adjusting_authors::text[], idem_key, error_code, completed_at`

func scanJob(r pgx.Row) (CloseJob, error) {
	var j CloseJob
	var raw string
	var authors []string
	err := r.Scan(&j.ID, &j.PeriodID, &j.PeriodCode, &j.Status, &j.RequestedBy, &j.RequestedAt, &j.AsOf, &raw, &j.SnapshotSHA256,
		&authors, &j.IdemKey, &j.ErrorCode, &j.CompletedAt)
	if err != nil {
		return j, err
	}
	for _, a := range authors {
		id, err := uuid.Parse(a)
		if err != nil {
			return j, err
		}
		j.AdjustingAuthors = append(j.AdjustingAuthors, id)
	}
	return j, json.Unmarshal([]byte(raw), &j.Snapshot)
}

// InsertJob ghi job đóng kỳ với ảnh chụp bất biến (DB CHECK + trigger: người đóng ≠ người lập điều chỉnh).
func InsertJob(ctx context.Context, tx pgx.Tx, tid uuid.UUID, j *CloseJob) error {
	raw, err := j.Snapshot.Canonical()
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_costing.close_job
	  (id, tenant_id, period_id, period_code, status, requested_by, as_of, snapshot, input_snapshot_sha256, adjusting_authors, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7::jsonb, $8, $9::uuid[], $10, $11) RETURNING requested_at`,
		j.ID, tid, j.PeriodID, j.PeriodCode, j.RequestedBy, j.AsOf, string(raw), j.SnapshotSHA256, uuidStrings(j.AdjustingAuthors), j.IdemKey,
		domain.RulePeriodClose).
		Scan(&j.RequestedAt)
}

// JobByID đọc job.
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*CloseJob, error) {
	return one(scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM mkg_costing.close_job WHERE id = $1"+lockClause(forUpdate), id)))
}

// JobByIdem đọc job theo idem_key.
func JobByIdem(ctx context.Context, tx pgx.Tx, idem string) (*CloseJob, error) {
	return one(scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM mkg_costing.close_job WHERE idem_key = $1", idem)))
}

// CompleteJob đánh dấu job xong.
func CompleteJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_costing.close_job SET status = 'completed', completed_at = $2, attempts = attempts + 1 WHERE id = $1`, id, at)
	return err
}

// FailJob đánh dấu job lỗi vĩnh viễn.
func FailJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_costing.close_job SET status = 'failed', error_code = $2, attempts = attempts + 1 WHERE id = $1`, id, code)
	return err
}

// ---------------------------------------------------------------------------
// Kết quả đóng kỳ
// ---------------------------------------------------------------------------

// Summary là kết quả đóng kỳ đã ghi sổ.
type Summary struct {
	ID                  uuid.UUID
	PeriodID            uuid.UUID
	PeriodCode          string
	CloseRunID          uuid.UUID
	From, To            time.Time
	Lines               []domain.Line
	InputSnapshotSHA256 string
	ResultSetSHA256     string
	ClosedBy            uuid.UUID
	EventTime           time.Time
	RuleVersion         string
}

// InsertSummary ghi kết quả đóng kỳ (sổ, không bao giờ bị thay).
func InsertSummary(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s Summary) error {
	raw, err := json.Marshal(s.Lines)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_costing.period_summary
	  (id, tenant_id, stream_key, period_id, period_code, close_run_id, from_at, to_at, lines, line_count, input_snapshot_sha256,
	   result_set_sha256, closed_by, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14, $15, $16, $17)`,
		s.ID, tid, "period:"+s.PeriodID.String(), s.PeriodID, s.PeriodCode, s.CloseRunID, s.From, s.To, string(raw), len(s.Lines),
		s.InputSnapshotSHA256, s.ResultSetSHA256, s.ClosedBy, s.EventTime, s.ClosedBy, "close:"+s.CloseRunID.String(), domain.RulePeriodClose)
	return ledger.Classify(err)
}

// SummaryByRun đọc kết quả của một lần đóng kỳ.
func SummaryByRun(ctx context.Context, tx pgx.Tx, run uuid.UUID) (*Summary, error) {
	var s Summary
	var raw string
	err := tx.QueryRow(ctx, `SELECT id, period_id, period_code, close_run_id, from_at, to_at, lines::text, input_snapshot_sha256, result_set_sha256,
	  closed_by, event_time, rule_version FROM mkg_costing.period_summary WHERE close_run_id = $1`, run).
		Scan(&s.ID, &s.PeriodID, &s.PeriodCode, &s.CloseRunID, &s.From, &s.To, &raw, &s.InputSnapshotSHA256, &s.ResultSetSHA256,
			&s.ClosedBy, &s.EventTime, &s.RuleVersion)
	p, err := one(s, err)
	if err != nil || p == nil {
		return p, err
	}
	return p, json.Unmarshal([]byte(raw), &p.Lines)
}

// DBNow trả now() của giao dịch (mốc as_of của ảnh chụp: recorded_at của sổ do DB đặt).
func DBNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var t time.Time
	err := tx.QueryRow(ctx, "SELECT now()").Scan(&t)
	return t.UTC(), err
}

func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.String())
	}
	return out
}
