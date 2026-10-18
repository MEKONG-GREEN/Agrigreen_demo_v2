// Package store: SQL của Kỳ KHTC (pf 20261013120542 — pf_extension.khtc_period, khtc_period_event, khtc_period_source) trên
// pool vai của pf/extension, luôn trong giao dịch có ngữ cảnh tenant (RLS). Ba sổ chỉ ghi thêm (shared.apply_ledger).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
)

// Period là một dòng kỳ KHTC.
type Period struct {
	ID             uuid.UUID
	Type           string
	Year           int
	No             *int
	Scope          *string
	Key            *string
	Name           string
	From, To       time.Time
	Cutoff         time.Time
	DueAt          time.Time
	ExportDueAt    time.Time
	MaxSubmissions int
	Departments    []string
	FormsPeriodRef uuid.UUID
	TemplateCode   string
	Rule           string
	ActorRef       uuid.UUID
	EventTime      time.Time
	RecordedAt     time.Time
}

const periodCols = `id, report_type, plan_year, period_no, scope, period_key, name, period_from, period_to, cutoff_date, due_at,
  export_due_at, max_submissions, departments, forms_period_ref, template_code, period_rule, actor_ref, event_time, recorded_at`

func scanPeriod(row pgx.Row) (Period, error) {
	var p Period
	err := row.Scan(&p.ID, &p.Type, &p.Year, &p.No, &p.Scope, &p.Key, &p.Name, &p.From, &p.To, &p.Cutoff, &p.DueAt, &p.ExportDueAt,
		&p.MaxSubmissions, &p.Departments, &p.FormsPeriodRef, &p.TemplateCode, &p.Rule, &p.ActorRef, &p.EventTime, &p.RecordedAt)
	return p, err
}

func dateOnly(t time.Time) string { return t.Format(time.DateOnly) }

// InsertPeriod ghi kỳ (trigger chặn mở trùng — unique_violation khtc_period_duplicate).
func InsertPeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Period, idem string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_period (id, tenant_id, stream_key, report_type, plan_year, period_no, scope,
	    period_key, name, period_from, period_to, cutoff_date, due_at, export_due_at, max_submissions, departments, forms_period_ref,
	    template_code, period_rule, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2, 'khtc_period:' || $1::uuid::text, $3, $4, $5, $6, $7, $8, $9::date, $10::date, $11::date, $12, $13, $14,
	    $15, $16, $17, $18, $19, $20, $21, $22)
	  RETURNING recorded_at`,
		p.ID, tid, p.Type, p.Year, p.No, p.Scope, p.Key, p.Name, dateOnly(p.From), dateOnly(p.To), dateOnly(p.Cutoff), p.DueAt.UTC(),
		p.ExportDueAt.UTC(), p.MaxSubmissions, p.Departments, p.FormsPeriodRef, p.TemplateCode, p.Rule, p.EventTime.UTC(), p.ActorRef,
		idem, domain.RuleVersion).Scan(&p.RecordedAt)
}

// PeriodByID đọc một kỳ (nil nếu không có).
func PeriodByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Period, error) {
	p, err := scanPeriod(tx.QueryRow(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// PeriodByIdem đọc kỳ đã ghi với khóa idempotent (gọi lặp "Mở kỳ" — nil nếu chưa có).
func PeriodByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Period, error) {
	p, err := scanPeriod(tx.QueryRow(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Periods liệt kê kỳ (năm kế hoạch year, 0 = mọi năm), mới trước.
func Periods(ctx context.Context, tx pgx.Tx, year, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period
	  WHERE ($1 = 0 OR plan_year = $1) ORDER BY id DESC LIMIT $2`, year, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// PeriodsOfDepartment liệt kê kỳ có phiếu của phòng dept (năm kế hoạch year, 0 = mọi năm), mới trước — W11.11 "Báo cáo
// cho KHTC" phía phòng.
func PeriodsOfDepartment(ctx context.Context, tx pgx.Tx, dept string, year, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period
	  WHERE $1 = ANY(departments) AND ($2 = 0 OR plan_year = $2) ORDER BY id DESC LIMIT $3`, dept, year, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

// LivePeriodByKey trả kỳ chưa hủy cùng loại + khóa (kiểm mở trùng trước khi phát hành phiếu) — nil nếu không có.
func LivePeriodByKey(ctx context.Context, tx pgx.Tx, typ, key string) (*Period, error) {
	p, err := scanPeriod(tx.QueryRow(ctx, `SELECT `+periodCols+` FROM pf_extension.khtc_period p
	  WHERE p.report_type = $1 AND p.period_key = $2
	    AND NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = p.tenant_id AND e.period_id = p.id
	                      AND e.action = 'cancelled')
	  ORDER BY p.id DESC LIMIT 1`, typ, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Event là một sự kiện kỳ KHTC.
type Event struct {
	ID          uuid.UUID
	PeriodID    uuid.UUID
	Action      string // deadline_changed | closed | cancelled
	Forced      bool
	Reason      *string
	DueAt       *time.Time
	ExportDueAt *time.Time
	State       *string // trạng thái chiếu lúc đóng/hủy
	ActorRef    uuid.UUID
	EventTime   time.Time
	Seq         int64
}

// Hành động của sự kiện kỳ KHTC.
const (
	ActionDeadline  = "deadline_changed"
	ActionClosed    = "closed"
	ActionCancelled = "cancelled"
)

// InsertEvent ghi sự kiện (trigger: kỳ có thật, chưa đóng/hủy — check_violation khtc_period_event_terminal).
func InsertEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Event, idem string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_period_event (id, tenant_id, stream_key, period_id, action, forced, reason,
	    due_at, export_due_at, period_state, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'khtc_period_event:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		e.ID, tid, e.PeriodID, e.Action, e.Forced, e.Reason, e.DueAt, e.ExportDueAt, e.State, e.EventTime.UTC(), e.ActorRef, idem,
		domain.RuleVersion).Scan(&e.Seq)
}

// Events đọc sự kiện của các kỳ, theo kỳ rồi seq.
func Events(ctx context.Context, tx pgx.Tx, periodIDs []uuid.UUID) (map[uuid.UUID][]Event, error) {
	out := map[uuid.UUID][]Event{}
	if len(periodIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, period_id, action, forced, reason, due_at, export_due_at, period_state, actor_ref, event_time, seq
	  FROM pf_extension.khtc_period_event WHERE period_id = ANY($1::uuid[]) ORDER BY period_id, seq`, uuidStrings(periodIDs))
	if err != nil {
		return nil, err
	}
	evs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		err := r.Scan(&e.ID, &e.PeriodID, &e.Action, &e.Forced, &e.Reason, &e.DueAt, &e.ExportDueAt, &e.State, &e.ActorRef, &e.EventTime, &e.Seq)
		return e, err
	})
	for _, e := range evs {
		out[e.PeriodID] = append(out[e.PeriodID], e)
	}
	return out, err
}

// Source là một kỳ nguồn đã gắn.
type Source struct {
	ID              uuid.UUID
	PeriodID        uuid.UUID
	Department      string
	Source          string
	CalendarID      string
	CalendarVersion string
	NativeKey       string
	Label           *string
}

// InsertSource gắn một kỳ nguồn (cùng giao dịch mở kỳ — trigger khtc_period_source_open_tx).
func InsertSource(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Source, actor uuid.UUID, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_period_source (id, tenant_id, stream_key, period_id, department, source, calendar_id,
	    calendar_version, native_key, label, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'khtc_period_source:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		s.ID, tid, s.PeriodID, s.Department, s.Source, s.CalendarID, s.CalendarVersion, s.NativeKey, s.Label, at.UTC(), actor, idem,
		domain.RuleVersion)
	return err
}

// Sources đọc kỳ nguồn của một kỳ (theo thứ tự gắn).
func Sources(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) ([]Source, error) {
	rows, err := tx.Query(ctx, `SELECT id, period_id, department, source, calendar_id, calendar_version, native_key, label
	  FROM pf_extension.khtc_period_source WHERE period_id = $1 ORDER BY seq`, periodID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Source, error) {
		var s Source
		err := r.Scan(&s.ID, &s.PeriodID, &s.Department, &s.Source, &s.CalendarID, &s.CalendarVersion, &s.NativeKey, &s.Label)
		return s, err
	})
}

func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

// LastClosedCutoff trả ngày chốt muộn nhất của các kỳ KHTC ĐÃ ĐÓNG thuộc năm kế hoạch year (nil nếu chưa có kỳ đóng).
func LastClosedCutoff(ctx context.Context, tx pgx.Tx, year int) (*time.Time, error) {
	var t *time.Time
	err := tx.QueryRow(ctx, `SELECT max(p.cutoff_date) FROM pf_extension.khtc_period p
	  WHERE p.plan_year = $1
	    AND EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = p.tenant_id AND e.period_id = p.id
	                  AND e.action = 'closed')`, year).Scan(&t)
	return t, err
}
