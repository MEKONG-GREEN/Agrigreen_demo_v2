package store

// W11.44: SQL của tổng hợp CNTS Cấp Tỉnh (pf 20261013230931 — pf_extension.cnts_bm02_adjustment, cnts_aqua_entry,
// cnts_agg_close, cnts_agg_share). Bốn sổ chỉ ghi thêm (shared.apply_ledger); luôn trong giao dịch có ngữ cảnh tenant.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

func decJSON(d domain.Dec) (string, error) {
	m := map[string]string{}
	for k, v := range d {
		m[k] = v.String()
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func jsonDec(s string) (domain.Dec, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	out := domain.Dec{}
	for k, v := range m {
		d, err := decimal.NewFromString(v)
		if err != nil {
			return nil, err
		}
		out[k] = d
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Dòng điều chỉnh BM-02
// ---------------------------------------------------------------------------

// AdjustmentRow là một dòng điều chỉnh đã ghi.
type AdjustmentRow struct {
	domain.BM02Adjustment
	Province   string
	RecordedAt time.Time
}

const adjCols = `id, province_code, week_key, facility_ref::text, coalesce(commune_code, ''), deltas::text, base::text, reason, actor_ref,
  event_time, recorded_at`

func scanAdj(row pgx.Row) (AdjustmentRow, error) {
	var a AdjustmentRow
	var deltas, base string
	if err := row.Scan(&a.ID, &a.Province, &a.WeekKey, &a.Facility, &a.Commune, &deltas, &base, &a.Reason, &a.ActorRef, &a.At,
		&a.RecordedAt); err != nil {
		return a, err
	}
	var err error
	if a.Deltas, err = jsonDec(deltas); err != nil {
		return a, err
	}
	a.Base, err = jsonDec(base)
	return a, err
}

// InsertAdjustment ghi dòng điều chỉnh (trigger chặn tuần đã chốt — check_violation cnts_agg_input_closed).
func InsertAdjustment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *AdjustmentRow, idem string) error {
	deltas, err := decJSON(a.Deltas)
	if err != nil {
		return err
	}
	base, err := decJSON(a.Base)
	if err != nil {
		return err
	}
	var commune *string
	if a.Commune != "" {
		commune = &a.Commune
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_bm02_adjustment (id, tenant_id, stream_key, province_code, week_key, facility_ref,
	    commune_code, deltas, base, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_bm02_adj:' || $3::text || ':' || $4::text, $3, $4, $5::uuid, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13)
	  RETURNING recorded_at`,
		a.ID, tid, a.Province, a.WeekKey, a.Facility, commune, deltas, base, a.Reason, a.At.UTC(), a.ActorRef, idem,
		domain.AggRuleVersion).Scan(&a.RecordedAt)
}

// AdjustmentByIdem đọc dòng điều chỉnh đã ghi với khóa idem (nil nếu chưa có).
func AdjustmentByIdem(ctx context.Context, tx pgx.Tx, idem string) (*AdjustmentRow, error) {
	a, err := scanAdj(tx.QueryRow(ctx, `SELECT `+adjCols+` FROM pf_extension.cnts_bm02_adjustment WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// Adjustments liệt kê dòng điều chỉnh của tỉnh trong các tuần KSGM, cũ trước.
func Adjustments(ctx context.Context, tx pgx.Tx, province string, weekKeys []string) ([]AdjustmentRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+adjCols+` FROM pf_extension.cnts_bm02_adjustment
	  WHERE province_code = $1 AND week_key = ANY($2::text[]) ORDER BY recorded_at, seq`, province, weekKeys)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdjustmentRow, error) { return scanAdj(r) })
}

// ---------------------------------------------------------------------------
// Số nuôi trồng
// ---------------------------------------------------------------------------

// AquaRow là một dòng nuôi trồng đã ghi.
type AquaRow struct {
	ID           uuid.UUID
	Province     string
	WeekKey      string
	From, To     time.Time
	Object       string
	Area, Output *decimal.Decimal
	Note         string
	ActorRef     uuid.UUID
	RecordedAt   time.Time
}

// InsertAqua ghi một dòng nuôi trồng (trigger chặn tuần BM-18 đã chốt).
func InsertAqua(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *AquaRow, at time.Time, idem string) error {
	var note *string
	if a.Note != "" {
		note = &a.Note
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_aqua_entry (id, tenant_id, stream_key, province_code, week_key, week_from, week_to,
	    object_code, area, output, note, catalog_version, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_aqua:' || $3::text || ':' || $4::text, $3, $4, $5::date, $6::date, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	  RETURNING recorded_at`,
		a.ID, tid, a.Province, a.WeekKey, a.From.Format(time.DateOnly), a.To.Format(time.DateOnly), a.Object, a.Area, a.Output, note,
		domain.AquaCatalogVersion, at.UTC(), a.ActorRef, idem, domain.AggRuleVersion).Scan(&a.RecordedAt)
}

// AquaExists cho biết đã có dòng với khóa idem (gửi lặp).
func AquaExists(ctx context.Context, tx pgx.Tx, idem string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_extension.cnts_aqua_entry WHERE idem_key = $1)`, idem).Scan(&ok)
	return ok, err
}

// AquaCurrent trả dòng nuôi trồng HIỆN HÀNH (dòng cuối mỗi tuần × đối tượng) của tỉnh có ngày cuối tuần trong [from, to].
func AquaCurrent(ctx context.Context, tx pgx.Tx, province string, from, to time.Time) ([]AquaRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (week_key, object_code) id, province_code, week_key, week_from, week_to, object_code,
	    area, output, coalesce(note, ''), actor_ref, recorded_at
	  FROM pf_extension.cnts_aqua_entry
	 WHERE province_code = $1 AND week_to BETWEEN $2::date AND $3::date
	 ORDER BY week_key, object_code, seq DESC`, province, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AquaRow, error) {
		var a AquaRow
		err := r.Scan(&a.ID, &a.Province, &a.WeekKey, &a.From, &a.To, &a.Object, &a.Area, &a.Output, &a.Note, &a.ActorRef, &a.RecordedAt)
		return a, err
	})
}

// ---------------------------------------------------------------------------
// Chốt / mở lại bảng tổng hợp
// ---------------------------------------------------------------------------

// AggEvent là một lần chốt/mở lại bảng tổng hợp.
type AggEvent struct {
	ID          uuid.UUID
	AggPeriodID uuid.UUID
	Province    string
	Form        string
	DefVersion  uuid.UUID
	PeriodType  string
	PeriodKey   string
	From, To    time.Time
	Rule, Label string
	Action      string // closed | reopened
	Reason      string
	Pub         *domain.Pub // closed
	FiguresHash string
	Sources     json.RawMessage
	Seq         int64
	ActorRef    uuid.UUID
	At          time.Time
}

const aggCols = `id, agg_period_id, province_code, form_code, def_version_id, period_type, period_key, period_from, period_to, period_rule,
  period_label, action, coalesce(reason, ''), rows::text, totals::text, units_total, units_with_data, missing_units, provisional,
  coalesce(figures_hash, ''), coalesce(sources::text, ''), seq, actor_ref, event_time`

func scanAgg(row pgx.Row) (AggEvent, error) {
	var e AggEvent
	var rows, totals *string
	var ut, uw *int
	var miss []string
	var prov *bool
	var src string
	if err := row.Scan(&e.ID, &e.AggPeriodID, &e.Province, &e.Form, &e.DefVersion, &e.PeriodType, &e.PeriodKey, &e.From, &e.To, &e.Rule,
		&e.Label, &e.Action, &e.Reason, &rows, &totals, &ut, &uw, &miss, &prov, &e.FiguresHash, &src, &e.Seq, &e.ActorRef, &e.At); err != nil {
		return e, err
	}
	if src != "" {
		e.Sources = json.RawMessage(src)
	}
	if rows != nil && totals != nil {
		p := &domain.Pub{MissingUnits: miss}
		if err := json.Unmarshal([]byte(*rows), &p.Rows); err != nil {
			return e, err
		}
		if err := json.Unmarshal([]byte(*totals), &p.Totals); err != nil {
			return e, err
		}
		if ut != nil {
			p.UnitsTotal = *ut
		}
		if uw != nil {
			p.UnitsWithData = *uw
		}
		if prov != nil {
			p.Provisional = *prov
		}
		e.Pub = p
	}
	return e, nil
}

// InsertAgg ghi một lần chốt/mở lại (trigger kiểm máy trạng thái — check_violation cnts_agg_close_state).
func InsertAgg(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *AggEvent, idem string) error {
	var rows, totals *string
	var ut, uw *int
	var miss []string
	var prov *bool
	var hash, src, reason *string
	if e.Pub != nil {
		rb, err := json.Marshal(e.Pub.Rows)
		if err != nil {
			return err
		}
		tb, err := json.Marshal(e.Pub.Totals)
		if err != nil {
			return err
		}
		r, t := string(rb), string(tb)
		rows, totals, ut, uw, prov = &r, &t, &e.Pub.UnitsTotal, &e.Pub.UnitsWithData, &e.Pub.Provisional
		miss = e.Pub.MissingUnits
		if miss == nil {
			miss = []string{}
		}
		h, s := e.FiguresHash, string(e.Sources)
		hash, src = &h, &s
	}
	if e.Reason != "" {
		reason = &e.Reason
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_agg_close (id, tenant_id, stream_key, agg_period_id, province_code, form_code,
	    def_version_id, period_type, period_key, period_from, period_to, period_rule, period_label, action, reason, rows, totals, units_total,
	    units_with_data, missing_units, provisional, figures_hash, sources, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_agg:' || $3::text || ':' || $4::text || ':' || $5::text || ':' || $6::text, $7, $3, $4, $8, $5, $6, $9::date,
	    $10::date, $11, $12, $13, $14, $15::jsonb, $16::jsonb, $17, $18, $19, $20, $21, $22::jsonb, $23, $24, $25, $26)
	  RETURNING seq`,
		e.ID, tid, e.Province, e.Form, e.PeriodType, e.PeriodKey, e.AggPeriodID, e.DefVersion, e.From.Format(time.DateOnly),
		e.To.Format(time.DateOnly), e.Rule, e.Label, e.Action, reason, rows, totals, ut, uw, miss, prov, hash, src, e.At.UTC(), e.ActorRef,
		idem, domain.AggRuleVersion).Scan(&e.Seq)
}

// AggByIdem đọc lần chốt/mở lại đã ghi với khóa idem (nil nếu chưa có).
func AggByIdem(ctx context.Context, tx pgx.Tx, idem string) (*AggEvent, error) {
	e, err := scanAgg(tx.QueryRow(ctx, `SELECT `+aggCols+` FROM pf_extension.cnts_agg_close WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// AggEvents liệt kê các lần chốt/mở lại của một bảng tổng hợp, cũ trước.
func AggEvents(ctx context.Context, tx pgx.Tx, province, form, periodType, key string) ([]AggEvent, error) {
	rows, err := tx.Query(ctx, `SELECT `+aggCols+` FROM pf_extension.cnts_agg_close
	  WHERE stream_key = 'cnts_agg:' || $1::text || ':' || $2::text || ':' || $3::text || ':' || $4::text ORDER BY seq`,
		province, form, periodType, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AggEvent, error) { return scanAgg(r) })
}

// LastAggActions trả sự kiện cuối (closed | reopened) của các bảng tổng hợp theo khóa kỳ (vắng = chưa chốt lần nào).
func LastAggActions(ctx context.Context, tx pgx.Tx, province, form, periodType string, keys []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (period_key) period_key, action FROM pf_extension.cnts_agg_close
	  WHERE province_code = $1 AND form_code = $2 AND period_type = $3 AND period_key = ANY($4::text[])
	  ORDER BY period_key, seq DESC`, province, form, periodType, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, a string
		if err := rows.Scan(&k, &a); err != nil {
			return nil, err
		}
		out[k] = a
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Phiên bản tệp chia sẻ Chi cục
// ---------------------------------------------------------------------------

// Share là một phiên bản tệp chia sẻ.
type Share struct {
	ID          uuid.UUID
	Province    string
	Form        string
	PeriodType  string
	PeriodKey   string
	Basis       string
	Content     json.RawMessage // nil khi liệt kê
	ContentHash string
	Recipient   string
	Channel     string
	Note        string
	Version     int64
	ActorRef    uuid.UUID
	At          time.Time
}

const shareCols = `id, province_code, form_code, period_type, period_key, basis, content_hash, recipient, channel, coalesce(note, ''), seq,
  actor_ref, event_time`

func scanShare(row pgx.Row, extra ...any) (Share, error) {
	var s Share
	err := row.Scan(append([]any{&s.ID, &s.Province, &s.Form, &s.PeriodType, &s.PeriodKey, &s.Basis, &s.ContentHash, &s.Recipient,
		&s.Channel, &s.Note, &s.Version, &s.ActorRef, &s.At}, extra...)...)
	return s, err
}

// InsertShare ghi một phiên bản tệp chia sẻ (seq = số phiên bản).
func InsertShare(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Share, idem string) error {
	var note *string
	if s.Note != "" {
		note = &s.Note
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_agg_share (id, tenant_id, stream_key, province_code, form_code, period_type,
	    period_key, basis, content, content_hash, recipient, channel, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_share:' || $3::text || ':' || $4::text || ':' || $5::text || ':' || $6::text, $3, $4, $5, $6, $7, $8::jsonb, $9,
	    $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq`,
		s.ID, tid, s.Province, s.Form, s.PeriodType, s.PeriodKey, s.Basis, string(s.Content), s.ContentHash, s.Recipient, s.Channel, note,
		s.At.UTC(), s.ActorRef, idem, domain.AggRuleVersion).Scan(&s.Version)
}

// ShareByIdem đọc phiên bản đã ghi với khóa idem (nil nếu chưa có).
func ShareByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Share, error) {
	s, err := scanShare(tx.QueryRow(ctx, `SELECT `+shareCols+` FROM pf_extension.cnts_agg_share WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// Shares liệt kê phiên bản tệp chia sẻ của một bảng (cũ trước, không kèm nội dung).
func Shares(ctx context.Context, tx pgx.Tx, province, form, periodType, key string) ([]Share, error) {
	rows, err := tx.Query(ctx, `SELECT `+shareCols+` FROM pf_extension.cnts_agg_share
	  WHERE stream_key = 'cnts_share:' || $1::text || ':' || $2::text || ':' || $3::text || ':' || $4::text ORDER BY seq`,
		province, form, periodType, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Share, error) { return scanShare(r) })
}

// ShareByID đọc một phiên bản kèm nội dung (nil nếu không có).
func ShareByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Share, error) {
	var content string
	s, err := scanShare(tx.QueryRow(ctx, `SELECT `+shareCols+`, content::text FROM pf_extension.cnts_agg_share WHERE id = $1`, id), &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	s.Content = json.RawMessage(content)
	return &s, err
}
