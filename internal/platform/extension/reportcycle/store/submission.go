package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SubRow là một dòng của bài nộp: ô do người nhập và ô đã tính.
type SubRow struct {
	Key       string            `json:"key"`
	Indicator string            `json:"indicator,omitempty"`
	Input     map[string]string `json:"input"`
	Cells     map[string]string `json:"cells"`
}

// Submission là một dòng submission_version (bất biến).
type Submission struct {
	ID                 uuid.UUID
	TaskID             uuid.UUID
	VersionNo          int
	FormVersionID      uuid.UUID
	Kind               string
	Rows               []SubRow
	Issues             json.RawMessage
	AdjustmentResponse bool
	PreviousID         *uuid.UUID
	Commune            string
	Department         string
	OnBehalfOf         *uuid.UUID
	SupportSessionID   *uuid.UUID
	SupportReason      *string
	EventTime          time.Time
	ActorRef           uuid.UUID
	DeviceID           *string
	IdemKey            string
	Seq                int64
	RecordedAt         time.Time
}

const subCols = `id, task_id, version_no, form_version_id, kind, rows::text, issues::text, adjustment_response, previous_id, commune_code,
  department_code, on_behalf_of, support_session_id, support_reason, event_time, actor_ref, device_id, idem_key, seq, recorded_at`

func scanSub(row pgx.Row) (Submission, error) {
	var s Submission
	var rows, issues string
	err := row.Scan(&s.ID, &s.TaskID, &s.VersionNo, &s.FormVersionID, &s.Kind, &rows, &issues, &s.AdjustmentResponse, &s.PreviousID, &s.Commune,
		&s.Department, &s.OnBehalfOf, &s.SupportSessionID, &s.SupportReason, &s.EventTime, &s.ActorRef, &s.DeviceID, &s.IdemKey, &s.Seq, &s.RecordedAt)
	if err != nil {
		return s, err
	}
	s.Issues = json.RawMessage(issues)
	err = json.Unmarshal([]byte(rows), &s.Rows)
	return s, err
}

// InsertSubmission ghi một phiên bản bài nộp.
func InsertSubmission(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Submission) error {
	rows, err := json.Marshal(s.Rows)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.submission_version (id, tenant_id, stream_key, task_id, version_no, form_version_id, kind, rows,
	    issues, adjustment_response, previous_id, commune_code, department_code, on_behalf_of, support_session_id, support_reason, event_time,
	    actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21) RETURNING seq, recorded_at`,
		s.ID, tid, "sub:"+s.TaskID.String(), s.TaskID, s.VersionNo, s.FormVersionID, s.Kind, string(rows), rawOr(s.Issues, "[]"), s.AdjustmentResponse,
		s.PreviousID, s.Commune, s.Department, s.OnBehalfOf, s.SupportSessionID, s.SupportReason, s.EventTime.UTC(), s.ActorRef, s.DeviceID, s.IdemKey,
		RuleVersion).Scan(&s.Seq, &s.RecordedAt)
}

// SubmissionByID đọc một phiên bản; không có → nil.
func SubmissionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Submission, error) {
	s, err := scanSub(tx.QueryRow(ctx, "SELECT "+subCols+" FROM pf_extension.submission_version WHERE id = $1::uuid", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SubmissionsByIDs đọc nhiều phiên bản theo id.
func SubmissionsByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Submission, error) {
	out := map[uuid.UUID]Submission{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+subCols+" FROM pf_extension.submission_version WHERE id = ANY($1::uuid[])", ids)
	if err != nil {
		return nil, err
	}
	ss, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSub(r) })
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		out[s.ID] = s
	}
	return out, nil
}

// SubmissionHistory trả mọi phiên bản của công việc (mới nhất trước) — "Lịch sử sửa".
func SubmissionHistory(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]Submission, error) {
	rows, err := tx.Query(ctx, "SELECT "+subCols+" FROM pf_extension.submission_version WHERE task_id = $1::uuid ORDER BY version_no DESC", taskID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSub(r) })
}

// ActAsSubmissionsBy đếm bản nộp do actor nhập thay (on_behalf_of) trong các công việc — chặn người nhập thay tự
// xác nhận/chốt bản của chính mình (gap §4.6).
func ActAsSubmissionsBy(ctx context.Context, tx pgx.Tx, actor uuid.UUID, taskIDs []uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.submission_version
	  WHERE actor_ref = $1 AND on_behalf_of IS NOT NULL AND kind = 'submitted' AND task_id = ANY($2::uuid[])`, actor, taskIDs).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Rà soát, nhắc nhở, kỳ
// ---------------------------------------------------------------------------

// Review là một dòng review_event.
type Review struct {
	ID           uuid.UUID
	TaskID       uuid.UUID
	SubmissionID uuid.UUID
	Action       string
	Reason       *string
	Department   string
	OnBehalfOf   *uuid.UUID
	EventTime    time.Time
	ActorRef     uuid.UUID
	IdemKey      string
}

// InsertReview ghi sổ rà soát.
func InsertReview(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Review) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.review_event (id, tenant_id, stream_key, task_id, submission_id, action, reason, department_code,
	    on_behalf_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		r.ID, tid, "review:"+r.TaskID.String(), r.TaskID, r.SubmissionID, r.Action, r.Reason, r.Department, r.OnBehalfOf, r.EventTime.UTC(), r.ActorRef,
		r.IdemKey, RuleVersion)
	return err
}

// Reminder là một dòng reminder_event.
type Reminder struct {
	ID         uuid.UUID
	BatchID    uuid.UUID
	PeriodID   uuid.UUID
	TemplateID *uuid.UUID
	Mode       string
	Message    string
	TaskIDs    []uuid.UUID
	Communes   []string
	Recipients []uuid.UUID
	Department string
	OnBehalfOf *uuid.UUID
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	RecordedAt time.Time
}

// InsertReminder ghi sổ nhắc.
func InsertReminder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Reminder) error {
	var seq int64
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.reminder_event (id, tenant_id, stream_key, batch_id, period_id, template_id, mode, message, task_ids,
	    communes, recipient_refs, department_code, on_behalf_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10::text[], $11::uuid[], $12, $13, $14, $15, $16, $17) RETURNING seq, recorded_at`,
		r.ID, tid, "reminder:"+r.BatchID.String(), r.BatchID, r.PeriodID, r.TemplateID, r.Mode, r.Message, r.TaskIDs, r.Communes, r.Recipients,
		r.Department, r.OnBehalfOf, r.EventTime.UTC(), r.ActorRef, r.IdemKey, RuleVersion).Scan(&seq, &r.RecordedAt)
}

// Reminders trả lịch sử nhắc của đợt (mới nhất trước), tùy chọn một kỳ.
func Reminders(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, periodID *uuid.UUID, limit int) ([]Reminder, error) {
	rows, err := tx.Query(ctx, `SELECT id, batch_id, period_id, template_id, mode, message, task_ids, communes, recipient_refs, department_code,
	    on_behalf_of, event_time, actor_ref, idem_key, recorded_at FROM pf_extension.reminder_event
	  WHERE batch_id = $1::uuid AND ($2::uuid IS NULL OR period_id = $2::uuid) ORDER BY seq DESC LIMIT $3`, batchID, periodID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Reminder, error) {
		var m Reminder
		err := r.Scan(&m.ID, &m.BatchID, &m.PeriodID, &m.TemplateID, &m.Mode, &m.Message, &m.TaskIDs, &m.Communes, &m.Recipients, &m.Department,
			&m.OnBehalfOf, &m.EventTime, &m.ActorRef, &m.IdemKey, &m.RecordedAt)
		return m, err
	})
}

// LatestReminderFor trả lần nhắc thủ công mới nhất của Cấp Tỉnh tới một trong các xã (khung "nhắc mới nhất" của
// màn Công việc cần làm); không có → nil.
func LatestReminderFor(ctx context.Context, tx pgx.Tx, communes []string, dept string) (*Reminder, error) {
	var m Reminder
	err := tx.QueryRow(ctx, `SELECT id, batch_id, period_id, template_id, mode, message, task_ids, communes, recipient_refs, department_code,
	    on_behalf_of, event_time, actor_ref, idem_key, recorded_at FROM pf_extension.reminder_event
	  WHERE mode = 'manual' AND department_code = $2 AND communes && $1::text[] ORDER BY recorded_at DESC LIMIT 1`, communes, dept).
		Scan(&m.ID, &m.BatchID, &m.PeriodID, &m.TemplateID, &m.Mode, &m.Message, &m.TaskIDs, &m.Communes, &m.Recipients, &m.Department,
			&m.OnBehalfOf, &m.EventTime, &m.ActorRef, &m.IdemKey, &m.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// RemindedSince cho biết công việc đã được nhắc tự động chế độ mode từ since (job idempotent).
func RemindedSince(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, mode string, since time.Time) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT unnest(task_ids) FROM pf_extension.reminder_event
	  WHERE period_id = $1::uuid AND mode = $2 AND event_time >= $3`, periodID, mode, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// PeriodEvent là một dòng period_event.
type PeriodEvent struct {
	ID          uuid.UUID
	PeriodID    uuid.UUID
	BatchID     uuid.UUID
	Action      string
	Communes    []string
	TemplateIDs []uuid.UUID
	TaskIDs     []uuid.UUID
	Reason      *string
	ReopenUntil *time.Time
	Department  string
	OnBehalfOf  *uuid.UUID
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
	Seq         int64
	RecordedAt  time.Time
}

// InsertPeriodEvent ghi sổ kỳ.
func InsertPeriodEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *PeriodEvent) error {
	if e.Communes == nil {
		e.Communes = []string{}
	}
	if e.TemplateIDs == nil {
		e.TemplateIDs = []uuid.UUID{}
	}
	if e.TaskIDs == nil {
		e.TaskIDs = []uuid.UUID{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.period_event (id, tenant_id, stream_key, period_id, batch_id, action, communes, template_ids,
	    task_ids, reason, reopen_until, department_code, on_behalf_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::text[], $8::uuid[], $9::uuid[], $10, $11, $12, $13, $14, $15, $16, $17) RETURNING seq, recorded_at`,
		e.ID, tid, "period:"+e.PeriodID.String(), e.PeriodID, e.BatchID, e.Action, e.Communes, e.TemplateIDs, e.TaskIDs, e.Reason, e.ReopenUntil,
		e.Department, e.OnBehalfOf, e.EventTime.UTC(), e.ActorRef, e.IdemKey, RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// PeriodEvents trả nhật ký kỳ (mới nhất trước).
func PeriodEvents(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) ([]PeriodEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, period_id, batch_id, action, communes, template_ids, task_ids, reason, reopen_until, department_code,
	    on_behalf_of, event_time, actor_ref, idem_key, seq, recorded_at FROM pf_extension.period_event WHERE period_id = $1::uuid ORDER BY seq DESC`, periodID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PeriodEvent, error) {
		var e PeriodEvent
		err := r.Scan(&e.ID, &e.PeriodID, &e.BatchID, &e.Action, &e.Communes, &e.TemplateIDs, &e.TaskIDs, &e.Reason, &e.ReopenUntil, &e.Department,
			&e.OnBehalfOf, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
		return e, err
	})
}
