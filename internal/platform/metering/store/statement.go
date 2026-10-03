package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Job là yêu cầu lập bảng kê (bảng vận hành, mẫu §12).
type Job struct {
	ID               uuid.UUID
	Period           string
	ClientID         *uuid.UUID
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	RequestedBy      uuid.UUID
	RequestedAt      time.Time
	IdemKey          string
	Status           string
	StatementIDs     []uuid.UUID
	Error            *string
	CompletedAt      *time.Time
}

const jobCols = `id, period, client_id, supersedes_id, correction_reason, requested_by, requested_at, idem_key, status, statement_ids::text[], error, completed_at`

func scanJob(r pgx.Row) (*Job, error) {
	var j Job
	var sids []string
	err := r.Scan(&j.ID, &j.Period, &j.ClientID, &j.SupersedesID, &j.CorrectionReason, &j.RequestedBy, &j.RequestedAt, &j.IdemKey, &j.Status,
		&sids, &j.Error, &j.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, s := range sids {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		j.StatementIDs = append(j.StatementIDs, id)
	}
	return &j, nil
}

// InsertJob ghi yêu cầu lập bảng kê.
func InsertJob(ctx context.Context, tx pgx.Tx, tid uuid.UUID, j *Job) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement_job (id, tenant_id, period, client_id, supersedes_id, correction_reason,
	    requested_by, requested_at, idem_key, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'queued')`,
		j.ID, tid, j.Period, j.ClientID, j.SupersedesID, j.CorrectionReason, j.RequestedBy, j.RequestedAt, j.IdemKey)
	j.Status = "queued"
	return err
}

// JobByIdem đọc job theo Idempotency-Key.
func JobByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Job, error) {
	return scanJob(tx.QueryRow(ctx, `SELECT `+jobCols+` FROM pf_metering.statement_job WHERE idem_key = $1`, idem))
}

// JobByID đọc job (forUpdate → khóa dòng cho worker).
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Job, error) {
	q := `SELECT ` + jobCols + ` FROM pf_metering.statement_job WHERE id = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	return scanJob(tx.QueryRow(ctx, q, id))
}

// CompleteJob đánh dấu job xong.
func CompleteJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, statements []uuid.UUID, errText *string, at time.Time) error {
	status := "completed"
	if errText != nil {
		status = "failed"
	}
	sids := make([]string, 0, len(statements))
	for _, s := range statements {
		sids = append(sids, s.String())
	}
	_, err := tx.Exec(ctx, `UPDATE pf_metering.statement_job SET status = $2, statement_ids = $3::text[]::uuid[], error = $4, completed_at = $5 WHERE id = $1`,
		id, status, sids, errText, at)
	return err
}

// ClientsWithUsage trả khách hàng có dòng sổ trong stream của kỳ.
func ClientsWithUsage(ctx context.Context, tx pgx.Tx, period string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT client_id FROM pf_metering.usage_event WHERE stream_key LIKE 'client:%:' || $1 ORDER BY client_id`, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Aggregate là tổng của một stream sử dụng đến lastSeq (bao gồm).
type Aggregate struct {
	Lines          []AggLine
	FirstSeq       int64
	LastSeq        int64
	LastHash       []byte
	ClientTenantID uuid.UUID
	Rules          []string
}

// AggLine là tổng (sản phẩm × lớp trạng thái).
type AggLine struct {
	Product     string
	StatusClass string
	Calls       int64
	Units       int64
}

// AggregateStream gộp dòng sổ của stream có seq ≤ upTo (upTo ≤ 0 = mọi dòng hiện có). Rỗng → nil.
func AggregateStream(ctx context.Context, tx pgx.Tx, stream string, upTo int64) (*Aggregate, error) {
	var a Aggregate
	var first, last *int64
	err := tx.QueryRow(ctx, `SELECT min(seq), max(seq) FROM pf_metering.usage_event WHERE stream_key = $1 AND ($2::bigint <= 0 OR seq <= $2)`,
		stream, upTo).Scan(&first, &last)
	if err != nil {
		return nil, err
	}
	if first == nil {
		return nil, nil
	}
	a.FirstSeq, a.LastSeq = *first, *last
	if err := tx.QueryRow(ctx, `SELECT row_hash, client_tenant_id FROM pf_metering.usage_event WHERE stream_key = $1 AND seq = $2`,
		stream, a.LastSeq).Scan(&a.LastHash, &a.ClientTenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT product, (status / 100)::text || 'xx', count(*), coalesce(sum(units), 0)::bigint
	  FROM pf_metering.usage_event WHERE stream_key = $1 AND seq <= $2 GROUP BY 1, 2 ORDER BY 1, 2`, stream, a.LastSeq)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l AggLine
		if err := rows.Scan(&l.Product, &l.StatusClass, &l.Calls, &l.Units); err != nil {
			rows.Close()
			return nil, err
		}
		a.Lines = append(a.Lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rr, err := tx.Query(ctx, `SELECT DISTINCT rule_version FROM pf_metering.usage_event WHERE stream_key = $1 AND seq <= $2 ORDER BY 1`, stream, a.LastSeq)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	for rr.Next() {
		var s string
		if err := rr.Scan(&s); err != nil {
			return nil, err
		}
		a.Rules = append(a.Rules, s)
	}
	return &a, rr.Err()
}

// SnapshotRow là ảnh chụp bất biến của bảng kê.
type SnapshotRow struct {
	StatementID    uuid.UUID
	JobID          uuid.UUID
	ClientID       uuid.UUID
	Period         string
	StreamKey      string
	FirstSeq       int64
	LastSeq        int64
	ChainHeadHash  string
	Body           []byte
	SnapshotSHA256 string
}

// InsertSnapshot ghi ảnh chụp.
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s SnapshotRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement_snapshot (statement_id, tenant_id, job_id, client_id, period, stream_key,
	    first_seq, last_seq, chain_head_hash, body, snapshot_sha256) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		s.StatementID, tid, s.JobID, s.ClientID, s.Period, s.StreamKey, s.FirstSeq, s.LastSeq, s.ChainHeadHash, s.Body, s.SnapshotSHA256)
	return err
}

// SnapshotOf đọc ảnh chụp.
func SnapshotOf(ctx context.Context, tx pgx.Tx, statement uuid.UUID) (*SnapshotRow, error) {
	var s SnapshotRow
	err := tx.QueryRow(ctx, `SELECT statement_id, job_id, client_id, period, stream_key, first_seq, last_seq, chain_head_hash, body, snapshot_sha256
	  FROM pf_metering.statement_snapshot WHERE statement_id = $1`, statement).Scan(&s.StatementID, &s.JobID, &s.ClientID, &s.Period,
		&s.StreamKey, &s.FirstSeq, &s.LastSeq, &s.ChainHeadHash, &s.Body, &s.SnapshotSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// Line là một dòng bảng kê (tiền dạng chuỗi thập phân — không float).
type Line struct {
	Product      string
	StatusClass  string
	Calls        int64
	Units        int64
	UnitPrice    *string // numeric(18,4)
	Amount       *string // numeric(18,2)
	PriceVersion *string
}

// InsertLines ghi dòng bảng kê.
func InsertLines(ctx context.Context, tx pgx.Tx, tid, statement uuid.UUID, lines []Line) error {
	for _, l := range lines {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement_line (tenant_id, statement_id, product, status_class, calls, units,
		    unit_price_vnd, amount_vnd, price_version) VALUES ($1,$2,$3,$4,$5,$6,$7::text::numeric,$8::text::numeric,$9)`,
			tid, statement, l.Product, l.StatusClass, l.Calls, l.Units, l.UnitPrice, l.Amount, l.PriceVersion); err != nil {
			return err
		}
	}
	return nil
}

// LinesOf đọc dòng bảng kê (sắp theo sản phẩm, lớp trạng thái).
func LinesOf(ctx context.Context, tx pgx.Tx, statement uuid.UUID) ([]Line, error) {
	rows, err := tx.Query(ctx, `SELECT product, status_class, calls, units, unit_price_vnd::text, amount_vnd::text, price_version
	  FROM pf_metering.statement_line WHERE statement_id = $1 ORDER BY product, status_class`, statement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Line
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.Product, &l.StatusClass, &l.Calls, &l.Units, &l.UnitPrice, &l.Amount, &l.PriceVersion); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// StatementEvent là một dòng sổ bảng kê.
type StatementEvent struct {
	ID               uuid.UUID
	StatementID      uuid.UUID
	Action           string
	ClientID         uuid.UUID
	ClientTenantID   uuid.UUID
	Period           string
	JobID            uuid.UUID
	SnapshotSHA256   string
	FirstSeq         int64
	LastSeq          int64
	ChainHeadHash    string
	RequestedBy      uuid.UUID
	Subscription     *string
	Total            *string
	PriceVersion     *string
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	RuleVersion      string
}

// InsertStatementEvent ghi dòng sổ bảng kê; stream statement:<client>:<kỳ>, idem '<action>:<statement_id>'.
func InsertStatementEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, stream string, e StatementEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement (id, tenant_id, stream_key, statement_id, action, client_id, client_tenant_id,
	    period, job_id, snapshot_sha256, first_seq, last_seq, chain_head_hash, requested_by, subscription_vnd, total_vnd, price_version,
	    event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::text::numeric,$16::text::numeric,$17,$18,$19,$20,$21,$22,$23)`,
		e.ID, tid, stream, e.StatementID, e.Action, e.ClientID, e.ClientTenantID, e.Period, e.JobID, e.SnapshotSHA256, e.FirstSeq, e.LastSeq,
		e.ChainHeadHash, e.RequestedBy, e.Subscription, e.Total, e.PriceVersion, e.EventTime, e.SupersedesID, e.CorrectionReason, e.ActorRef,
		e.Action+":"+e.StatementID.String(), e.RuleVersion)
	return err
}

// Current là bảng chiếu hiện hành của một bảng kê.
type Current struct {
	StatementID      uuid.UUID
	ClientID         uuid.UUID
	ClientTenantID   uuid.UUID
	Period           string
	Status           string
	JobID            uuid.UUID
	SnapshotSHA256   string
	FirstSeq         int64
	LastSeq          int64
	ChainHeadHash    string
	RuleVersion      string
	Subscription     *string
	Total            *string
	PriceVersion     *string
	RequestedBy      uuid.UUID
	RequestedAt      time.Time
	ApprovedBy       *uuid.UUID
	ApprovedAt       *time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	SupersededBy     *uuid.UUID
}

const currentCols = `statement_id, client_id, client_tenant_id, period, status, job_id, snapshot_sha256, first_seq, last_seq, chain_head_hash,
  rule_version, subscription_vnd::text, total_vnd::text, price_version, requested_by, requested_at, approved_by, approved_at, supersedes_id,
  correction_reason, superseded_by`

func scanCurrent(r pgx.Row) (Current, error) {
	var c Current
	err := r.Scan(&c.StatementID, &c.ClientID, &c.ClientTenantID, &c.Period, &c.Status, &c.JobID, &c.SnapshotSHA256, &c.FirstSeq, &c.LastSeq,
		&c.ChainHeadHash, &c.RuleVersion, &c.Subscription, &c.Total, &c.PriceVersion, &c.RequestedBy, &c.RequestedAt, &c.ApprovedBy,
		&c.ApprovedAt, &c.SupersedesID, &c.CorrectionReason, &c.SupersededBy)
	return c, err
}

// InsertCurrent tạo bảng chiếu cho bảng kê mới (draft).
func InsertCurrent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Current) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement_current (tenant_id, statement_id, client_id, client_tenant_id, period, status,
	    job_id, snapshot_sha256, first_seq, last_seq, chain_head_hash, rule_version, subscription_vnd, total_vnd, price_version, requested_by,
	    requested_at, supersedes_id, correction_reason)
	  VALUES ($1,$2,$3,$4,$5,'draft',$6,$7,$8,$9,$10,$11,$12::text::numeric,$13::text::numeric,$14,$15,$16,$17,$18)`,
		tid, c.StatementID, c.ClientID, c.ClientTenantID, c.Period, c.JobID, c.SnapshotSHA256, c.FirstSeq, c.LastSeq, c.ChainHeadHash,
		c.RuleVersion, c.Subscription, c.Total, c.PriceVersion, c.RequestedBy, c.RequestedAt, c.SupersedesID, c.CorrectionReason)
	return err
}

// CurrentOf đọc bảng chiếu (forUpdate → khóa dòng); nil khi không có.
func CurrentOf(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Current, error) {
	q := `SELECT ` + currentCols + ` FROM pf_metering.statement_current WHERE statement_id = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	c, err := scanCurrent(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ActiveStatement trả bảng kê chưa bị thay của (khách hàng, kỳ); nil khi chưa có.
func ActiveStatement(ctx context.Context, tx pgx.Tx, client uuid.UUID, period string) (*Current, error) {
	c, err := scanCurrent(tx.QueryRow(ctx, `SELECT `+currentCols+` FROM pf_metering.statement_current
	  WHERE client_id = $1 AND period = $2 AND superseded_by IS NULL ORDER BY statement_id DESC LIMIT 1`, client, period))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// MarkApproved cập nhật bảng chiếu khi duyệt.
func MarkApproved(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_metering.statement_current SET status = 'approved', approved_by = $2, approved_at = $3, updated_at = now()
	  WHERE statement_id = $1`, id, by, at)
	return err
}

// MarkSuperseded ghi bảng kê thay thế vào bảng chiếu của bảng kê cũ.
func MarkSuperseded(ctx context.Context, tx pgx.Tx, old, by uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_metering.statement_current SET superseded_by = $2, updated_at = now() WHERE statement_id = $1`, old, by)
	return err
}

// StatementFilter lọc danh sách bảng kê.
type StatementFilter struct {
	Period   string
	ClientID uuid.UUID
	After    uuid.UUID
	Limit    int
}

// Statements liệt kê bảng kê (mới nhất trước).
func Statements(ctx context.Context, tx pgx.Tx, f StatementFilter) ([]Current, error) {
	rows, err := tx.Query(ctx, `SELECT `+currentCols+` FROM pf_metering.statement_current
	  WHERE ($1::text = '' OR period = $1::text) AND ($2::uuid IS NULL OR client_id = $2) AND ($3::uuid IS NULL OR statement_id < $3)
	  ORDER BY statement_id DESC LIMIT $4`, f.Period, nullID(f.ClientID), nullID(f.After), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Current
	for rows.Next() {
		c, err := scanCurrent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
