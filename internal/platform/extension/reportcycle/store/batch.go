// Package store: SQL của vòng đời báo cáo KN v7.7 (migration pf 000068–000071, vai extension_app). Mọi hàm nhận
// pgx.Tx đã ở ngữ cảnh tenant (RLS); không lọc tenant trong SQL ngoài tham số tenant_id khi INSERT.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RuleVersion là rule_version của các sổ vòng đời báo cáo.
const RuleVersion = "extension.reportcycle.v1"

// BatchForm là một mẫu của đợt (phiên bản ghim lúc phát hành).
type BatchForm struct {
	TemplateID  uuid.UUID `json:"template_id"`
	VersionID   uuid.UUID `json:"version_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Version     int       `json:"version"`
	PeriodBlock bool      `json:"period_block"`
}

// Batch là một dòng report_batch.
type Batch struct {
	ID                 uuid.UUID
	DepartmentCode     string
	ProvinceCode       string
	ReportType         string
	PeriodType         string
	SeasonLabel        string
	From, To           time.Time
	PeriodRule         string
	DeadlineOffsetDays int
	DeadlineTime       string
	CloseAfterDays     int
	Note               *string
	Forms              []BatchForm
	Communes           []string
	Status             string
	PeriodCount        int
	TaskCount          int
	IssuedBy           uuid.UUID
	IssuedAt           time.Time
	WithdrawnBy        *uuid.UUID
	WithdrawnAt        *time.Time
	WithdrawReason     *string
	LastEventID        uuid.UUID
	UpdatedAt          time.Time
}

const batchCols = `id, department_code, province_code, report_type, period_type, season_label, from_date, to_date, period_rule,
  deadline_offset_days, deadline_time, close_after_days, note, forms::text, communes, status, period_count, task_count, issued_by, issued_at,
  withdrawn_by, withdrawn_at, withdraw_reason, last_event_id, updated_at`

func scanBatch(row pgx.Row) (Batch, error) {
	var b Batch
	var forms string
	err := row.Scan(&b.ID, &b.DepartmentCode, &b.ProvinceCode, &b.ReportType, &b.PeriodType, &b.SeasonLabel, &b.From, &b.To, &b.PeriodRule,
		&b.DeadlineOffsetDays, &b.DeadlineTime, &b.CloseAfterDays, &b.Note, &forms, &b.Communes, &b.Status, &b.PeriodCount, &b.TaskCount,
		&b.IssuedBy, &b.IssuedAt, &b.WithdrawnBy, &b.WithdrawnAt, &b.WithdrawReason, &b.LastEventID, &b.UpdatedAt)
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(forms), &b.Forms)
	return b, err
}

// InsertBatch ghi bảng chiếu đợt mới.
func InsertBatch(ctx context.Context, tx pgx.Tx, tid uuid.UUID, b Batch) error {
	forms, err := json.Marshal(b.Forms)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.report_batch (tenant_id, id, department_code, province_code, report_type, period_type,
	    season_label, from_date, to_date, period_rule, deadline_offset_days, deadline_time, close_after_days, note, forms, communes, status,
	    period_count, task_count, issued_by, issued_at, last_event_id, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, $12, $13, $14, $15::jsonb, $16::text[], $17, $18, $19, $20, $21, $22, $21)`,
		tid, b.ID, b.DepartmentCode, b.ProvinceCode, b.ReportType, b.PeriodType, b.SeasonLabel, b.From.Format(time.DateOnly), b.To.Format(time.DateOnly),
		b.PeriodRule, b.DeadlineOffsetDays, b.DeadlineTime, b.CloseAfterDays, b.Note, string(forms), b.Communes, b.Status, b.PeriodCount, b.TaskCount,
		b.IssuedBy, b.IssuedAt.UTC(), b.LastEventID)
	return err
}

// BatchByID đọc (khóa nếu lock) một đợt; không có → nil.
func BatchByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Batch, error) {
	q := "SELECT " + batchCols + " FROM pf_extension.report_batch WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	b, err := scanBatch(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BatchFilter lọc danh sách đợt.
type BatchFilter struct {
	AllProvinces bool
	Province     string
	Department   string // rỗng = mọi phòng ban (Admin)
	ReportType   string
	Before       *uuid.UUID // con trỏ: id < before
	Limit        int
}

// ListBatches trả đợt mới nhất trước.
func ListBatches(ctx context.Context, tx pgx.Tx, f BatchFilter) ([]Batch, error) {
	rows, err := tx.Query(ctx, "SELECT "+batchCols+` FROM pf_extension.report_batch
	  WHERE ($1::boolean OR province_code = $2) AND ($3 = '' OR department_code = $3) AND ($4 = '' OR report_type = $4)
	    AND ($5::uuid IS NULL OR id < $5::uuid)
	  ORDER BY id DESC LIMIT $6`, f.AllProvinces, f.Province, f.Department, f.ReportType, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Batch, error) { return scanBatch(r) })
}

// PreviousBatch trả đợt đã phát hành gần nhất (trước id) của phòng ban × tỉnh có mẫu templateID ("Lấy từ đợt trước").
func PreviousBatch(ctx context.Context, tx pgx.Tx, province, dept string, templateID uuid.UUID, before uuid.UUID) (*Batch, error) {
	b, err := scanBatch(tx.QueryRow(ctx, "SELECT "+batchCols+` FROM pf_extension.report_batch
	  WHERE province_code = $1 AND department_code = $2 AND status = 'issued' AND forms @> $3::jsonb AND id < $4::uuid
	  ORDER BY id DESC LIMIT 1`, province, dept, `[{"template_id":"`+templateID.String()+`"}]`, before))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// WithdrawBatch đánh dấu đợt đã thu hồi (bảng chiếu; sự kiện ghi ở sổ).
func WithdrawBatch(ctx context.Context, tx pgx.Tx, id, actor, eventID uuid.UUID, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_batch SET status = 'withdrawn', withdrawn_by = $2, withdrawn_at = $3, withdraw_reason = $4,
	    last_event_id = $5, updated_at = $3 WHERE id = $1`, id, actor, at.UTC(), reason, eventID)
	return err
}

// TouchBatch ghi sự kiện cuối của đợt.
func TouchBatch(ctx context.Context, tx pgx.Tx, id, eventID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_batch SET last_event_id = $2, updated_at = $3 WHERE id = $1`, id, eventID, at.UTC())
	return err
}

// BatchEvent là một dòng sổ report_batch_event.
type BatchEvent struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	Action         string
	DepartmentCode string
	ProvinceCode   string
	Payload        json.RawMessage
	Reason         *string
	OnBehalfOf     *uuid.UUID
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	Seq            int64
	RecordedAt     time.Time
}

// InsertBatchEvent ghi sổ đợt.
func InsertBatchEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *BatchEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.report_batch_event (id, tenant_id, stream_key, batch_id, action, department_code, province_code,
	    payload, reason, on_behalf_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		e.ID, tid, "batch:"+e.BatchID.String(), e.BatchID, e.Action, e.DepartmentCode, e.ProvinceCode, rawOr(e.Payload, "{}"), e.Reason, e.OnBehalfOf,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// BatchEvents trả nhật ký đợt (mới nhất trước).
func BatchEvents(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) ([]BatchEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, batch_id, action, department_code, province_code, payload::text, reason, on_behalf_of, event_time, actor_ref,
	    idem_key, seq, recorded_at FROM pf_extension.report_batch_event WHERE batch_id = $1::uuid ORDER BY seq DESC`, batchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BatchEvent, error) {
		var e BatchEvent
		var p string
		err := r.Scan(&e.ID, &e.BatchID, &e.Action, &e.DepartmentCode, &e.ProvinceCode, &p, &e.Reason, &e.OnBehalfOf, &e.EventTime, &e.ActorRef,
			&e.IdemKey, &e.Seq, &e.RecordedAt)
		e.Payload = json.RawMessage(p)
		return e, err
	})
}

func rawOr(b []byte, def string) string {
	if len(b) == 0 {
		return def
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Kỳ
// ---------------------------------------------------------------------------

// Period là một dòng report_period.
type Period struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	Slot           int
	Label          string
	RangeLabel     string
	ISOWeek        *int
	From, To       time.Time
	DueAt          time.Time
	AutoCloseAt    time.Time
	Status         string
	ClosedAt       *time.Time
	ClosedBy       *uuid.UUID
	CloseMode      *string
	DepartmentCode string
	ProvinceCode   string
}

const periodCols = `id, batch_id, slot, label, range_label, iso_week, from_date, to_date, due_at, auto_close_at, status, closed_at, closed_by,
  close_mode, department_code, province_code`

func scanPeriod(row pgx.Row) (Period, error) {
	var p Period
	err := row.Scan(&p.ID, &p.BatchID, &p.Slot, &p.Label, &p.RangeLabel, &p.ISOWeek, &p.From, &p.To, &p.DueAt, &p.AutoCloseAt, &p.Status,
		&p.ClosedAt, &p.ClosedBy, &p.CloseMode, &p.DepartmentCode, &p.ProvinceCode)
	return p, err
}

// InsertPeriod ghi một kỳ.
func InsertPeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p Period) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.report_period (tenant_id, id, batch_id, slot, label, range_label, iso_week, from_date, to_date,
	    due_at, auto_close_at, status, department_code, province_code)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, 'open', $12, $13)`,
		tid, p.ID, p.BatchID, p.Slot, p.Label, p.RangeLabel, p.ISOWeek, p.From.Format(time.DateOnly), p.To.Format(time.DateOnly), p.DueAt.UTC(),
		p.AutoCloseAt.UTC(), p.DepartmentCode, p.ProvinceCode)
	return err
}

// PeriodByID đọc (khóa nếu lock) một kỳ; không có → nil.
func PeriodByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Period, error) {
	q := "SELECT " + periodCols + " FROM pf_extension.report_period WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	p, err := scanPeriod(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PeriodsOfBatch trả các kỳ của đợt theo thứ tự khe.
func PeriodsOfBatch(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) ([]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+periodCols+" FROM pf_extension.report_period WHERE batch_id = $1::uuid ORDER BY slot", batchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// PeriodsByIDs trả các kỳ theo id.
func PeriodsByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+periodCols+" FROM pf_extension.report_period WHERE id = ANY($1::uuid[])", ids)
	if err != nil {
		return nil, err
	}
	ps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Period, len(ps))
	for _, p := range ps {
		out[p.ID] = p
	}
	return out, nil
}

// SetPeriodDue đổi hạn nộp và giờ chốt tự động của kỳ.
func SetPeriodDue(ctx context.Context, tx pgx.Tx, id uuid.UUID, due, autoClose, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_period SET due_at = $2, auto_close_at = $3, updated_at = $4 WHERE id = $1`,
		id, due.UTC(), autoClose.UTC(), at.UTC())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE pf_extension.report_task SET due_at = $2, updated_at = $3 WHERE period_id = $1`, id, due.UTC(), at.UTC())
	return err
}

// ClosePeriod chiếu sự kiện chốt kỳ.
func ClosePeriod(ctx context.Context, tx pgx.Tx, id, actor uuid.UUID, mode string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_period SET status = 'closed', closed_at = $2, closed_by = $3, close_mode = $4, updated_at = $2
	  WHERE id = $1`, id, at.UTC(), actor, mode)
	return err
}

// DuePeriods trả kỳ mở đã tới giờ chốt tự động (job).
func DuePeriods(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+periodCols+` FROM pf_extension.report_period p
	  WHERE p.status = 'open' AND p.auto_close_at <= $1
	    AND EXISTS (SELECT 1 FROM pf_extension.report_batch b WHERE b.id = p.batch_id AND b.status = 'issued')
	  ORDER BY auto_close_at LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// OpenPeriodsOf trả kỳ còn mở (của đợt chưa thu hồi) — cho job tự nhắc.
func OpenPeriodsOf(ctx context.Context, tx pgx.Tx, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+periodCols+` FROM pf_extension.report_period p
	  WHERE p.status = 'open' AND EXISTS (SELECT 1 FROM pf_extension.report_batch b WHERE b.id = p.batch_id AND b.status = 'issued')
	  ORDER BY due_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// PeriodFilter lọc kỳ cho dashboard/báo cáo.
type PeriodFilter struct {
	AllProvinces bool
	Province     string
	Department   string
	ReportType   string
	BatchID      *uuid.UUID
	Limit        int
}

// ListPeriods trả kỳ của đợt đã phát hành (mới nhất trước).
func ListPeriods(ctx context.Context, tx pgx.Tx, f PeriodFilter) ([]Period, error) {
	rows, err := tx.Query(ctx, "SELECT "+prefixed("p.", periodCols)+` FROM pf_extension.report_period p
	  JOIN pf_extension.report_batch b ON b.id = p.batch_id AND b.status = 'issued'
	  WHERE ($1::boolean OR p.province_code = $2) AND ($3 = '' OR p.department_code = $3) AND ($4 = '' OR b.report_type = $4)
	    AND ($5::uuid IS NULL OR p.batch_id = $5::uuid)
	  ORDER BY p.from_date DESC, p.id DESC LIMIT $6`, f.AllProvinces, f.Province, f.Department, f.ReportType, f.BatchID, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

func prefixed(p, cols string) string {
	out := ""
	field := ""
	flush := func() {
		if field != "" {
			if out != "" {
				out += ", "
			}
			out += p + field
			field = ""
		}
	}
	for _, r := range cols {
		switch r {
		case ',':
			flush()
		case ' ', '\n', '\t':
		default:
			field += string(r)
		}
	}
	flush()
	return out
}

// OpenPeriodsForCommunes đếm kỳ chưa chốt có công việc ở các xã (bảng Ảnh hưởng, FN-AD-04.02).
func OpenPeriodsForCommunes(ctx context.Context, tx pgx.Tx, communes []string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT t.period_id) FROM pf_extension.report_task t
	  JOIN pf_extension.report_period p ON p.id = t.period_id
	  WHERE p.status = 'open' AND NOT t.withdrawn AND t.commune_code = ANY($1::text[])`, communes).Scan(&n)
	return n, err
}

// DepartmentRecords đếm công việc báo cáo của phòng ban (cột "dữ liệu" khi "xóa" phòng ban, FN-AD-04.03).
func DepartmentRecords(ctx context.Context, tx pgx.Tx, dept string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.report_task WHERE department_code = $1`, dept).Scan(&n)
	return n, err
}
