package store

// W11.27: SQL của kế hoạch nhân lực năm — sổ plan_period, plan_draft, plan_adjustment. Vòng đời kỳ ở cap/forms.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
)

// PlanPeriodRow là kỳ kế hoạch của một năm.
type PlanPeriodRow struct {
	ID          uuid.UUID
	Year        int
	PeriodRef   uuid.UUID
	TemplateRef uuid.UUID
	CutoffOn    time.Time
	DueAt       time.Time
	Units       []string
	Actor       uuid.UUID
	RecordedAt  time.Time
}

const planPeriodCols = `id, plan_year, period_ref, template_ref, cutoff_on, due_at, units, actor_ref, recorded_at`

func scanPlanPeriod(r pgx.CollectableRow) (PlanPeriodRow, error) {
	var x PlanPeriodRow
	err := r.Scan(&x.ID, &x.Year, &x.PeriodRef, &x.TemplateRef, &x.CutoffOn, &x.DueAt, &x.Units, &x.Actor, &x.RecordedAt)
	return x, err
}

// PlanPeriods trả mọi kỳ kế hoạch (bản đầu của mỗi năm), năm mới trước.
func PlanPeriods(ctx context.Context, tx pgx.Tx) ([]PlanPeriodRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+planPeriodCols+` FROM pf_hrm.plan_period WHERE seq = 1 ORDER BY plan_year DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPlanPeriod)
}

// PlanPeriodByYear trả kỳ của năm (nil nếu chưa mở).
func PlanPeriodByYear(ctx context.Context, tx pgx.Tx, year int) (*PlanPeriodRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+planPeriodCols+` FROM pf_hrm.plan_period WHERE plan_year = $1 AND seq = 1`, year)
	if err != nil {
		return nil, err
	}
	xs, err := pgx.CollectRows(rows, scanPlanPeriod)
	if err != nil || len(xs) == 0 {
		return nil, err
	}
	return &xs[0], nil
}

// LockPlans khóa ghi kế hoạch của tenant trong giao dịch (mở kỳ trùng năm).
func LockPlans(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.plan:' || $1::text, 0))`, tid)
	return err
}

// InsertPlanPeriod ghi kỳ kế hoạch năm.
func InsertPlanPeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PlanPeriodRow, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_hrm.plan_period (id, tenant_id, stream_key, plan_year, period_ref, template_ref, cutoff_on,
	    due_at, units, change, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'year:' || $3::int::text, $3, $4, $5, $6, $7, $8, 'opened', $9, $10, $11, $12)`,
		x.ID, tid, x.Year, x.PeriodRef, x.TemplateRef, x.CutoffOn.Format(time.DateOnly), x.DueAt.UTC(), x.Units, at.UTC(), x.Actor, idem,
		domain.RuleVersion)
	return err
}

// PlanEntryRow là một bản nháp hoặc một điều chỉnh của đơn vị (RecordedAt = event_time — đồng hồ nghiệp vụ, so được với
// thời điểm gửi/rà soát của cap/forms).
type PlanEntryRow struct {
	ID         uuid.UUID
	PeriodRef  uuid.UUID
	Unit       string
	Values     []byte
	Reason     string // điều chỉnh
	Seq        int64
	Actor      uuid.UUID
	RecordedAt time.Time
}

func scanPlanEntry(r pgx.CollectableRow) (PlanEntryRow, error) {
	var x PlanEntryRow
	err := r.Scan(&x.ID, &x.PeriodRef, &x.Unit, &x.Values, &x.Reason, &x.Seq, &x.Actor, &x.RecordedAt)
	return x, err
}

// PlanDrafts trả bản nháp MỚI NHẤT của từng đơn vị trong kỳ.
func PlanDrafts(ctx context.Context, tx pgx.Tx, period uuid.UUID) ([]PlanEntryRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (unit_code) id, period_ref, unit_code, plan_values, '', seq, actor_ref, event_time
	  FROM pf_hrm.plan_draft WHERE period_ref = $1 ORDER BY unit_code, seq DESC`, period)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPlanEntry)
}

// InsertPlanDraft ghi một bản nháp.
func InsertPlanDraft(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PlanEntryRow, at time.Time, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.plan_draft (id, tenant_id, stream_key, period_ref, unit_code, plan_values, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'draft:' || $3::uuid::text || ':' || $4::text, $3, $4, $5::jsonb, $6, $7, $8, $9) RETURNING seq`,
		x.ID, tid, x.PeriodRef, x.Unit, string(x.Values), at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// PlanAdjustments trả mọi điều chỉnh sau chốt của kỳ (unit rỗng = mọi đơn vị), cũ trước.
func PlanAdjustments(ctx context.Context, tx pgx.Tx, period uuid.UUID, unit string) ([]PlanEntryRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, period_ref, unit_code, plan_values, reason, seq, actor_ref, event_time
	  FROM pf_hrm.plan_adjustment WHERE period_ref = $1 AND ($2 = '' OR unit_code = $2) ORDER BY unit_code, seq`, period, unit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPlanEntry)
}

// InsertPlanAdjustment ghi một điều chỉnh sau chốt.
func InsertPlanAdjustment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PlanEntryRow, at time.Time, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.plan_adjustment (id, tenant_id, stream_key, period_ref, unit_code, plan_values, reason,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'adj:' || $3::uuid::text || ':' || $4::text, $3, $4, $5::jsonb, $6, $7, $8, $9, $10) RETURNING seq`,
		x.ID, tid, x.PeriodRef, x.Unit, string(x.Values), x.Reason, at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// --- NOTIFY-HCTH-KN (#50, pf 20261026125619): sổ nhắc lập kế hoạch. ---

// PlanReminderRow là một lần nhắc các đơn vị chưa gửi kế hoạch của một kỳ.
type PlanReminderRow struct {
	ID         uuid.UUID
	PeriodRef  uuid.UUID
	Year       int
	NodeRef    uuid.UUID
	Units      []string
	Actor      uuid.UUID
	RecordedAt time.Time // event_time — đồng hồ nghiệp vụ
}

const planReminderCols = `id, period_ref, plan_year, node_ref, units, actor_ref, event_time`

func scanPlanReminder(r pgx.CollectableRow) (PlanReminderRow, error) {
	var x PlanReminderRow
	err := r.Scan(&x.ID, &x.PeriodRef, &x.Year, &x.NodeRef, &x.Units, &x.Actor, &x.RecordedAt)
	return x, err
}

// InsertPlanReminder ghi một lần nhắc (khóa idem trùng → unique_violation, bên gọi đọc lại bằng PlanReminderByIdem).
func InsertPlanReminder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PlanReminderRow, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_hrm.plan_reminder (id, tenant_id, stream_key, period_ref, plan_year, node_ref, units, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'remind:' || $3::uuid::text, $3, $4, $5, $6, $7, $8, $9, $10)`,
		x.ID, tid, x.PeriodRef, x.Year, x.NodeRef, x.Units, at.UTC(), x.Actor, idem, domain.RuleVersion)
	return err
}

// PlanReminderByIdem trả lần nhắc đã ghi với khóa idem (nil nếu không có).
func PlanReminderByIdem(ctx context.Context, tx pgx.Tx, idem string) (*PlanReminderRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+planReminderCols+` FROM pf_hrm.plan_reminder WHERE idem_key = $1 ORDER BY recorded_at DESC LIMIT 1`, idem)
	if err != nil {
		return nil, err
	}
	xs, err := pgx.CollectRows(rows, scanPlanReminder)
	if err != nil || len(xs) == 0 {
		return nil, err
	}
	return &xs[0], nil
}
