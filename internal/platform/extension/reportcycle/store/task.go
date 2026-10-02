package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Task là một dòng report_task (công việc xã × biểu × kỳ).
type Task struct {
	ID                    uuid.UUID
	BatchID               uuid.UUID
	PeriodID              uuid.UUID
	TemplateID            uuid.UUID
	VersionID             uuid.UUID
	FormCode              string
	DepartmentCode        string
	ProvinceCode          string
	CommuneCode           string
	RegionCode            string
	Slot                  int
	PeriodBlock           bool
	DueAt                 time.Time
	Status                string
	Assignees             []uuid.UUID
	LatestVersion         int
	LatestSubmissionID    *uuid.UUID
	SubmittedSubmissionID *uuid.UUID
	SubmittedAt           *time.Time
	SubmittedBy           *uuid.UUID
	OnBehalfOf            *uuid.UUID
	FirstSubmittedAt      *time.Time
	AdjustmentReason      *string
	AdjustmentAt          *time.Time
	ConfirmedBy           *uuid.UUID
	ConfirmedAt           *time.Time
	LastRemindedAt        *time.Time
	ReopenedUntil         *time.Time
	Withdrawn             bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

const taskCols = `id, batch_id, period_id, template_id, version_id, form_code, department_code, province_code, commune_code, region_code, slot,
  period_block, due_at, status, assignees, latest_version, latest_submission_id, submitted_submission_id, submitted_at, submitted_by,
  on_behalf_of, first_submitted_at, adjustment_reason, adjustment_requested_at, confirmed_by, confirmed_at, last_reminded_at, reopened_until,
  withdrawn, created_at, updated_at`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.BatchID, &t.PeriodID, &t.TemplateID, &t.VersionID, &t.FormCode, &t.DepartmentCode, &t.ProvinceCode, &t.CommuneCode,
		&t.RegionCode, &t.Slot, &t.PeriodBlock, &t.DueAt, &t.Status, &t.Assignees, &t.LatestVersion, &t.LatestSubmissionID, &t.SubmittedSubmissionID,
		&t.SubmittedAt, &t.SubmittedBy, &t.OnBehalfOf, &t.FirstSubmittedAt, &t.AdjustmentReason, &t.AdjustmentAt, &t.ConfirmedBy, &t.ConfirmedAt,
		&t.LastRemindedAt, &t.ReopenedUntil, &t.Withdrawn, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func collectTasks(rows pgx.Rows, err error) ([]Task, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Task, error) { return scanTask(r) })
}

// NewTask là một công việc sẽ sinh khi phát hành.
type NewTask struct {
	ID          uuid.UUID
	PeriodID    uuid.UUID
	Form        BatchForm
	Commune     string
	Region      string
	Slot        int
	DueAt       time.Time
	AssigneeCSV string // mảng uuid dạng chuỗi Postgres "{a,b}"
}

// InsertTasks sinh công việc hàng loạt bằng một câu lệnh unnest (8.670 công việc ≪ 2 s, xem ghi chú tải).
func InsertTasks(ctx context.Context, tx pgx.Tx, tid, batchID uuid.UUID, dept, province string, ts []NewTask, at time.Time) error {
	const chunk = 2000
	for start := 0; start < len(ts); start += chunk {
		end := min(start+chunk, len(ts))
		n := end - start
		ids, periods, tpls, vers, codes, communes, regions, assignees := make([]string, n), make([]string, n), make([]string, n),
			make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		slots, blocks, dues := make([]string, n), make([]string, n), make([]string, n)
		for i, t := range ts[start:end] {
			ids[i], periods[i], tpls[i], vers[i], codes[i] = t.ID.String(), t.PeriodID.String(), t.Form.TemplateID.String(), t.Form.VersionID.String(), t.Form.Code
			communes[i], regions[i], assignees[i] = t.Commune, t.Region, t.AssigneeCSV
			slots[i], blocks[i], dues[i] = strconv.Itoa(t.Slot), strconv.FormatBool(t.Form.PeriodBlock), t.DueAt.UTC().Format(time.RFC3339Nano)
		}
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.report_task (tenant_id, id, batch_id, period_id, template_id, version_id, form_code,
		    department_code, province_code, commune_code, region_code, slot, period_block, due_at, status, assignees, created_at, updated_at)
		  SELECT $1, u.id, $2, u.period_id, u.tpl, u.ver, u.code, $3, $4, u.commune, u.region, u.slot, u.block, u.due, 'not_started', u.asg::uuid[], $5, $5
		  FROM unnest($6::uuid[], $7::uuid[], $8::uuid[], $9::uuid[], $10::text[], $11::text[], $12::text[], $13::int[], $14::boolean[],
		              $15::timestamptz[], $16::text[]) AS u(id, period_id, tpl, ver, code, commune, region, slot, block, due, asg)`,
			tid, batchID, dept, province, at.UTC(), ids, periods, tpls, vers, codes, communes, regions, slots, blocks, dues, assignees)
		if err != nil {
			return err
		}
	}
	return nil
}

// TaskByID đọc (khóa nếu lock) một công việc; không có → nil.
func TaskByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Task, error) {
	q := "SELECT " + taskCols + " FROM pf_extension.report_task WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	t, err := scanTask(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TaskFilter lọc công việc.
type TaskFilter struct {
	Department string   // rỗng = mọi phòng ban (Admin)
	Communes   []string // nil = mọi xã trong tỉnh
	Province   string   // rỗng = mọi tỉnh
	BatchID    *uuid.UUID
	PeriodID   *uuid.UUID
	TemplateID *uuid.UUID
	FormCode   string
	PeriodType string
	Statuses   []string // trạng thái lưu
	Before     *uuid.UUID
	OrderByDue bool
	Limit      int
}

// ListTasks trả công việc theo bộ lọc (mặc định sắp theo hạn gần nhất — FN-XA-03.01).
func ListTasks(ctx context.Context, tx pgx.Tx, f TaskFilter) ([]Task, error) {
	order := " ORDER BY t.id DESC"
	if f.OrderByDue {
		order = " ORDER BY t.due_at, t.id"
	}
	if f.Limit <= 0 {
		f.Limit = 100000
	}
	return collectTasks(tx.Query(ctx, "SELECT "+prefixed("t.", taskCols)+` FROM pf_extension.report_task t
	  JOIN pf_extension.report_batch b ON b.id = t.batch_id
	  WHERE NOT t.withdrawn AND ($1 = '' OR t.department_code = $1) AND ($2::text[] IS NULL OR t.commune_code = ANY($2::text[]))
	    AND ($3 = '' OR t.province_code = $3) AND ($4::uuid IS NULL OR t.batch_id = $4::uuid) AND ($5::uuid IS NULL OR t.period_id = $5::uuid)
	    AND ($6::uuid IS NULL OR t.template_id = $6::uuid) AND ($7 = '' OR t.form_code = $7) AND ($8 = '' OR b.period_type = $8)
	    AND (cardinality($9::text[]) = 0 OR t.status = ANY($9::text[])) AND ($10::uuid IS NULL OR t.id < $10::uuid)`+order+` LIMIT $11`,
		f.Department, nilIfEmpty(f.Communes), f.Province, f.BatchID, f.PeriodID, f.TemplateID, f.FormCode, f.PeriodType, emptyIfNil(f.Statuses),
		f.Before, f.Limit))
}

func nilIfEmpty(s []string) []string {
	if s == nil {
		return nil
	}
	return s
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// TasksOfPeriod trả mọi công việc (chưa thu hồi) của một kỳ, tùy chọn một biểu.
func TasksOfPeriod(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, templateID *uuid.UUID, lock bool) ([]Task, error) {
	q := "SELECT " + taskCols + ` FROM pf_extension.report_task WHERE period_id = $1::uuid AND NOT withdrawn
	  AND ($2::uuid IS NULL OR template_id = $2::uuid) ORDER BY commune_code, form_code`
	if lock {
		q += " FOR UPDATE"
	}
	return collectTasks(tx.Query(ctx, q, periodID, templateID))
}

// SiblingTasks trả công việc cùng đợt × biểu × xã ở các kỳ khác (giá trị khe kỳ khác của mẫu nhiều kỳ).
func SiblingTasks(ctx context.Context, tx pgx.Tx, batchID, templateID uuid.UUID, commune string) ([]Task, error) {
	return collectTasks(tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.report_task
	  WHERE batch_id = $1::uuid AND template_id = $2::uuid AND commune_code = $3 AND NOT withdrawn ORDER BY slot`, batchID, templateID, commune))
}

// TaskForFormInPeriod trả công việc của mẫu mã formCode cùng xã cùng kỳ (tham chiếu liên biểu, vd T03 ↔ T01).
func TaskForFormInPeriod(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, formCode, commune string) (*Task, error) {
	t, err := scanTask(tx.QueryRow(ctx, "SELECT "+taskCols+` FROM pf_extension.report_task
	  WHERE period_id = $1::uuid AND form_code = $2 AND commune_code = $3 AND NOT withdrawn LIMIT 1`, periodID, formCode, commune))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CountSubmittedInBatch đếm công việc đã có bản gửi trong đợt (thu hồi chỉ khi = 0).
func CountSubmittedInBatch(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.report_task WHERE batch_id = $1::uuid AND submitted_at IS NOT NULL`, batchID).Scan(&n)
	return n, err
}

// WithdrawTasks đánh dấu công việc của đợt đã thu hồi.
func WithdrawTasks(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_task SET withdrawn = true, updated_at = $2 WHERE batch_id = $1`, batchID, at.UTC())
	return err
}

// SaveSubmissionState chiếu một bài nộp lên công việc.
func SaveSubmissionState(ctx context.Context, tx pgx.Tx, t Task, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_task SET status = $2, latest_version = $3, latest_submission_id = $4,
	    submitted_submission_id = $5, submitted_at = $6, submitted_by = $7, on_behalf_of = $8, first_submitted_at = $9, adjustment_reason = $10,
	    adjustment_requested_at = $11, confirmed_by = $12, confirmed_at = $13, updated_at = $14
	  WHERE id = $1`, t.ID, t.Status, t.LatestVersion, t.LatestSubmissionID, t.SubmittedSubmissionID, t.SubmittedAt, t.SubmittedBy, t.OnBehalfOf,
		t.FirstSubmittedAt, t.AdjustmentReason, t.AdjustmentAt, t.ConfirmedBy, t.ConfirmedAt, at.UTC())
	return err
}

// SetReopened đặt/xóa hạn mở lại của công việc.
func SetReopened(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, until *time.Time, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_task SET reopened_until = $2, updated_at = $3 WHERE id = ANY($1::uuid[])`, strs(ids), until, at.UTC())
	return err
}

// MarkReminded ghi lần nhắc gần nhất.
func MarkReminded(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.report_task SET last_reminded_at = $2, updated_at = $2 WHERE id = ANY($1::uuid[])`, strs(ids), at.UTC())
	return err
}

// ExpiredReopens trả công việc có hạn mở lại đã qua (job tự chốt lại).
func ExpiredReopens(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]Task, error) {
	return collectTasks(tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.report_task
	  WHERE reopened_until IS NOT NULL AND reopened_until <= $1 ORDER BY period_id, reopened_until LIMIT $2`, now.UTC(), limit))
}

// ---------------------------------------------------------------------------
// Sổ công việc
// ---------------------------------------------------------------------------

// TaskEvent là một dòng report_task_event.
type TaskEvent struct {
	ID           uuid.UUID
	TaskID       uuid.UUID
	Action       string
	StatusAfter  string
	SubmissionID *uuid.UUID
	Reason       *string
	Payload      json.RawMessage
	OnBehalfOf   *uuid.UUID
	EventTime    time.Time
	ActorRef     uuid.UUID
	IdemKey      string
	Seq          int64
	RecordedAt   time.Time
}

// InsertTaskEvents ghi nhiều sự kiện công việc.
func InsertTaskEvents(ctx context.Context, tx pgx.Tx, tid uuid.UUID, es []TaskEvent) error {
	for i := range es {
		e := &es[i]
		if err := tx.QueryRow(ctx, `INSERT INTO pf_extension.report_task_event (id, tenant_id, stream_key, task_id, action, status_after, submission_id,
		    reason, payload, on_behalf_of, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
			e.ID, tid, "task:"+e.TaskID.String(), e.TaskID, e.Action, e.StatusAfter, e.SubmissionID, e.Reason, rawOr(e.Payload, "{}"), e.OnBehalfOf,
			e.EventTime.UTC(), e.ActorRef, e.IdemKey, RuleVersion).Scan(&e.Seq, &e.RecordedAt); err != nil {
			return err
		}
	}
	return nil
}

// TaskEvents trả nhật ký công việc (mới nhất trước).
func TaskEvents(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]TaskEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, task_id, action, status_after, submission_id, reason, payload::text, on_behalf_of, event_time, actor_ref,
	    idem_key, seq, recorded_at FROM pf_extension.report_task_event WHERE task_id = $1::uuid ORDER BY seq DESC`, taskID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TaskEvent, error) {
		var e TaskEvent
		var p string
		err := r.Scan(&e.ID, &e.TaskID, &e.Action, &e.StatusAfter, &e.SubmissionID, &e.Reason, &p, &e.OnBehalfOf, &e.EventTime, &e.ActorRef,
			&e.IdemKey, &e.Seq, &e.RecordedAt)
		e.Payload = json.RawMessage(p)
		return e, err
	})
}

// ---------------------------------------------------------------------------
// Giá trị Tỉnh giao
// ---------------------------------------------------------------------------

// ProvincialValue là giá trị cột Tỉnh giao của một xã cho một biểu.
type ProvincialValue struct {
	ID            uuid.UUID
	BatchID       uuid.UUID
	TemplateID    uuid.UUID
	Commune       string
	Cells         map[string]string
	Source        string
	SourceBatchID *uuid.UUID
	Department    string
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
}

// InsertProvincialValues ghi sổ giá trị Tỉnh giao.
func InsertProvincialValues(ctx context.Context, tx pgx.Tx, tid uuid.UUID, vs []ProvincialValue) error {
	for _, v := range vs {
		b, err := json.Marshal(v.Cells)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.provincial_value (id, tenant_id, stream_key, batch_id, template_id, commune_code, cells,
		    source, source_batch_id, department_code, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14)`,
			v.ID, tid, "pv:"+v.BatchID.String(), v.BatchID, v.TemplateID, v.Commune, string(b), v.Source, v.SourceBatchID, v.Department,
			v.EventTime.UTC(), v.ActorRef, v.IdemKey, RuleVersion); err != nil {
			return err
		}
	}
	return nil
}

// ProvincialValues trả giá trị Tỉnh giao MỚI NHẤT theo (biểu, xã) của đợt; templateID nil = mọi biểu.
func ProvincialValues(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, templateID *uuid.UUID) (map[uuid.UUID]map[string]map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (template_id, commune_code) template_id, commune_code, cells::text
	  FROM pf_extension.provincial_value WHERE batch_id = $1::uuid AND ($2::uuid IS NULL OR template_id = $2::uuid)
	  ORDER BY template_id, commune_code, seq DESC`, batchID, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]map[string]map[string]string{}
	for rows.Next() {
		var tpl uuid.UUID
		var commune, cells string
		if err := rows.Scan(&tpl, &commune, &cells); err != nil {
			return nil, err
		}
		m := map[string]string{}
		if err := json.Unmarshal([]byte(cells), &m); err != nil {
			return nil, err
		}
		if out[tpl] == nil {
			out[tpl] = map[string]map[string]string{}
		}
		out[tpl][commune] = m
	}
	return out, rows.Err()
}
