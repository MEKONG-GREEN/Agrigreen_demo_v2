// Package store: SQL cảnh báo CNTS (pf_extension.cnts_alert_event, cnts_alert_current, cnts_alert_config,
// cnts_reminder_sent — W11.43) trên pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS). Sổ chỉ ghi thêm
// (shared.apply_ledger: DB tính seq, prev_hash, row_hash, chống trùng idem_key); bảng chiếu cập nhật cùng giao dịch (R-21).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
)

// Alert là trạng thái hiện tại của một cảnh báo.
type Alert struct {
	Ref           uuid.UUID
	DedupKey      string
	Department    string
	Kind          string
	Level         string
	Status        string
	CommuneCode   string
	ProvinceCode  string
	SubjectKind   string
	SubjectRef    string
	TemplateCode  string
	PeriodKey     string
	PeriodRef     *uuid.UUID
	SourceRef     *uuid.UUID
	Title         string
	Detail        map[string]any
	RecipientRefs []uuid.UUID
	AssigneeRef   *uuid.UUID
	RaisedAt      time.Time
	DueAt         time.Time
	OverdueAt     *time.Time
	ClosedAt      *time.Time
	AutoClosed    bool
	Result        string
	LastEventID   uuid.UUID
	LastSeq       int64
	UpdatedAt     time.Time
}

// Event là một dòng sổ cnts_alert_event.
type Event struct {
	ID            uuid.UUID
	AlertRef      uuid.UUID
	Action        string
	StatusAfter   string
	AssigneeRef   *uuid.UUID
	DueAt         *time.Time
	Note          string
	RecipientRefs []uuid.UUID
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
	Seq           int64
}

// Lock tuần tự hóa ghi theo khóa (khóa chống trùng hoặc mã cảnh báo).
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cnts_alert:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func refs(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

// InsertEvent ghi một sự việc (cột chụp của cảnh báo lấy từ a).
func InsertEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Alert, e *Event) error {
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_alert_event (id, tenant_id, stream_key, alert_ref, action, status_after,
	    department_code, kind, level, dedup_key, commune_code, province_code, subject_kind, subject_ref, template_code, period_key,
	    period_ref, source_ref, title, detail, assignee_ref, due_at, note, recipient_refs, event_time, actor_ref, on_behalf_of,
	    support_reason, idem_key, rule_version)
	  VALUES ($1, $2, 'alert:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
	    $19::jsonb, $20, $21, $22, $23::text[]::uuid[], $24, $25, $26, $27, $28, $29)
	  RETURNING seq, recorded_at`,
		e.ID, tid, a.Ref, e.Action, e.StatusAfter, a.Department, a.Kind, a.Level, a.DedupKey, optText(a.CommuneCode),
		optText(a.ProvinceCode), a.SubjectKind, a.SubjectRef, optText(a.TemplateCode), optText(a.PeriodKey), a.PeriodRef, a.SourceRef,
		a.Title, string(detail), e.AssigneeRef, e.DueAt, optText(e.Note), refs(e.RecipientRefs), e.EventTime.UTC(), e.ActorRef,
		e.OnBehalfOf, e.SupportReason, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// InsertCurrent ghi bảng chiếu của cảnh báo mới. Trả false nếu đã có cảnh báo cùng khóa chống trùng.
func InsertCurrent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Alert) (bool, error) {
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_alert_current (tenant_id, alert_ref, dedup_key, department_code, kind, level,
	    status, commune_code, province_code, subject_kind, subject_ref, template_code, period_key, period_ref, source_ref, title, detail,
	    recipient_refs, assignee_ref, raised_at, due_at, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb, $18::text[]::uuid[], $19, $20, $21, $22, $23)
	  ON CONFLICT (tenant_id, dedup_key) DO NOTHING`,
		tid, a.Ref, a.DedupKey, a.Department, a.Kind, a.Level, a.Status, optText(a.CommuneCode), optText(a.ProvinceCode), a.SubjectKind,
		a.SubjectRef, optText(a.TemplateCode), optText(a.PeriodKey), a.PeriodRef, a.SourceRef, a.Title, string(detail),
		refs(a.RecipientRefs), a.AssigneeRef, a.RaisedAt.UTC(), a.DueAt.UTC(), a.LastEventID, a.LastSeq)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateCurrent ghi lại trạng thái hiện tại sau một sự việc.
func UpdateCurrent(ctx context.Context, tx pgx.Tx, a Alert) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.cnts_alert_current SET status = $2, assignee_ref = $3, due_at = $4, overdue_at = $5,
	    closed_at = $6, auto_closed = $7, result = $8, last_event_id = $9, last_seq = $10, updated_at = now()
	  WHERE alert_ref = $1`,
		a.Ref, a.Status, a.AssigneeRef, a.DueAt.UTC(), a.OverdueAt, a.ClosedAt, a.AutoClosed, optText(a.Result), a.LastEventID, a.LastSeq)
	return err
}

const alertCols = `alert_ref, dedup_key, department_code, kind, level, status, commune_code, province_code, subject_kind, subject_ref,
  template_code, period_key, period_ref, source_ref, title, detail::text, recipient_refs, assignee_ref, raised_at, due_at, overdue_at,
  closed_at, auto_closed, result, last_event_id, last_seq, updated_at`

func scanAlert(row pgx.Row) (Alert, error) {
	var a Alert
	var commune, province, tpl, pkey, result *string
	var detail string
	err := row.Scan(&a.Ref, &a.DedupKey, &a.Department, &a.Kind, &a.Level, &a.Status, &commune, &province, &a.SubjectKind, &a.SubjectRef,
		&tpl, &pkey, &a.PeriodRef, &a.SourceRef, &a.Title, &detail, &a.RecipientRefs, &a.AssigneeRef, &a.RaisedAt, &a.DueAt, &a.OverdueAt,
		&a.ClosedAt, &a.AutoClosed, &result, &a.LastEventID, &a.LastSeq, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	a.CommuneCode, a.ProvinceCode, a.TemplateCode, a.PeriodKey, a.Result = str(commune), str(province), str(tpl), str(pkey), str(result)
	if err := json.Unmarshal([]byte(detail), &a.Detail); err != nil {
		return a, err
	}
	return a, nil
}

func one(rows pgx.Rows, err error) (*Alert, error) {
	if err != nil {
		return nil, err
	}
	as, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) { return scanAlert(r) })
	if err != nil || len(as) == 0 {
		return nil, err
	}
	return &as[0], nil
}

// ByRef đọc cảnh báo (nil nếu không có).
func ByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Alert, error) {
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_extension.cnts_alert_current WHERE alert_ref = $1`, ref)
	return one(rows, err)
}

// ByDedup đọc cảnh báo theo khóa chống trùng (nil nếu không có).
func ByDedup(ctx context.Context, tx pgx.Tx, key string) (*Alert, error) {
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_extension.cnts_alert_current WHERE dedup_key = $1`, key)
	return one(rows, err)
}

// Filter lọc danh sách cảnh báo.
type Filter struct {
	Communes   []string // nil = mọi xã (Cấp Tỉnh/Admin); khác nil = chỉ các xã này
	Status     string   // "" | new | assigned | in_progress | closed | open (chưa đóng) | overdue
	Kind       string
	Level      string
	Template   string
	Assignee   *uuid.UUID
	Department string
	Now        time.Time
	Limit      int
}

// List liệt kê cảnh báo, mới trước.
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Alert, error) {
	var communes []string
	if f.Communes != nil {
		communes = f.Communes
		if communes == nil {
			communes = []string{}
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_extension.cnts_alert_current
	   WHERE department_code = $1
	     AND ($2::text[] IS NULL OR commune_code = ANY($2::text[]))
	     AND ($3 = '' OR ($3 = 'open' AND status <> 'closed') OR ($3 = 'overdue' AND status <> 'closed' AND due_at < $9)
	          OR status = $3)
	     AND ($4 = '' OR kind = $4) AND ($5 = '' OR level = $5) AND ($6 = '' OR template_code = $6)
	     AND ($7::uuid IS NULL OR assignee_ref = $7)
	   ORDER BY (status = 'closed'), (level = 'urgent') DESC, raised_at DESC LIMIT $8`,
		f.Department, communes, f.Status, f.Kind, f.Level, f.Template, f.Assignee, f.Limit, f.Now.UTC())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) { return scanAlert(r) })
}

// OpenBySubject trả cảnh báo chưa đóng của đối tượng subject (mọi loại trong kinds) — tự đóng khi số liệu hết vi phạm.
func OpenBySubject(ctx context.Context, tx pgx.Tx, subject string, kinds []string) ([]Alert, error) {
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_extension.cnts_alert_current
	   WHERE subject_ref = $1 AND kind = ANY($2::text[]) AND status <> 'closed'`, subject, kinds)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) { return scanAlert(r) })
}

// Overdue trả cảnh báo chưa đóng quá hạn xử lý chưa báo Quá hạn.
func Overdue(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]Alert, error) {
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_extension.cnts_alert_current
	   WHERE status <> 'closed' AND overdue_at IS NULL AND due_at < $1 ORDER BY due_at LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) { return scanAlert(r) })
}

// Events trả lịch sử của cảnh báo, cũ trước.
func Events(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Event, error) {
	rows, err := tx.Query(ctx, `SELECT id, alert_ref, action, status_after, assignee_ref, due_at, coalesce(note, ''), recipient_refs,
	    event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key, seq
	  FROM pf_extension.cnts_alert_event WHERE alert_ref = $1 ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		err := r.Scan(&e.ID, &e.AlertRef, &e.Action, &e.StatusAfter, &e.AssigneeRef, &e.DueAt, &e.Note, &e.RecipientRefs, &e.EventTime,
			&e.RecordedAt, &e.ActorRef, &e.OnBehalfOf, &e.SupportReason, &e.IdemKey, &e.Seq)
		return e, err
	})
}

// EventByIdem tìm sự việc đã ghi với idem_key (gọi lặp).
func EventByIdem(ctx context.Context, tx pgx.Tx, ref uuid.UUID, idem string) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.cnts_alert_event WHERE alert_ref = $1 AND idem_key = $2`, ref, idem).Scan(&n)
	return n > 0, err
}

// Config là một phiên bản cấu hình.
type Config struct {
	Scope     string
	Code      string
	Settings  json.RawMessage
	Seq       int64
	EventTime time.Time
	ActorRef  uuid.UUID
}

// InsertConfig ghi phiên bản cấu hình mới.
func InsertConfig(ctx context.Context, tx pgx.Tx, tid uuid.UUID, id uuid.UUID, scope, code string, settings []byte, at time.Time, actor uuid.UUID,
	onBehalf *uuid.UUID, support *string, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_alert_config (id, tenant_id, stream_key, department_code, scope, code, settings,
	    event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, 'config:' || $3::text || ':' || $4::text, $5, $3, $4, $6::jsonb, $7, $8, $9, $10, $11, $12) RETURNING seq`,
		id, tid, scope, code, domain.Department, string(settings), at.UTC(), actor, onBehalf, support, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// Configs trả phiên bản mới nhất của mọi cấu hình.
func Configs(ctx context.Context, tx pgx.Tx) ([]Config, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (stream_key) scope, code, settings::text, seq, event_time, actor_ref
	    FROM pf_extension.cnts_alert_config WHERE department_code = $1 ORDER BY stream_key, seq DESC`, domain.Department)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Config, error) {
		var c Config
		var s string
		err := r.Scan(&c.Scope, &c.Code, &s, &c.Seq, &c.EventTime, &c.ActorRef)
		c.Settings = json.RawMessage(s)
		return c, err
	})
}

// MarkReminder ghi lượt nhắc của (kỳ, đơn vị, lượt). Trả false nếu lượt đã gửi.
func MarkReminder(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID, unit, slot, code string, recipients int, at time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_reminder_sent (tenant_id, period_ref, unit_code, slot, template_code, recipients, sent_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`, tid, period, unit, slot, code, recipients, at.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RemindersSince đếm lượt nhắc đã gửi từ thời điểm since theo biểu (FN-13 luồng phụ "xem số nhắc đã gửi trong ngày").
func RemindersSince(ctx context.Context, tx pgx.Tx, since time.Time) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT template_code, count(*) FROM pf_extension.cnts_reminder_sent WHERE sent_at >= $1 GROUP BY template_code`, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// ErrNotFound: không có bản ghi.
var ErrNotFound = errors.New("không có")

// PublicationOwner trả cán bộ tỉnh phụ trách biểu của lần phát hành mới nhất chứa kỳ native period (BF-CN-PUBLISH, sổ
// pf_extension.cnts_publication của extension/reportcycle) — nil nếu kỳ không ghi người phụ trách.
func PublicationOwner(ctx context.Context, tx pgx.Tx, period uuid.UUID) (*uuid.UUID, error) {
	var owner *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT owner_ref FROM pf_extension.cnts_publication WHERE period_refs @> ARRAY[$1::uuid]
	  ORDER BY event_time DESC LIMIT 1`, period).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return owner, err
}
