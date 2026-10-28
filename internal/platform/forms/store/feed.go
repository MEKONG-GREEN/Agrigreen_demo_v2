package store

// W11.43: đọc bài nộp cho quy tắc liên biểu + cảnh báo của bên dùng (port.SubmissionFeed) — bài nộp cuối của từng
// (kỳ, đơn vị) kèm đặc tả kỳ (khóa, khoảng ngày, xã cha) và cờ không phát sinh.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FeedSubmission là một phiên bản bài nộp kèm kỳ.
type FeedSubmission struct {
	Submission
	Profile      string
	TemplateCode string
	PeriodLabel  string
	DueAt        time.Time
	UnitKind     string
	Parent       string
	PeriodKind   string
	PeriodKey    string
	From, To     *time.Time
	NilReport    bool
}

const feedSelect = `SELECT x.id, x.period_id, x.unit_code, x.seq, x."values"::text, x.submitter_ref, x.event_time, x.recorded_at,
	    p.profile, p.template_code, p.label, pf_forms.effective_due(p.tenant_id, p.id, p.due_at),
	    coalesce(s.unit_kind, 'commune'),
	    coalesce(nullif(s.unit_parents[array_position(p.units, x.unit_code)], ''), x.unit_code),
	    coalesce(s.period_kind, ''), coalesce(s.period_key, ''), s.period_from, s.period_to, coalesce(m.nil_report, false)
	  FROM pf_forms.submission x
	  JOIN pf_forms.period p ON p.tenant_id = x.tenant_id AND p.id = x.period_id
	  LEFT JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id
	  LEFT JOIN pf_forms.submission_meta m ON m.tenant_id = x.tenant_id AND m.submission_id = x.id`

func scanFeed(r pgx.Row) (FeedSubmission, error) {
	var f FeedSubmission
	var vals string
	err := r.Scan(&f.ID, &f.PeriodID, &f.Unit, &f.Version, &vals, &f.Submitter, &f.EventTime, &f.RecordedAt, &f.Profile, &f.TemplateCode,
		&f.PeriodLabel, &f.DueAt, &f.UnitKind, &f.Parent, &f.PeriodKind, &f.PeriodKey, &f.From, &f.To, &f.NilReport)
	if err != nil {
		return f, err
	}
	return f, jsonUnmarshal(vals, &f.Values)
}

// FeedByID đọc một phiên bản bài nộp kèm kỳ (nil nếu không có).
func FeedByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*FeedSubmission, error) {
	rows, err := tx.Query(ctx, feedSelect+` WHERE x.id = $1`, id)
	if err != nil {
		return nil, err
	}
	fs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (FeedSubmission, error) { return scanFeed(r) })
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}

// FeedLatest trả bài nộp cuối của từng (kỳ, đơn vị) của profile + mã biểu, lọc đơn vị/xã cha và ngày đầu kỳ; mới trước.
func FeedLatest(ctx context.Context, tx pgx.Tx, profile, code string, units, parents []string, from, to *time.Time, limit int, excludedKinds []string) ([]FeedSubmission, error) {
	if units == nil {
		units = []string{}
	}
	if parents == nil {
		parents = []string{}
	}
	if excludedKinds == nil {
		excludedKinds = []string{}
	}
	rows, err := tx.Query(ctx, `WITH last AS (
	    SELECT DISTINCT ON (x.period_id, x.unit_code) x.id FROM pf_forms.submission x
	      JOIN pf_forms.period p ON p.tenant_id = x.tenant_id AND p.id = x.period_id
	      LEFT JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id
	     WHERE p.profile = $1 AND p.template_code = $2
	       AND NOT (coalesce(s.period_kind, '') = ANY($8::text[]))
	       AND ($5::date IS NULL OR s.period_from >= $5::date) AND ($6::date IS NULL OR s.period_from <= $6::date)
	       AND ((cardinality($3::text[]) = 0 AND cardinality($4::text[]) = 0) OR x.unit_code = ANY($3::text[])
	            OR coalesce(nullif(s.unit_parents[array_position(p.units, x.unit_code)], ''), x.unit_code) = ANY($4::text[]))
	     ORDER BY x.period_id, x.unit_code, x.seq DESC)
	  `+feedSelect+` JOIN last ON last.id = x.id ORDER BY s.period_from DESC NULLS LAST, x.period_id DESC, x.unit_code LIMIT $7`,
		profile, code, units, parents, dateOrNil(from), dateOrNil(to), limit, excludedKinds)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FeedSubmission, error) { return scanFeed(r) })
}

// DuePeriodIDs trả mã kỳ của profile có hạn nộp hiệu lực trong [from, to] (kỳ hủy lọc ở tầng trên theo sự kiện).
func DuePeriodIDs(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT p.id FROM pf_forms.period p
	   WHERE p.profile = $1 AND pf_forms.effective_due(p.tenant_id, p.id, p.due_at) BETWEEN $2 AND $3
	   ORDER BY p.id LIMIT $4`, profile, from.UTC(), to.UTC(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
