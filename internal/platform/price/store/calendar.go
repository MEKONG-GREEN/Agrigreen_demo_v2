package store

// SQL lịch báo giá, ngoại lệ, ngưỡng, nhật ký job (pf 000146, W11.74). Chạy trong giao dịch của module (vai r_price);
// RLS p_tenant lọc tenant. Sổ chỉ ghi thêm. Số % đọc/ghi qua text ↔ decimal (không float — R-CODE-01).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
)

// CalMeta là cột chung của một dòng sổ do app ghi.
type CalMeta struct {
	ID        uuid.UUID
	Tenant    uuid.UUID
	EventTime time.Time
	Actor     uuid.UUID
	Idem      string
}

// RuleRow là một phiên bản lịch kèm người ghi (lịch sử thay đổi).
type RuleRow struct {
	Profile    string
	Schedule   domain.Schedule
	ActorRef   uuid.UUID
	RecordedAt time.Time
}

func todText(t domain.TimeOfDay) string { return t.String() }

// CalendarRules trả mọi phiên bản lịch của bên dùng (mới nhất trước), tối đa limit.
func CalendarRules(ctx context.Context, tx pgx.Tx, profile string, limit int) ([]RuleRow, error) {
	rows, err := tx.Query(ctx, `SELECT weekdays, to_char(lock_at, 'HH24:MI'), to_char(respond_by, 'HH24:MI'),
	    to_char(remind1_at, 'HH24:MI'), to_char(remind2_at, 'HH24:MI'), notify_in_app, notify_email,
	    template, valid_from, seq, actor_ref, recorded_at
	  FROM pf_price.calendar_rule WHERE profile = $1 ORDER BY seq DESC LIMIT $2`, profile, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RuleRow, error) {
		var x RuleRow
		var wd int16
		var lock, resp, r1, r2 string
		s := &x.Schedule
		if err := r.Scan(&wd, &lock, &resp, &r1, &r2, &s.InApp, &s.Email, &s.Template, &s.ValidFrom, &s.Version, &x.ActorRef, &x.RecordedAt); err != nil {
			return x, err
		}
		s.Weekdays = domain.Weekdays(wd) //nolint:gosec // CHECK 1..127
		x.Profile = profile
		var err error
		for _, f := range []struct {
			dst *domain.TimeOfDay
			src string
		}{{&s.LockAt, lock}, {&s.RespondBy, resp}, {&s.Remind1, r1}, {&s.Remind2, r2}} {
			if *f.dst, err = domain.ParseTimeOfDay(f.src); err != nil {
				return x, err
			}
		}
		return x, nil
	})
}

// InsertCalendarRule ghi một phiên bản lịch; seq do trigger đặt.
func InsertCalendarRule(ctx context.Context, tx pgx.Tx, m CalMeta, profile string, s domain.Schedule) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.calendar_rule (id, tenant_id, stream_key, profile, weekdays, lock_at, respond_by,
	    remind1_at, remind2_at, notify_in_app, notify_email, template, valid_from, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'calendar:' || $3::text, $3::text, $4, $5::time, $6::time, $7::time, $8::time, $9, $10, $11, $12::date, $13, $14, $15, $16)
	  RETURNING seq`,
		m.ID, m.Tenant, profile, int16(s.Weekdays), todText(s.LockAt), todText(s.RespondBy), todText(s.Remind1), todText(s.Remind2),
		s.InApp, s.Email, s.Template, s.ValidFrom.Format(time.DateOnly), m.EventTime.UTC(), m.Actor, m.Idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// Exceptions trả ngoại lệ đang hiệu lực (phiên bản mới nhất của mỗi ngày, status active) trong [from, to].
func Exceptions(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) ([]domain.Exception, error) {
	rows, err := tx.Query(ctx, `SELECT day, kind, reason, seq FROM (
	    SELECT DISTINCT ON (day) day, kind, reason, seq, status FROM pf_price.calendar_exception
	     WHERE profile = $1 AND day BETWEEN $2::date AND $3::date ORDER BY day, seq DESC) v
	  WHERE status = 'active' ORDER BY day`, profile, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Exception, error) {
		var e domain.Exception
		err := r.Scan(&e.Day, &e.Kind, &e.Reason, &e.Version)
		return e, err
	})
}

// InsertException ghi một phiên bản ngoại lệ (status 'active' hoặc 'retired').
func InsertException(ctx context.Context, tx pgx.Tx, m CalMeta, profile string, e domain.Exception, status string) (int64, error) {
	day := e.Day.Format(time.DateOnly)
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.calendar_exception (id, tenant_id, stream_key, profile, day, kind, reason, status,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'exception:' || $3::text || ':' || $4::text, $3::text, $4::date, $5, $6, $7, $8, $9, $10, $11) RETURNING seq`,
		m.ID, m.Tenant, profile, day, e.Kind, e.Reason, status, m.EventTime.UTC(), m.Actor, m.Idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// ThresholdRow là một phiên bản ngưỡng kèm người ghi.
type ThresholdRow struct {
	Threshold  domain.Threshold
	Status     string
	ActorRef   uuid.UUID
	RecordedAt time.Time
}

func scanThreshold(r pgx.CollectableRow) (ThresholdRow, error) {
	var x ThresholdRow
	var pct string
	var lookback *int32
	var flat, survey *string
	t := &x.Threshold
	if err := r.Scan(&t.Scope, &t.Code, &pct, &lookback, &flat, &survey, &x.Status, &t.Version, &x.ActorRef, &x.RecordedAt); err != nil {
		return x, err
	}
	var err error
	if t.Pct, err = decimal.NewFromString(pct); err != nil {
		return x, err
	}
	if lookback != nil && flat != nil && survey != nil {
		p := &domain.AlertParams{LookbackDays: int(*lookback)}
		if p.FlatPct, err = decimal.NewFromString(*flat); err != nil {
			return x, err
		}
		if p.SurveyPct, err = decimal.NewFromString(*survey); err != nil {
			return x, err
		}
		t.Params = p
	}
	return x, nil
}

const thresholdCols = `scope, scope_code, pct::text, lookback_days, flat_pct::text, survey_pct::text, status, seq, actor_ref, recorded_at`

// Thresholds trả phiên bản mới nhất của mỗi phạm vi đang hiệu lực (bỏ 'retired').
func Thresholds(ctx context.Context, tx pgx.Tx, profile string) ([]ThresholdRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+thresholdCols+` FROM (
	    SELECT DISTINCT ON (scope, scope_code) * FROM pf_price.alert_threshold WHERE profile = $1
	     ORDER BY scope, scope_code, seq DESC) v
	  WHERE status = 'active' ORDER BY scope, scope_code`, profile)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanThreshold)
}

// ThresholdHistory trả mọi phiên bản ngưỡng (mới nhất trước), tối đa limit — lịch sử thay đổi (BR-04).
func ThresholdHistory(ctx context.Context, tx pgx.Tx, profile string, limit int) ([]ThresholdRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+thresholdCols+` FROM pf_price.alert_threshold WHERE profile = $1
	  ORDER BY recorded_at DESC, seq DESC LIMIT $2`, profile, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanThreshold)
}

// InsertThreshold ghi một phiên bản ngưỡng.
func InsertThreshold(ctx context.Context, tx pgx.Tx, m CalMeta, profile string, t domain.Threshold, status string) (int64, error) {
	var lookback *int
	var flat, survey *string
	if t.Params != nil {
		lb, f, s := t.Params.LookbackDays, t.Params.FlatPct.String(), t.Params.SurveyPct.String()
		lookback, flat, survey = &lb, &f, &s
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.alert_threshold (id, tenant_id, stream_key, profile, scope, scope_code, pct,
	    lookback_days, flat_pct, survey_pct, status, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'threshold:' || $3::text || ':' || $4::text || ':' || $5::text, $3::text, $4::text, $5::text, $6::numeric,
	    $7, $8::numeric, $9::numeric, $10, $11, $12, $13, $14) RETURNING seq`,
		m.ID, m.Tenant, profile, t.Scope, t.Code, t.Pct.String(), lookback, flat, survey, status, m.EventTime.UTC(), m.Actor, m.Idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// Run là một dòng nhật ký job lịch.
type Run struct {
	Day         time.Time
	Kind        string
	ScheduledAt time.Time
	RanAt       time.Time
	Recipients  int
	Targets     int
	Note        string
}

// InsertRun ghi một lần chạy (CHECK seq = 1; idem_key trùng → ledger.ErrDuplicate qua ledger.Classify).
func InsertRun(ctx context.Context, tx pgx.Tx, m CalMeta, profile string, r Run) error {
	var note *string
	if r.Note != "" {
		note = &r.Note
	}
	day := r.Day.Format(time.DateOnly)
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.calendar_run (id, tenant_id, stream_key, profile, day, kind, scheduled_at, recipients,
	    targets, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'run:' || $3::text || ':' || $4::text || ':' || $5::text, $3::text, $4::date, $5::text, $6, $7, $8, $9, $10, $11, $12, $13)`,
		m.ID, m.Tenant, profile, day, r.Kind, r.ScheduledAt.UTC(), r.Recipients, r.Targets, note, m.EventTime.UTC(), m.Actor, m.Idem,
		domain.RuleVersion)
	return err
}

// RunKinds trả loại lần chạy đã ghi của ngày.
func RunKinds(ctx context.Context, tx pgx.Tx, profile string, day time.Time) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT kind FROM pf_price.calendar_run WHERE profile = $1 AND day = $2::date`, profile, day.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		out[k] = true
	}
	return out, nil
}

// Runs trả nhật ký job trong [from, to], mới nhất trước.
func Runs(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) ([]Run, error) {
	rows, err := tx.Query(ctx, `SELECT day, kind, scheduled_at, event_time, recipients, targets, coalesce(note, '')
	  FROM pf_price.calendar_run WHERE profile = $1 AND day BETWEEN $2::date AND $3::date ORDER BY day DESC, scheduled_at DESC`,
		profile, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) {
		var x Run
		err := r.Scan(&x.Day, &x.Kind, &x.ScheduledAt, &x.RanAt, &x.Recipients, &x.Targets, &x.Note)
		return x, err
	})
}
