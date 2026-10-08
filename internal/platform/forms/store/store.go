// Package store là SQL của pf/forms (schema pf_forms, pf 000121). Giao dịch riêng của module chạy dưới vai r_forms
// (db.ForModule): chỉ SELECT, INSERT vào sổ. Lối ghi song song của bên dùng là hàm pf_forms.mirror (SECURITY
// DEFINER) gọi trong giao dịch của bên dùng.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
)

// Lock khóa kỳ đến hết giao dịch (nộp bài, chốt, mở lại xếp hàng — ảnh chụp không lỡ bài nộp chen giữa).
func Lock(ctx context.Context, tx pgx.Tx, periodRef uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.period:' || $1::text, 0))`, periodRef)
	return err
}

// Template là một phiên bản biểu.
type Template struct {
	ID         uuid.UUID
	Profile    string
	Code       string
	Version    int64
	Title      string
	Fields     []domain.Field
	Fixture    bool
	ActorRef   uuid.UUID
	EventTime  time.Time
	RecordedAt time.Time
}

const tplCols = `id, profile, code, seq, title, fields::text, fixture, actor_ref, event_time, recorded_at`

func scanTemplate(row pgx.Row) (Template, error) {
	var t Template
	var fields string
	if err := row.Scan(&t.ID, &t.Profile, &t.Code, &t.Version, &t.Title, &fields, &t.Fixture, &t.ActorRef, &t.EventTime, &t.RecordedAt); err != nil {
		return t, err
	}
	return t, json.Unmarshal([]byte(fields), &t.Fields)
}

// InsertTemplate ghi một phiên bản biểu mới (số phiên bản = seq của chuỗi template:<profile>:<code>).
func InsertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t *Template, idem string) error {
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.template (id, tenant_id, stream_key, profile, code, title, fields, fixture, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'template:' || $3::text || ':' || $4::text, $3::text, $4::text, $5, $6::jsonb, $7, $8, $9, $10, $11) RETURNING seq, recorded_at`,
		t.ID, tid, t.Profile, t.Code, t.Title, string(fields), t.Fixture, t.EventTime.UTC(), t.ActorRef, idem, domain.RuleVersion).
		Scan(&t.Version, &t.RecordedAt)
}

// TemplateByID đọc một phiên bản biểu (nil nếu không có).
func TemplateByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Template, error) {
	t, err := scanTemplate(tx.QueryRow(ctx, `SELECT `+tplCols+` FROM pf_forms.template WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

// LatestTemplates trả phiên bản mới nhất của mỗi mã biểu của profile.
func LatestTemplates(ctx context.Context, tx pgx.Tx, profile string) ([]Template, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (code) `+tplCols+` FROM pf_forms.template WHERE profile = $1 ORDER BY code, seq DESC`, profile)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) { return scanTemplate(r) })
}

// Period là một kỳ native đã phát hành.
type Period struct {
	ID              uuid.UUID
	Profile         string
	TemplateID      uuid.UUID
	TemplateCode    string
	TemplateVersion int64
	Label           string
	DueAt           time.Time
	Units           []string
	OwnerRef        *uuid.UUID
	PublisherLevel  string
	ActorRef        uuid.UUID
	EventTime       time.Time
}

const periodCols = `id, profile, template_id, template_code, template_version, label, due_at, units, owner_ref, publisher_level, actor_ref, event_time`

func scanPeriod(row pgx.Row) (Period, error) {
	var p Period
	err := row.Scan(&p.ID, &p.Profile, &p.TemplateID, &p.TemplateCode, &p.TemplateVersion, &p.Label, &p.DueAt, &p.Units, &p.OwnerRef,
		&p.PublisherLevel, &p.ActorRef, &p.EventTime)
	return p, err
}

// InsertPeriod ghi kỳ (profile, mã và phiên bản biểu do trigger chép từ template).
func InsertPeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Period, idem string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.period (id, tenant_id, stream_key, profile, template_id, template_code, template_version, label,
	    due_at, units, owner_ref, publisher_level, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2, 'period:' || $1::uuid::text, '', $3, '', 0, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	  RETURNING profile, template_code, template_version`,
		p.ID, tid, p.TemplateID, p.Label, p.DueAt.UTC(), p.Units, p.OwnerRef, p.PublisherLevel, p.EventTime.UTC(), p.ActorRef, idem, domain.RuleVersion).
		Scan(&p.Profile, &p.TemplateCode, &p.TemplateVersion)
}

// PeriodByID đọc kỳ native (nil nếu không có).
func PeriodByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Period, error) {
	p, err := scanPeriod(tx.QueryRow(ctx, `SELECT `+periodCols+` FROM pf_forms.period WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Periods liệt kê kỳ native của profile, mới trước.
func Periods(ctx context.Context, tx pgx.Tx, profile string, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_forms.period WHERE profile = $1 ORDER BY id DESC LIMIT $2`, profile, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// InsertEvent ghi sự kiện kỳ native (máy trạng thái kiểm ở trigger trg_transition — sai → AGF01).
func InsertEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, periodRef uuid.UUID, profile string, e *domain.Event, idem string) error {
	var reason *string
	if e.Reason != "" {
		reason = &e.Reason
	}
	units := e.Units
	if units == nil {
		units = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.period_event (id, tenant_id, stream_key, period_ref, profile, origin, action, mode, units, reason,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'period:' || $3::uuid::text, $3::uuid, $4, 'native', $5, $6, $7, $8, $9, $10, $11, $12) RETURNING seq`,
		e.ID, tid, periodRef, profile, e.Action, e.Mode, units, reason, e.At.UTC(), e.Actor, idem, domain.RuleVersion).Scan(&e.Seq)
}

// Events đọc sự kiện của kỳ theo seq.
func Events(ctx context.Context, tx pgx.Tx, periodRef uuid.UUID) ([]domain.Event, error) {
	rows, err := tx.Query(ctx, `SELECT id, seq, action, mode, units, coalesce(reason, ''), actor_ref, event_time
	    FROM pf_forms.period_event WHERE stream_key = 'period:' || $1::text ORDER BY seq`, periodRef)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Event, error) {
		var e domain.Event
		err := r.Scan(&e.ID, &e.Seq, &e.Action, &e.Mode, &e.Units, &e.Reason, &e.Actor, &e.At)
		return e, err
	})
}

// Submission là một phiên bản bài nộp.
type Submission struct {
	ID         uuid.UUID
	PeriodID   uuid.UUID
	Unit       string
	Version    int64
	Values     map[string]string
	Submitter  uuid.UUID
	EventTime  time.Time
	RecordedAt time.Time
}

const subCols = `id, period_id, unit_code, seq, "values"::text, submitter_ref, event_time, recorded_at`

func scanSub(row pgx.Row) (Submission, error) {
	var s Submission
	var vals string
	if err := row.Scan(&s.ID, &s.PeriodID, &s.Unit, &s.Version, &vals, &s.Submitter, &s.EventTime, &s.RecordedAt); err != nil {
		return s, err
	}
	return s, json.Unmarshal([]byte(vals), &s.Values)
}

// InsertSubmission ghi một phiên bản bài nộp (trigger kiểm kỳ đang mở, đơn vị thuộc kỳ — sai → AGF02).
func InsertSubmission(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Submission, idem string) error {
	vals, err := json.Marshal(s.Values)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.submission (id, tenant_id, stream_key, period_id, unit_code, profile, template_id, "values",
	    submitter_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'sub:' || $3::uuid::text || ':' || $4::text, $3::uuid, $4::text, '', $3::uuid, $5::jsonb, $6, $7, $6, $8, $9) RETURNING seq, recorded_at`,
		s.ID, tid, s.PeriodID, s.Unit, string(vals), s.Submitter, s.EventTime.UTC(), idem, domain.RuleVersion).Scan(&s.Version, &s.RecordedAt)
}

// SubmissionByID đọc một phiên bản bài nộp (nil nếu không có).
func SubmissionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Submission, error) {
	s, err := scanSub(tx.QueryRow(ctx, `SELECT `+subCols+` FROM pf_forms.submission WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// LatestSubmissions trả phiên bản cuối của từng đơn vị trong kỳ.
func LatestSubmissions(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (map[string]Submission, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (unit_code) `+subCols+` FROM pf_forms.submission WHERE period_id = $1
	    ORDER BY unit_code, seq DESC`, periodID)
	if err != nil {
		return nil, err
	}
	subs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSub(r) })
	if err != nil {
		return nil, err
	}
	out := make(map[string]Submission, len(subs))
	for _, s := range subs {
		out[s.Unit] = s
	}
	return out, nil
}

// SubmissionHistory trả mọi phiên bản của một đơn vị trong kỳ, mới trước.
func SubmissionHistory(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, unit string) ([]Submission, error) {
	rows, err := tx.Query(ctx, `SELECT `+subCols+` FROM pf_forms.submission WHERE period_id = $1 AND unit_code = $2 ORDER BY seq DESC`, periodID, unit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Submission, error) { return scanSub(r) })
}

// InsertReview ghi rà soát (kỳ, đơn vị, người nộp do trigger chép; người rà soát = người nộp → 23514 review_four_eyes).
func InsertReview(ctx context.Context, tx pgx.Tx, tid uuid.UUID, submissionID uuid.UUID, r *domain.Review, workflowRequest *uuid.UUID, idem string) error {
	var note *string
	if r.Note != "" {
		note = &r.Note
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_forms.review (id, tenant_id, stream_key, submission_id, period_id, unit_code, submitter_ref, reviewer_ref,
	    decision, note, workflow_request_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'review:' || $3::uuid::text, $3::uuid, $3::uuid, '', $4::uuid, $4::uuid, $5, $6, $7, $8, $4::uuid, $9, $10)`,
		r.ID, tid, submissionID, r.Reviewer, r.Decision, note, workflowRequest, r.At.UTC(), idem, domain.RuleVersion)
	return err
}

// ReviewsOf trả rà soát theo mã phiên bản bài nộp.
func ReviewsOf(ctx context.Context, tx pgx.Tx, submissionIDs []uuid.UUID) (map[uuid.UUID]domain.Review, error) {
	out := map[uuid.UUID]domain.Review{}
	if len(submissionIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT submission_id, id, decision, reviewer_ref, coalesce(note, ''), event_time
	    FROM pf_forms.review WHERE submission_id = ANY($1::text[]::uuid[])`, uuidStrings(submissionIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid uuid.UUID
		var r domain.Review
		if err := rows.Scan(&sid, &r.ID, &r.Decision, &r.Reviewer, &r.Note, &r.At); err != nil {
			return nil, err
		}
		out[sid] = r
	}
	return out, rows.Err()
}

// Snapshot là một ảnh chụp đóng băng.
type Snapshot struct {
	ID            uuid.UUID
	PeriodRef     uuid.UUID
	Profile       string
	No            int64
	PeriodEventID uuid.UUID
	Content       json.RawMessage // nil khi liệt kê
	Hash          []byte
	HashValid     bool // sha256(content::text) tính lại khớp content_hash
	ActorRef      uuid.UUID
	EventTime     time.Time
	RuleVersion   string
}

// InsertSnapshot ghi ảnh chụp của sự kiện chốt (content_hash do trigger tính).
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Snapshot) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.close_snapshot (id, tenant_id, stream_key, period_ref, profile, period_event_id, content,
	    content_hash, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'snapshot:' || $3::uuid::text, $3::uuid, '', $4::uuid, $5::jsonb, '\x'::bytea, $6, $7, 'snapshot:' || $4::uuid::text, $8)
	  RETURNING seq, profile, content_hash`,
		s.ID, tid, s.PeriodRef, s.PeriodEventID, string(s.Content), s.EventTime.UTC(), s.ActorRef, domain.RuleVersion).Scan(&s.No, &s.Profile, &s.Hash)
}

const snapCols = `id, period_ref, profile, seq, period_event_id, content_hash, content_hash = sha256(convert_to(content::text, 'UTF8')),
  actor_ref, event_time, rule_version`

func scanSnap(row pgx.Row, extra ...any) (Snapshot, error) {
	var s Snapshot
	dst := append([]any{&s.ID, &s.PeriodRef, &s.Profile, &s.No, &s.PeriodEventID, &s.Hash, &s.HashValid, &s.ActorRef, &s.EventTime, &s.RuleVersion}, extra...)
	err := row.Scan(dst...)
	return s, err
}

// Snapshots liệt kê ảnh chụp của kỳ (không kèm nội dung), cũ trước.
func Snapshots(ctx context.Context, tx pgx.Tx, periodRef uuid.UUID) ([]Snapshot, error) {
	rows, err := tx.Query(ctx, `SELECT `+snapCols+` FROM pf_forms.close_snapshot WHERE period_ref = $1 ORDER BY seq`, periodRef)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Snapshot, error) { return scanSnap(r) })
}

// SnapshotByID đọc ảnh chụp kèm nội dung (nil nếu không có).
func SnapshotByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Snapshot, error) {
	var content string
	s, err := scanSnap(tx.QueryRow(ctx, `SELECT `+snapCols+`, content::text FROM pf_forms.close_snapshot WHERE id = $1`, id), &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	s.Content = json.RawMessage(content)
	return &s, err
}

// MirrorResult là kết quả pf_forms.mirror.
type MirrorResult struct {
	EventID    uuid.UUID
	Seq        int64
	SnapshotID *uuid.UUID
	SnapshotNo *int64
	Hash       []byte
	Replayed   bool
}

// Mirror gọi pf_forms.mirror trong giao dịch của bên dùng.
func Mirror(ctx context.Context, tx pgx.Tx, eventID, snapshotID uuid.UUID, profile string, periodRef uuid.UUID, label *string, action, mode string,
	units []string, reason *string, content []byte, actor uuid.UUID, at time.Time, idem string) (MirrorResult, error) {
	var r MirrorResult
	var c *string
	if content != nil {
		s := string(content)
		c = &s
	}
	if units == nil {
		units = []string{}
	}
	err := tx.QueryRow(ctx, `SELECT o_event_id, o_seq, o_snapshot_id, o_snapshot_no, o_content_hash, o_replayed
	    FROM pf_forms.mirror($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, $14)`,
		eventID, snapshotID, profile, periodRef, label, action, mode, units, reason, c, actor, at.UTC(), idem, domain.RuleVersion).
		Scan(&r.EventID, &r.Seq, &r.SnapshotID, &r.SnapshotNo, &r.Hash, &r.Replayed)
	return r, err
}

// CurrentTenant đọc app.tenant_id của giao dịch (giao dịch bên dùng đã đặt tenant).
func CurrentTenant(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
	var tid *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT shared.current_tenant()`).Scan(&tid)
	return tid, err
}

// uuidStrings: pool chạy chế độ không mô tả kiểu tham số (PgBouncer) — mảng uuid truyền dạng text[] rồi ép trong SQL.
func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}
