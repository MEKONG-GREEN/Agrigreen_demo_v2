package store

// W11.44: đọc số liệu kỳ native theo lô cho tổng hợp của module nghiệp vụ (forms/port.FiguresReader). Bốn câu SQL cho
// cả lô (kỳ + trạng thái, bài nộp cuối + rà soát + "không phát sinh", ảnh chụp mới nhất) — không mở từng kỳ.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
)

// FigurePeriod là một kỳ có lịch của lô đọc số liệu.
type FigurePeriod struct {
	Period
	Spec       domain.Spec
	LastAction string // sự kiện cuối: published | closed | reopened (kỳ hủy đã lọc)
}

// FigurePeriods liệt kê kỳ có khóa kỳ đã lưu của profile + mã biểu giao với [from, to], chưa hủy, cũ trước.
func FigurePeriods(ctx context.Context, tx pgx.Tx, profile, code string, from, to time.Time, limit int) ([]FigurePeriod, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodSelect("p.")+`, s.unit_kind, s.unit_parents, s.period_kind, s.period_key, s.period_from,
	    s.period_to, coalesce(s.period_rule, ''),
	    coalesce((SELECT e.action FROM pf_forms.period_event e WHERE e.tenant_id = p.tenant_id AND e.period_ref = p.id
	               ORDER BY e.seq DESC LIMIT 1), 'published')
	  FROM pf_forms.period p
	  JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id AND s.period_key IS NOT NULL
	 WHERE p.profile = $1 AND p.template_code = $2 AND s.period_to >= $3::date AND s.period_from <= $4::date
	 ORDER BY s.period_from, p.id LIMIT $5`, profile, code, from.Format(time.DateOnly), to.Format(time.DateOnly), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FigurePeriod
	for rows.Next() {
		var f FigurePeriod
		var kind, key *string
		p := &f.Period
		if err := rows.Scan(&p.ID, &p.Profile, &p.TemplateID, &p.TemplateCode, &p.TemplateVersion, &p.Label, &p.DueAt, &p.Units, &p.OwnerRef,
			&p.PublisherLevel, &p.ActorRef, &p.EventTime, &f.Spec.UnitKind, &f.Spec.UnitParents, &kind, &key, &f.Spec.From, &f.Spec.To,
			&f.Spec.PeriodRule, &f.LastAction); err != nil {
			return nil, err
		}
		f.Spec.PeriodKind, f.Spec.PeriodKey = deref(kind), deref(key)
		if f.LastAction == "cancelled" {
			continue
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FigureSubmission là bài nộp cuối của một đơn vị trong kỳ.
type FigureSubmission struct {
	PeriodID  uuid.UUID
	Unit      string
	Version   int64
	Values    map[string]string
	NilReport bool
	Decision  string
}

// LatestFigureSubmissions trả bài nộp cuối (kèm "không phát sinh", rà soát) của mọi đơn vị trong các kỳ.
func LatestFigureSubmissions(ctx context.Context, tx pgx.Tx, periodIDs []uuid.UUID) ([]FigureSubmission, error) {
	if len(periodIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT l.period_id, l.unit_code, l.seq, l.vals, coalesce(m.nil_report, false), coalesce(r.decision, '')
	  FROM (SELECT DISTINCT ON (s.period_id, s.unit_code) s.id, s.period_id, s.unit_code, s.seq, s."values"::text AS vals
	          FROM pf_forms.submission s WHERE s.period_id = ANY($1::text[]::uuid[])
	         ORDER BY s.period_id, s.unit_code, s.seq DESC) l
	  LEFT JOIN pf_forms.submission_meta m ON m.submission_id = l.id
	  LEFT JOIN pf_forms.review r ON r.submission_id = l.id`, uuidStrings(periodIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FigureSubmission
	for rows.Next() {
		var f FigureSubmission
		var vals string
		if err := rows.Scan(&f.PeriodID, &f.Unit, &f.Version, &vals, &f.NilReport, &f.Decision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(vals), &f.Values); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FigureSnapshot là ảnh chụp đóng băng mới nhất của một kỳ (nội dung đã giải).
type FigureSnapshot struct {
	PeriodID uuid.UUID
	No       int64
	At       time.Time
	Content  domain.Snapshot
}

// LatestFigureSnapshots trả ảnh chụp mới nhất của các kỳ (kỳ chưa chốt lần nào vắng).
func LatestFigureSnapshots(ctx context.Context, tx pgx.Tx, periodIDs []uuid.UUID) ([]FigureSnapshot, error) {
	if len(periodIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (c.period_ref) c.period_ref, c.seq, c.event_time, c.content::text
	  FROM pf_forms.close_snapshot c WHERE c.period_ref = ANY($1::text[]::uuid[])
	 ORDER BY c.period_ref, c.seq DESC`, uuidStrings(periodIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FigureSnapshot
	for rows.Next() {
		var f FigureSnapshot
		var content string
		if err := rows.Scan(&f.PeriodID, &f.No, &f.At, &content); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(content), &f.Content); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
