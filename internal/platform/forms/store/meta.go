package store

// W11.42 (pf 000149): sổ phụ thông tin phiên bản bài nộp (khóa lạc quan, báo không phát sinh, ghi chú, giải trình,
// nộp trễ, thời điểm ghi offline) và nguồn con trỏ đồng bộ ngoại tuyến sync_feed.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Meta là thông tin của một phiên bản bài nộp.
type Meta struct {
	SubmissionID uuid.UUID
	BaseVersion  int64
	NilReport    bool
	Note         string
	Explanation  string
	CapturedAt   *time.Time
	Late         bool // DB tính
}

// InsertMeta ghi thông tin phiên bản bài nộp CÙNG GIAO DỊCH nộp (trigger kiểm base_version = seq − 1 — sai → AGF04;
// tính late).
func InsertMeta(ctx context.Context, tx pgx.Tx, tid uuid.UUID, m *Meta, actor uuid.UUID, at time.Time, device, idem string) error {
	var cap *time.Time
	if m.CapturedAt != nil {
		c := m.CapturedAt.UTC()
		cap = &c
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.submission_meta (id, tenant_id, stream_key, submission_id, period_id, unit_code, profile,
	    base_version, nil_report, note, explanation, captured_at, late, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, 'meta:' || $3::uuid::text, $3::uuid, $3::uuid, '', '', $4, $5, $6, $7, $8, false, $9, $10, $11, $12, $13) RETURNING late`,
		ids.New(), tid, m.SubmissionID, m.BaseVersion, m.NilReport, nilIfEmpty(m.Note), nilIfEmpty(m.Explanation), cap, at.UTC(), actor,
		nilIfEmpty(device), idem, domain.RuleVersion).Scan(&m.Late)
}

// MetaOf trả thông tin theo mã phiên bản bài nộp (phiên bản ghi trước pf 000149 không có).
func MetaOf(ctx context.Context, tx pgx.Tx, submissionIDs []uuid.UUID) (map[uuid.UUID]Meta, error) {
	out := map[uuid.UUID]Meta{}
	if len(submissionIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT submission_id, base_version, nil_report, coalesce(note, ''), coalesce(explanation, ''), captured_at, late
	    FROM pf_forms.submission_meta WHERE submission_id = ANY($1::text[]::uuid[])`, uuidStrings(submissionIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Meta
		if err := rows.Scan(&m.SubmissionID, &m.BaseVersion, &m.NilReport, &m.Note, &m.Explanation, &m.CapturedAt, &m.Late); err != nil {
			return nil, err
		}
		out[m.SubmissionID] = m
	}
	return out, rows.Err()
}

// HeldByID đọc bài giữ theo mã (nil nếu không có) — nhận lại bản offline cùng mã (đồng bộ gửi lặp).
func HeldByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Held, error) {
	rows, err := tx.Query(ctx, `SELECT id, period_id, unit_code, seq, "values"::text, submitter_ref, captured_at, event_time
	    FROM pf_forms.held_submission WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	hs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Held, error) {
		var h Held
		var vals string
		if err := r.Scan(&h.ID, &h.PeriodID, &h.Unit, &h.Seq, &vals, &h.Submitter, &h.CapturedAt, &h.EventTime); err != nil {
			return h, err
		}
		return h, jsonUnmarshal(vals, &h.Values)
	})
	if err != nil || len(hs) == 0 {
		return nil, err
	}
	return &hs[0], nil
}

// FeedRow là một dòng nguồn đồng bộ đã gom theo (kỳ, đơn vị): feed_seq lớn nhất.
type FeedRow struct {
	Seq      int64
	PeriodID uuid.UUID
	Unit     string // "*" = mọi đơn vị của kỳ
}

// FeedSince trả thay đổi của profile có feed_seq > after (gom theo kỳ × đơn vị, lấy seq lớn nhất), tăng dần, tối đa
// limit.
func FeedSince(ctx context.Context, tx pgx.Tx, profiles []string, after int64, limit int) ([]FeedRow, error) {
	rows, err := tx.Query(ctx, `SELECT max(feed_seq) AS s, period_id, unit_code FROM pf_forms.sync_feed
	    WHERE profile = ANY($1::text[]) AND feed_seq > $2 GROUP BY period_id, unit_code ORDER BY s LIMIT $3`, profiles, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FeedRow, error) {
		var f FeedRow
		err := r.Scan(&f.Seq, &f.PeriodID, &f.Unit)
		return f, err
	})
}

// PreviousCandidate là một kỳ trước của cùng biểu có bài nộp của đơn vị.
type PreviousCandidate struct {
	PeriodID     uuid.UUID
	Label        string
	SubmissionID uuid.UUID
}

// PreviousSubmission tìm kỳ GẦN NHẤT trước kỳ periodID của cùng profile và mã biểu mà đơn vị unit đã nộp (sắp theo
// ngày đầu kỳ của đặc tả, rồi theo mã kỳ UUIDv7 — thời điểm phát hành). Kỳ định kỳ bỏ qua kỳ đột xuất;
// kỳ đột xuất hoặc không có loại kỳ giữ cách chọn cũ. Trả nil nếu không có.
func PreviousSubmission(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, unit string) (*PreviousCandidate, error) {
	rows, err := tx.Query(ctx, `WITH cur AS (
	      SELECT p.profile, p.template_code, p.id, s.period_from, s.period_kind FROM pf_forms.period p
	        LEFT JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id
	       WHERE p.id = $1)
	    SELECT p.id, p.label, sub.id FROM pf_forms.period p
	      JOIN cur ON cur.profile = p.profile AND cur.template_code = p.template_code
	      LEFT JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id
	      JOIN LATERAL (SELECT x.id FROM pf_forms.submission x WHERE x.tenant_id = p.tenant_id AND x.period_id = p.id AND x.unit_code = $2
	                     ORDER BY x.seq DESC LIMIT 1) sub ON true
	     WHERE p.id <> cur.id AND $2 = ANY (p.units)
	       AND (coalesce(cur.period_kind, '') IN ('', 'adhoc') OR coalesce(s.period_kind, '') <> 'adhoc')
	       AND (CASE WHEN cur.period_from IS NOT NULL AND s.period_from IS NOT NULL THEN s.period_from < cur.period_from ELSE p.id < cur.id END)
	     ORDER BY s.period_from DESC NULLS LAST, p.id DESC LIMIT 1`, periodID, unit)
	if err != nil {
		return nil, err
	}
	cs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PreviousCandidate, error) {
		var c PreviousCandidate
		err := r.Scan(&c.PeriodID, &c.Label, &c.SubmissionID)
		return c, err
	})
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return &cs[0], nil
}

// PeriodsByIDs đọc nhiều kỳ native.
func PeriodsByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Period, error) {
	out := map[uuid.UUID]Period{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+periodCols+` FROM pf_forms.period WHERE id = ANY($1::text[]::uuid[])`, uuidStrings(ids))
	if err != nil {
		return nil, err
	}
	ps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
	for _, p := range ps {
		out[p.ID] = p
	}
	return out, err
}

// PeriodsFiltered liệt kê kỳ native của profile (mới trước) lọc tùy chọn theo mã biểu và khoảng ngày của đặc tả
// (kỳ có period_to ≥ from và period_from ≤ to; kỳ không gắn lịch luôn qua).
func PeriodsFiltered(ctx context.Context, tx pgx.Tx, profile, code string, from, to *time.Time, limit int) ([]Period, error) {
	rows, err := tx.Query(ctx, `SELECT `+periodSelect("p.")+` FROM pf_forms.period p
	    LEFT JOIN pf_forms.period_spec s ON s.tenant_id = p.tenant_id AND s.period_id = p.id
	   WHERE p.profile = $1 AND ($2 = '' OR p.template_code = $2)
	     AND ($3::date IS NULL OR s.period_to IS NULL OR s.period_to >= $3::date)
	     AND ($4::date IS NULL OR s.period_from IS NULL OR s.period_from <= $4::date)
	   ORDER BY s.period_from DESC NULLS LAST, p.id DESC LIMIT $5`, profile, code, dateOrNil(from), dateOrNil(to), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Period, error) { return scanPeriod(r) })
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
