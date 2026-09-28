// Package store: SQL của pf/reports, chỉ dùng pool vai reports_app (R-05).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// ---------------------------------------------------------------------------
// Mẫu biểu
// ---------------------------------------------------------------------------

const tplCols = `template_key, version, status, definition::text, reason, created_by, created_at`

func scanTemplate(row pgx.Row) (domain.Template, error) {
	var t domain.Template
	var def string
	if err := row.Scan(&t.Key, &t.Version, &t.Status, &def, &t.Reason, &t.CreatedBy, &t.CreatedAt); err != nil {
		return t, err
	}
	return t, json.Unmarshal([]byte(def), &t.Def)
}

// LatestTemplate trả phiên bản mới nhất của mẫu trong tenant; chưa có → nil.
func LatestTemplate(ctx context.Context, tx pgx.Tx, key string) (*domain.Template, error) {
	t, err := scanTemplate(tx.QueryRow(ctx, "SELECT "+tplCols+` FROM pf_reports.template_version
	  WHERE template_key = $1 ORDER BY version DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// LatestTemplates trả phiên bản mới nhất của mọi mẫu trong tenant.
func LatestTemplates(ctx context.Context, tx pgx.Tx) ([]domain.Template, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (template_key) `+tplCols+` FROM pf_reports.template_version
	  ORDER BY template_key, version DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Template, error) { return scanTemplate(r) })
}

// TemplateVersions trả mọi phiên bản của một mẫu (mới nhất trước).
func TemplateVersions(ctx context.Context, tx pgx.Tx, key string) ([]domain.Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+tplCols+" FROM pf_reports.template_version WHERE template_key = $1 ORDER BY version DESC", key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Template, error) { return scanTemplate(r) })
}

// InsertTemplate ghi một phiên bản mẫu; version = phiên bản trước + 1 (khóa bảng theo mẫu bằng advisory lock).
func InsertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t *domain.Template) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_reports.template:' || $1::text || ':' || $2::text, 0))", tid, t.Key); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM pf_reports.template_version WHERE template_key = $1", t.Key).Scan(&t.Version); err != nil {
		return err
	}
	def, err := json.Marshal(t.Def)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_reports.template_version (tenant_id, template_key, version, status, definition, reason, created_by, created_at)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)`, tid, t.Key, t.Version, t.Status, string(def), t.Reason, t.CreatedBy, t.CreatedAt.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Việc xuất
// ---------------------------------------------------------------------------

const jobCols = `id, tenant_id, template_key, template_version, template_def::text, format, params::text, scope_all, scope_units,
  requested_by, requested_at, trigger_kind, schedule_id, schedule_slot, idem_key, status, object_key, sha256, size_bytes,
  row_count, provisional, error_code, completed_at`

func scanJob(row pgx.Row) (domain.Job, error) {
	var j domain.Job
	var def, params, status string
	err := row.Scan(&j.ID, &j.TenantID, &j.TemplateKey, &j.TemplateVersion, &def, &j.Format, &params, &j.ScopeAll, &j.ScopeUnits,
		&j.RequestedBy, &j.RequestedAt, &j.Trigger, &j.ScheduleID, &j.ScheduleSlot, &j.IdemKey, &status, &j.ObjectKey, &j.SHA256,
		&j.SizeBytes, &j.RowCount, &j.Provisional, &j.ErrorCode, &j.CompletedAt)
	if err != nil {
		return j, err
	}
	j.Status = domain.JobStatus(status)
	if err := json.Unmarshal([]byte(def), &j.Def); err != nil {
		return j, err
	}
	return j, json.Unmarshal([]byte(params), &j.Params)
}

func oneJob(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.Job, error) {
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM pf_reports.export_job WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// JobByIdemKey tìm việc xuất đã tạo với idem_key.
func JobByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Job, error) {
	return oneJob(ctx, tx, "idem_key = $1::text", key)
}

// JobByID đọc một việc xuất; forUpdate khóa dòng (worker).
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Job, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneJob(ctx, tx, q, id)
}

// JobsOf liệt kê việc xuất do actor yêu cầu, mới nhất trước (requested_at, id
// giảm dần); before khác uuid.Nil → chỉ việc đứng sau việc đó (cursor).
func JobsOf(ctx context.Context, tx pgx.Tx, actor, before uuid.UUID, limit int) ([]domain.Job, error) {
	rows, err := tx.Query(ctx, "SELECT "+jobCols+` FROM pf_reports.export_job j
	    WHERE j.requested_by = $1
	      AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid
	           OR (j.requested_at, j.id) < (SELECT c.requested_at, c.id FROM pf_reports.export_job c WHERE c.id = $2::uuid))
	    ORDER BY j.requested_at DESC, j.id DESC LIMIT $3`, actor, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// InsertJob ghi việc xuất; requested_at do DB đặt.
func InsertJob(ctx context.Context, tx pgx.Tx, j *domain.Job) error {
	def, err := json.Marshal(j.Def)
	if err != nil {
		return err
	}
	params, err := json.Marshal(j.Params)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_reports.export_job
	    (id, tenant_id, template_key, template_version, template_def, format, params, scope_all, scope_units, requested_by,
	     requested_at, trigger_kind, schedule_id, schedule_slot, idem_key, status)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14, $15, 'queued') RETURNING requested_at`,
		j.ID, j.TenantID, j.TemplateKey, j.TemplateVersion, string(def), j.Format, string(params), j.ScopeAll, units(j.ScopeUnits),
		j.RequestedBy, j.RequestedAt.UTC(), j.Trigger, j.ScheduleID, j.ScheduleSlot, j.IdemKey).Scan(&j.RequestedAt)
}

// CompleteJob đánh dấu việc xuất xong.
func CompleteJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, key, sha string, size int64, rows int, provisional bool, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.export_job SET status = 'completed', object_key = $2, sha256 = $3, size_bytes = $4,
	    row_count = $5, provisional = $6, completed_at = $7, attempts = attempts + 1 WHERE id = $1`, id, key, sha, size, rows, provisional, at.UTC())
	return err
}

// FailJob đánh dấu việc xuất hỏng vĩnh viễn.
func FailJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.export_job SET status = 'failed', error_code = $2, completed_at = $3, attempts = attempts + 1
	  WHERE id = $1`, id, code, at.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Nhật ký điểm kiểm soát xuất (DLP)
// ---------------------------------------------------------------------------

// LockActorDay tuần tự hóa việc đếm hạn mức của một người trong một ngày.
func LockActorDay(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, day string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_reports.quota:' || $1::text || ':' || $2::text || ':' || $3::text, 0))",
		tid, actor, day)
	return err
}

// CountExports đếm số lần xuất (API + theo lịch) của một người trong ngày.
func CountExports(ctx context.Context, tx pgx.Tx, actor uuid.UUID, day string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_reports.export_audit
	  WHERE actor_ref = $1 AND business_date = $2::date AND kind IN ('export', 'scheduled')`, actor, day).Scan(&n)
	return n, err
}

// AuditRow là một dòng nhật ký xuất.
type AuditRow struct {
	ID          uuid.UUID
	Kind        string
	ActorRef    uuid.UUID
	TemplateKey string
	Format      string
	JobID       uuid.UUID
	Day         string
	DailyCount  int
	Anomaly     bool
	EventTime   time.Time
}

// InsertAudit ghi nhật ký xuất (chỉ ghi thêm).
func InsertAudit(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a AuditRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.export_audit
	    (tenant_id, id, kind, actor_ref, template_key, format, job_id, business_date, daily_count, anomaly, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11)`,
		tid, a.ID, a.Kind, a.ActorRef, a.TemplateKey, a.Format, a.JobID, a.Day, a.DailyCount, a.Anomaly, a.EventTime.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Lịch gửi
// ---------------------------------------------------------------------------

const schedCols = `id, tenant_id, template_key, format, coalesce(unit_code, ''), params::text, frequency, coalesce(weekday, 0),
  coalesce(day_of_month, 0), hour, recipients::text[], owner_ref, owner_roles, scope_all, scope_units, active, deactivated_reason,
  created_at, updated_at, idem_key`

func scanSchedule(row pgx.Row) (domain.Schedule, error) {
	var s domain.Schedule
	var params string
	var wd, dom, hour int16
	var recips []string
	err := row.Scan(&s.ID, &s.TenantID, &s.TemplateKey, &s.Format, &s.UnitCode, &params, &s.Frequency, &wd, &dom, &hour,
		&recips, &s.OwnerRef, &s.OwnerRoles, &s.ScopeAll, &s.ScopeUnits, &s.Active, &s.DeactivatedReason, &s.CreatedAt,
		&s.UpdatedAt, &s.IdemKey)
	if err != nil {
		return s, err
	}
	s.Weekday, s.DayOfMonth, s.Hour = int(wd), int(dom), int(hour)
	for _, r := range recips {
		id, err := uuid.Parse(r)
		if err != nil {
			return s, err
		}
		s.Recipients = append(s.Recipients, id)
	}
	return s, json.Unmarshal([]byte(params), &s.Params)
}

func refStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func nilIfZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// InsertSchedule ghi lịch mới.
func InsertSchedule(ctx context.Context, tx pgx.Tx, s *domain.Schedule) error {
	params, err := json.Marshal(s.Params)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_reports.schedule
	    (tenant_id, id, template_key, format, unit_code, params, frequency, weekday, day_of_month, hour, recipients, owner_ref,
	     owner_roles, scope_all, scope_units, active, created_at, updated_at, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11::uuid[], $12, $13, $14, $15, true, $16, $16, $17)`,
		s.TenantID, s.ID, s.TemplateKey, s.Format, nilIfEmpty(s.UnitCode), string(params), s.Frequency, nilIfZero(s.Weekday),
		nilIfZero(s.DayOfMonth), s.Hour, refStrings(s.Recipients), s.OwnerRef, s.OwnerRoles, s.ScopeAll, units(s.ScopeUnits), s.CreatedAt.UTC(), s.IdemKey)
	return err
}

// ScheduleByID đọc một lịch.
func ScheduleByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Schedule, error) {
	q := "SELECT " + schedCols + " FROM pf_reports.schedule WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	s, err := scanSchedule(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ScheduleByIdemKey tìm lịch đã tạo với idem_key.
func ScheduleByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Schedule, error) {
	s, err := scanSchedule(tx.QueryRow(ctx, "SELECT "+schedCols+" FROM pf_reports.schedule WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSchedules liệt kê lịch (activeOnly = chỉ lịch đang dùng).
func ListSchedules(ctx context.Context, tx pgx.Tx, activeOnly bool) ([]domain.Schedule, error) {
	rows, err := tx.Query(ctx, "SELECT "+schedCols+" FROM pf_reports.schedule WHERE ($1::boolean = false OR active) ORDER BY created_at, id", activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Schedule, error) { return scanSchedule(r) })
}

// DeactivateSchedule ngừng dùng lịch kèm lý do (không xóa).
func DeactivateSchedule(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.schedule SET active = false, deactivated_reason = $2, updated_at = $3
	  WHERE id = $1 AND active`, id, reason, at.UTC())
	return err
}

// Run là một lần chạy lịch.
type Run struct {
	ScheduleID     uuid.UUID
	SlotKey        string
	SlotAt         time.Time
	Status         string
	SkipReason     *string
	JobID          *uuid.UUID
	FiredAt        time.Time
	DeliveryStatus *string
	DeliveryNote   *string
	SentCount      *int
	DeliveredAt    *time.Time
}

const runCols = `schedule_id, slot_key, slot_at, status, skip_reason, job_id, fired_at, delivery_status, delivery_note, sent_count, delivered_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ScheduleID, &r.SlotKey, &r.SlotAt, &r.Status, &r.SkipReason, &r.JobID, &r.FiredAt, &r.DeliveryStatus,
		&r.DeliveryNote, &r.SentCount, &r.DeliveredAt)
	return r, err
}

// ClaimRun ghi lần chạy của mốc; đã có → false (mỗi mốc chạy đúng một lần).
func ClaimRun(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Run) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_reports.schedule_run (tenant_id, schedule_id, slot_key, slot_at, status, skip_reason, job_id, fired_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
		tid, r.ScheduleID, r.SlotKey, r.SlotAt.UTC(), r.Status, r.SkipReason, r.JobID, r.FiredAt.UTC())
	return err == nil && tag.RowsAffected() == 1, err
}

// RunByJob tìm lần chạy lịch sinh ra việc xuất.
func RunByJob(ctx context.Context, tx pgx.Tx, job uuid.UUID) (*Run, error) {
	r, err := scanRun(tx.QueryRow(ctx, "SELECT "+runCols+" FROM pf_reports.schedule_run WHERE job_id = $1 FOR UPDATE", job))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RunsOf liệt kê các lần chạy của một lịch (mới nhất trước).
func RunsOf(ctx context.Context, tx pgx.Tx, schedule uuid.UUID) ([]Run, error) {
	rows, err := tx.Query(ctx, "SELECT "+runCols+" FROM pf_reports.schedule_run WHERE schedule_id = $1 ORDER BY slot_at DESC LIMIT 200", schedule)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) { return scanRun(r) })
}

// MarkDelivered ghi kết quả gửi email của lần chạy.
func MarkDelivered(ctx context.Context, tx pgx.Tx, schedule uuid.UUID, slot, status, note string, sent int, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.schedule_run SET delivery_status = $3, delivery_note = $4, sent_count = $5, delivered_at = $6
	  WHERE schedule_id = $1 AND slot_key = $2`, schedule, slot, status, nilIfEmpty(note), sent, at.UTC())
	return err
}

// RunExists cho biết mốc lịch đã có lần chạy.
func RunExists(ctx context.Context, tx pgx.Tx, schedule uuid.UUID, slot string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_reports.schedule_run WHERE schedule_id = $1 AND slot_key = $2)", schedule, slot).Scan(&ok)
	return ok, err
}
