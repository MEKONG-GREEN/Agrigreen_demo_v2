package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// ---------------------------------------------------------------------------
// Job (bảng vận hành, mẫu mục 12)
// ---------------------------------------------------------------------------

// Job là một job dựng / đẩy.
type Job struct {
	TenantID           uuid.UUID
	JobID              uuid.UUID
	Kind               string // build | push
	Status             string // queued | done | failed
	PeriodID           uuid.UUID
	PeriodCode         string
	PeriodFrom         time.Time
	PeriodTo           time.Time
	CloseRunID         uuid.UUID
	ExpectedSourceHash string
	MappingVersion     *int
	MappingVersionID   *uuid.UUID
	JournalID          *uuid.UUID
	Attempt            *int
	FailureCode        *string
	RequestedBy        uuid.UUID
	RequestedAt        time.Time
	CompletedAt        *time.Time
	IdemKey            *string
}

const jobCols = `tenant_id, job_id, kind, status, period_id, period_code, period_from, period_to, close_run_id, expected_source_hash,
  mapping_version, mapping_version_id, journal_id, attempt, failure_code, requested_by, requested_at, completed_at, idem_key`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.TenantID, &j.JobID, &j.Kind, &j.Status, &j.PeriodID, &j.PeriodCode, &j.PeriodFrom, &j.PeriodTo, &j.CloseRunID,
		&j.ExpectedSourceHash, &j.MappingVersion, &j.MappingVersionID, &j.JournalID, &j.Attempt, &j.FailureCode, &j.RequestedBy,
		&j.RequestedAt, &j.CompletedAt, &j.IdemKey)
	return j, err
}

func oneJob(row pgx.Row) (*Job, error) {
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// InsertJob ghi job (status queued).
func InsertJob(ctx context.Context, tx pgx.Tx, j Job) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_acctbridge.job (`+jobCols+`)
	  VALUES ($1, $2, $3, 'queued', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL, $14, $15, NULL, $16)`,
		j.TenantID, j.JobID, j.Kind, j.PeriodID, j.PeriodCode, j.PeriodFrom.UTC(), j.PeriodTo.UTC(), j.CloseRunID, j.ExpectedSourceHash,
		j.MappingVersion, j.MappingVersionID, j.JournalID, j.Attempt, j.RequestedBy, j.RequestedAt.UTC(), j.IdemKey)
	return err
}

// JobByID đọc job; forUpdate khóa dòng.
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Job, error) {
	q := "SELECT " + jobCols + " FROM mkg_acctbridge.job WHERE job_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneJob(tx.QueryRow(ctx, q, id))
}

// JobByIdem tìm job theo khóa idempotency của yêu cầu HTTP.
func JobByIdem(ctx context.Context, tx pgx.Tx, key string) (*Job, error) {
	return oneJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM mkg_acctbridge.job WHERE idem_key = $1", key))
}

// ActiveBuildJob trả job dựng chưa lỗi của (kỳ, lần đóng kỳ, phiên bản ánh xạ) — B-02 idempotent.
func ActiveBuildJob(ctx context.Context, tx pgx.Tx, period, closeRun uuid.UUID, mappingVersion *int) (*Job, error) {
	mv := 0
	if mappingVersion != nil {
		mv = *mappingVersion
	}
	return oneJob(tx.QueryRow(ctx, "SELECT "+jobCols+` FROM mkg_acctbridge.job
	    WHERE kind = 'build' AND status <> 'failed' AND period_id = $1 AND close_run_id = $2 AND coalesce(mapping_version, 0) = $3`,
		period, closeRun, mv))
}

// LatestBuildJob trả job dựng mới nhất của kỳ (đầu vào kỳ cho yêu cầu dựng lại).
func LatestBuildJob(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*Job, error) {
	return oneJob(tx.QueryRow(ctx, "SELECT "+jobCols+` FROM mkg_acctbridge.job WHERE kind = 'build' AND period_id = $1
	    ORDER BY requested_at DESC, job_id DESC LIMIT 1`, period))
}

// FinishJob đánh dấu job xong (failure = nil) hoặc lỗi (mã acctbridge.*).
func FinishJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, journal *uuid.UUID, failure *string, at time.Time) error {
	status := "done"
	if failure != nil {
		status = "failed"
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_acctbridge.job SET status = $2, journal_id = coalesce($3, journal_id), failure_code = $4, completed_at = $5
	  WHERE job_id = $1`, id, status, journal, failure, at.UTC())
	return err
}

// LockPeriod tuần tự hóa dựng / duyệt / đẩy bút toán của một kỳ trong tenant.
func LockPeriod(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_acctbridge.period:"+tid.String()+":"+period.String())
	return err
}

// ---------------------------------------------------------------------------
// Sổ bút toán
// ---------------------------------------------------------------------------

// lineJSON là dạng lưu của một dòng bút toán (jsonb).
type lineJSON struct {
	LineNo      int          `json:"line_no"`
	Account     string       `json:"account"`
	Side        string       `json:"side"`
	Amount      money.Amount `json:"amount"`
	Category    string       `json:"category"`
	Dimension   string       `json:"dimension"`
	Description string       `json:"description"`
}

func encodeLines(ls []port.JournalLine) ([]byte, error) {
	out := make([]lineJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, lineJSON{LineNo: l.LineNo, Account: l.Account, Side: string(l.Side), Amount: l.Amount, Category: string(l.Category),
			Dimension: l.Dimension, Description: l.Description})
	}
	return json.Marshal(out)
}

func decodeLines(b []byte) ([]port.JournalLine, error) {
	var in []lineJSON
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make([]port.JournalLine, 0, len(in))
	for _, l := range in {
		out = append(out, port.JournalLine{LineNo: l.LineNo, Account: l.Account, Side: port.Side(l.Side), Amount: l.Amount,
			Category: costingport.CostCategory(l.Category), Dimension: l.Dimension, Description: l.Description})
	}
	return out, nil
}

// JournalEvent là một dòng sổ journal_event.
type JournalEvent struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	Seq                 int64
	JournalID           uuid.UUID
	PeriodID            uuid.UUID
	Kind                string
	Version             int
	PeriodCode          string
	CloseRunID          uuid.UUID
	SourceHash          string
	LinesSHA256         string
	MappingVersion      int
	Currency            string
	Lines               []port.JournalLine // chỉ 'built'
	LineCount           int
	TotalDebit          money.Amount
	TotalCredit         money.Amount
	BuiltBy             uuid.UUID
	BuildJobID          *uuid.UUID
	SupersedesJournalID *uuid.UUID
	SupersededBy        *uuid.UUID
	Adapter             *string
	ExternalRef         *string
	ArtifactSHA256      *string
	Attempt             *int
	FailureCode         *string
	EventTime           time.Time
	RecordedAt          time.Time
	SupersedesID        *uuid.UUID
	CorrectionReason    *string
	ActorRef            uuid.UUID
	IdemKey             string
}

const journalEventCols = `id, tenant_id, seq, journal_id, period_id, kind, version, period_code, close_run_id, source_hash, lines_sha256,
  mapping_version, currency, lines, line_count, total_debit::text, total_credit::text, built_by, build_job_id, supersedes_journal_id, superseded_by,
  adapter, external_ref, artifact_sha256, attempt, failure_code, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, idem_key`

// InsertJournalEvent ghi sổ bút toán (stream period:<period_id>).
func InsertJournalEvent(ctx context.Context, tx pgx.Tx, e *JournalEvent) error {
	var lines *string // jsonb: truyền chuỗi (pgx mã hóa []byte như bytea)
	if e.Kind == domain.StatusBuilt {
		b, err := encodeLines(e.Lines)
		if err != nil {
			return err
		}
		s := string(b)
		lines = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_acctbridge.journal_event
	    (id, tenant_id, stream_key, journal_id, period_id, kind, version, period_code, close_run_id, source_hash, lines_sha256, mapping_version,
	     currency, lines, line_count, total_debit, total_credit, built_by, build_job_id, supersedes_journal_id, superseded_by, adapter,
	     external_ref, artifact_sha256, attempt, failure_code, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::numeric, $17::numeric, $18, $19, $20, $21, $22, $23, $24,
	          $25, $26, $27, $28, $29, $30, $31, $32)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "period:"+e.PeriodID.String(), e.JournalID, e.PeriodID, e.Kind, e.Version, e.PeriodCode, e.CloseRunID, e.SourceHash,
		e.LinesSHA256, e.MappingVersion, e.Currency, lines, e.LineCount, e.TotalDebit.String(), e.TotalCredit.String(), e.BuiltBy, e.BuildJobID,
		e.SupersedesJournalID, e.SupersededBy, e.Adapter, e.ExternalRef, e.ArtifactSHA256, e.Attempt, e.FailureCode, e.EventTime.UTC(),
		e.SupersedesID, e.CorrectionReason, e.ActorRef, e.IdemKey, domain.RuleJournal).Scan(&e.Seq, &e.RecordedAt)
}

func scanJournalEvent(row pgx.Row) (JournalEvent, error) {
	var e JournalEvent
	var lines []byte
	var dr, cr string
	err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.JournalID, &e.PeriodID, &e.Kind, &e.Version, &e.PeriodCode, &e.CloseRunID, &e.SourceHash,
		&e.LinesSHA256, &e.MappingVersion, &e.Currency, &lines, &e.LineCount, &dr, &cr, &e.BuiltBy, &e.BuildJobID, &e.SupersedesJournalID,
		&e.SupersededBy, &e.Adapter, &e.ExternalRef, &e.ArtifactSHA256, &e.Attempt, &e.FailureCode, &e.EventTime, &e.RecordedAt, &e.SupersedesID,
		&e.CorrectionReason, &e.ActorRef, &e.IdemKey)
	if err != nil {
		return e, err
	}
	if e.TotalDebit, err = money.ParseAmount(dr); err != nil {
		return e, err
	}
	if e.TotalCredit, err = money.ParseAmount(cr); err != nil {
		return e, err
	}
	if lines != nil {
		e.Lines, err = decodeLines(lines)
	}
	return e, err
}

// JournalEventByIdem tìm sự kiện bút toán theo idem_key.
func JournalEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*JournalEvent, error) {
	e, err := scanJournalEvent(tx.QueryRow(ctx, "SELECT "+journalEventCols+" FROM mkg_acctbridge.journal_event WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// JournalEvents đọc toàn bộ sổ bút toán của tenant theo thứ tự ghi (dựng lại bảng chiếu, R-22).
func JournalEvents(ctx context.Context, tx pgx.Tx) ([]JournalEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+journalEventCols+" FROM mkg_acctbridge.journal_event ORDER BY recorded_at, stream_key, seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalEvent
	for rows.Next() {
		e, err := scanJournalEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BuiltEvent trả sự kiện 'built' của bút toán (supersedes_id của bản dựng lại trỏ về nó).
func BuiltEvent(ctx context.Context, tx pgx.Tx, journal uuid.UUID) (*JournalEvent, error) {
	e, err := scanJournalEvent(tx.QueryRow(ctx, "SELECT "+journalEventCols+` FROM mkg_acctbridge.journal_event
	    WHERE journal_id = $1 AND kind = 'built'`, journal))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------------------------------------------------------------------
// Bảng chiếu bút toán
// ---------------------------------------------------------------------------

// Journal là bảng chiếu journal_current.
type Journal struct {
	TenantID            uuid.UUID
	JournalID           uuid.UUID
	PeriodID            uuid.UUID
	PeriodCode          string
	Version             int
	Status              string
	CloseRunID          uuid.UUID
	SourceHash          string
	LinesSHA256         string
	MappingVersion      int
	Currency            string
	Lines               []port.JournalLine
	LineCount           int
	TotalDebit          money.Amount
	TotalCredit         money.Amount
	BuiltBy             uuid.UUID
	BuiltAt             time.Time
	BuildJobID          uuid.UUID
	SupersedesJournalID *uuid.UUID
	SupersededBy        *uuid.UUID
	ApprovedBy          *uuid.UUID
	ApprovedAt          *time.Time
	Adapter             *string
	ExternalRef         *string
	ArtifactSHA256      *string
	PushedAt            *time.Time
	PushAttempts        int
	LastFailureCode     *string
	NextRetryAt         *time.Time
	UpdatedAt           time.Time
}

const journalCols = `tenant_id, journal_id, period_id, period_code, version, status, close_run_id, source_hash, lines_sha256, mapping_version,
  currency, lines, line_count, total_debit::text, total_credit::text, built_by, built_at, build_job_id, supersedes_journal_id, superseded_by,
  approved_by, approved_at, adapter, external_ref, artifact_sha256, pushed_at, push_attempts, last_failure_code, next_retry_at, updated_at`

func scanJournal(row pgx.Row) (Journal, error) {
	var j Journal
	var lines []byte
	var dr, cr string
	err := row.Scan(&j.TenantID, &j.JournalID, &j.PeriodID, &j.PeriodCode, &j.Version, &j.Status, &j.CloseRunID, &j.SourceHash, &j.LinesSHA256,
		&j.MappingVersion, &j.Currency, &lines, &j.LineCount, &dr, &cr, &j.BuiltBy, &j.BuiltAt, &j.BuildJobID, &j.SupersedesJournalID,
		&j.SupersededBy, &j.ApprovedBy, &j.ApprovedAt, &j.Adapter, &j.ExternalRef, &j.ArtifactSHA256, &j.PushedAt, &j.PushAttempts,
		&j.LastFailureCode, &j.NextRetryAt, &j.UpdatedAt)
	if err != nil {
		return j, err
	}
	if j.TotalDebit, err = money.ParseAmount(dr); err != nil {
		return j, err
	}
	if j.TotalCredit, err = money.ParseAmount(cr); err != nil {
		return j, err
	}
	j.Lines, err = decodeLines(lines)
	return j, err
}

func oneJournal(row pgx.Row) (*Journal, error) {
	j, err := scanJournal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// GetJournal đọc bút toán; forUpdate khóa dòng.
func GetJournal(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Journal, error) {
	q := "SELECT " + journalCols + " FROM mkg_acctbridge.journal_current WHERE journal_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneJournal(tx.QueryRow(ctx, q, id))
}

// ActiveJournal trả bút toán hiệu lực (không superseded) của kỳ; forUpdate khóa dòng.
func ActiveJournal(ctx context.Context, tx pgx.Tx, period uuid.UUID, forUpdate bool) (*Journal, error) {
	q := "SELECT " + journalCols + " FROM mkg_acctbridge.journal_current WHERE period_id = $1 AND status <> 'superseded'"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneJournal(tx.QueryRow(ctx, q, period))
}

// NextJournalVersion trả số phiên bản bút toán kế tiếp của kỳ.
func NextJournalVersion(ctx context.Context, tx pgx.Tx, period uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM mkg_acctbridge.journal_current WHERE period_id = $1", period).Scan(&n)
	return n, err
}

// JournalFilter lọc danh sách bút toán.
type JournalFilter struct {
	PeriodID *uuid.UUID
	Status   string
	Before   *uuid.UUID // con trỏ: journal_id (UUIDv7) của phần tử cuối trang trước
	Limit    int
}

// Journals liệt kê bút toán mới → cũ (journal_id UUIDv7).
func Journals(ctx context.Context, tx pgx.Tx, f JournalFilter) ([]Journal, error) {
	q := "SELECT " + journalCols + " FROM mkg_acctbridge.journal_current WHERE true"
	args := []any{}
	if f.PeriodID != nil {
		args = append(args, *f.PeriodID)
		q += fmt.Sprintf(" AND period_id = $%d", len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		q += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		q += fmt.Sprintf(" AND journal_id < $%d", len(args))
	}
	args = append(args, f.Limit)
	q += fmt.Sprintf(" ORDER BY journal_id DESC LIMIT $%d", len(args))
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Journal
	for rows.Next() {
		j, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ApplyJournalEvent cập nhật bảng chiếu bút toán theo một sự kiện sổ (cùng giao dịch — R-21; cũng là
// hàm dựng lại R-22). Dòng 'pushed' không bao giờ được sửa (trigger DB).
func ApplyJournalEvent(ctx context.Context, tx pgx.Tx, e JournalEvent) error {
	var err error
	switch e.Kind {
	case domain.StatusBuilt:
		var lines []byte
		if lines, err = encodeLines(e.Lines); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO mkg_acctbridge.journal_current
		    (tenant_id, journal_id, period_id, period_code, version, status, close_run_id, source_hash, lines_sha256, mapping_version, currency,
		     lines, line_count, total_debit, total_credit, built_by, built_at, build_job_id, supersedes_journal_id)
		  VALUES ($1, $2, $3, $4, $5, 'built', $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15, $16, $17, $18)`,
			e.TenantID, e.JournalID, e.PeriodID, e.PeriodCode, e.Version, e.CloseRunID, e.SourceHash, e.LinesSHA256, e.MappingVersion, e.Currency,
			string(lines), e.LineCount, e.TotalDebit.String(), e.TotalCredit.String(), e.BuiltBy, e.EventTime.UTC(), e.BuildJobID, e.SupersedesJournalID)
	case domain.StatusApproved:
		_, err = tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current SET status = 'approved', approved_by = $2, approved_at = $3, updated_at = now()
		  WHERE journal_id = $1`, e.JournalID, e.ActorRef, e.EventTime.UTC())
	case domain.StatusPushed:
		_, err = tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current SET status = 'pushed', adapter = $2, external_ref = $3, artifact_sha256 = $4,
		    pushed_at = $5, next_retry_at = NULL, last_failure_code = NULL, push_attempts = greatest(push_attempts, $6), updated_at = now()
		  WHERE journal_id = $1`, e.JournalID, e.Adapter, e.ExternalRef, e.ArtifactSHA256, e.EventTime.UTC(), e.Attempt)
	case domain.StatusPushFailed:
		_, err = tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current SET status = 'push_failed', last_failure_code = $2, next_retry_at = NULL,
		    push_attempts = greatest(push_attempts, $3), updated_at = now() WHERE journal_id = $1`, e.JournalID, e.FailureCode, e.Attempt)
	case domain.StatusSuperseded:
		_, err = tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current SET status = 'superseded', superseded_by = $2, next_retry_at = NULL,
		    updated_at = now() WHERE journal_id = $1`, e.JournalID, e.SupersededBy)
	default:
		return fmt.Errorf("acctbridge: loại sự kiện bút toán lạ %q", e.Kind)
	}
	return err
}

// ScheduleRetry ghi hạn thử lại (lỗi tạm thời) lên bảng chiếu — trạng thái vẫn approved.
func ScheduleRetry(ctx context.Context, tx pgx.Tx, journal uuid.UUID, attempts int, code string, at *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current SET push_attempts = $2,
	    last_failure_code = CASE WHEN status = 'approved' THEN $3 ELSE last_failure_code END, next_retry_at = $4, updated_at = now()
	  WHERE journal_id = $1`, journal, attempts, code, at)
	return err
}

// DueRetries khóa (SKIP LOCKED) bút toán approved có hạn thử lại ≤ now.
func DueRetries(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]Journal, error) {
	rows, err := tx.Query(ctx, "SELECT "+journalCols+` FROM mkg_acctbridge.journal_current
	    WHERE status = 'approved' AND next_retry_at IS NOT NULL AND next_retry_at <= $1 ORDER BY next_retry_at LIMIT $2 FOR UPDATE SKIP LOCKED`,
		now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Journal
	for rows.Next() {
		j, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClearRetry xóa hạn thử lại sau khi đã tạo job đẩy mới.
func ClearRetry(ctx context.Context, tx pgx.Tx, journal uuid.UUID) error {
	_, err := tx.Exec(ctx, "UPDATE mkg_acctbridge.journal_current SET next_retry_at = NULL, updated_at = now() WHERE journal_id = $1", journal)
	return err
}

// ClearProjections xóa bảng chiếu để dựng lại từ sổ (R-22) — chỉ bảng chiếu, không bao giờ sổ.
func ClearProjections(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_acctbridge.journal_current"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM mkg_acctbridge.mapping_current")
	return err
}

// RestoreRetryState dựng lại số lần đẩy, mã lỗi cuối và hạn thử lại từ bảng vận hành push_attempt.
func RestoreRetryState(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_acctbridge.journal_current j SET push_attempts = a.attempts,
	    last_failure_code = CASE WHEN j.status = 'approved' THEN a.last_code ELSE coalesce(j.last_failure_code, a.last_code) END,
	    next_retry_at = CASE WHEN j.status = 'approved' THEN a.next_retry_at ELSE NULL END
	  FROM (SELECT DISTINCT ON (journal_id) journal_id, attempt AS attempts, error_code AS last_code, next_retry_at
	        FROM mkg_acctbridge.push_attempt ORDER BY journal_id, attempt DESC) a
	  WHERE a.journal_id = j.journal_id AND j.status <> 'pushed'`)
	return err
}

// ---------------------------------------------------------------------------
// Lần đẩy (bảng vận hành)
// ---------------------------------------------------------------------------

// Attempt là một lần đẩy.
type Attempt struct {
	TenantID       uuid.UUID
	ID             uuid.UUID
	JournalID      uuid.UUID
	JobID          uuid.UUID
	Attempt        int
	Adapter        string
	IdempotencyKey string
	Outcome        string // succeeded | unavailable | rejected
	ErrorCode      *string
	ExternalRef    *string
	ArtifactSHA256 *string
	NextRetryAt    *time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

// InsertAttempt ghi một lần đẩy.
func InsertAttempt(ctx context.Context, tx pgx.Tx, a Attempt) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_acctbridge.push_attempt
	    (tenant_id, id, journal_id, job_id, attempt, adapter, idempotency_key, outcome, error_code, external_ref, artifact_sha256, next_retry_at,
	     started_at, finished_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		a.TenantID, a.ID, a.JournalID, a.JobID, a.Attempt, a.Adapter, a.IdempotencyKey, a.Outcome, a.ErrorCode, a.ExternalRef, a.ArtifactSHA256,
		a.NextRetryAt, a.StartedAt.UTC(), a.FinishedAt.UTC())
	return err
}

// Attempts liệt kê các lần đẩy của bút toán theo thứ tự.
func Attempts(ctx context.Context, tx pgx.Tx, journal uuid.UUID) ([]Attempt, error) {
	rows, err := tx.Query(ctx, `SELECT tenant_id, id, journal_id, job_id, attempt, adapter, idempotency_key, outcome, error_code, external_ref,
	    artifact_sha256, next_retry_at, started_at, finished_at FROM mkg_acctbridge.push_attempt WHERE journal_id = $1 ORDER BY attempt`, journal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attempt
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.TenantID, &a.ID, &a.JournalID, &a.JobID, &a.Attempt, &a.Adapter, &a.IdempotencyKey, &a.Outcome, &a.ErrorCode,
			&a.ExternalRef, &a.ArtifactSHA256, &a.NextRetryAt, &a.StartedAt, &a.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
