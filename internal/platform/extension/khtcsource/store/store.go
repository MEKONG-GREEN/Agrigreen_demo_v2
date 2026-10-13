// Package store: SQL của ảnh chụp "Cập nhật số liệu" (W11.64, pf 20261013175705 — pf_extension.khtc_snapshot,
// khtc_snapshot_row, khtc_snapshot_value, khtc_snapshot_change) trên pool vai của pf/extension, luôn trong giao dịch có ngữ
// cảnh tenant (RLS). Bốn sổ chỉ ghi thêm (shared.apply_ledger); dòng con ghi cùng giao dịch ảnh chụp (trigger).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/domain"
)

// Snapshot là một lần "Cập nhật số liệu" của một kỳ KHTC.
type Snapshot struct {
	ID                    uuid.UUID
	PeriodID              uuid.UUID
	No                    int64 // seq của luồng kỳ
	PeriodFrom, PeriodTo  time.Time
	WindowFrom            time.Time
	TargetCalendarID      string
	TargetCalendarVersion int
	TargetRule            string
	CrosswalkVersion      string
	PlanVersionID         *uuid.UUID
	Closed                bool
	FiguresHash           string
	StatusHash            string
	RowCount              int
	ValueCount            int
	ChangeCount           int
	ActorRef              uuid.UUID
	EventTime             time.Time
	RecordedAt            time.Time
}

const snapCols = `id, period_id, seq, period_from, period_to, window_from, target_calendar_id, target_calendar_version, target_rule,
  crosswalk_version, plan_version_id, closed, figures_hash, status_hash, row_count, value_count, change_count, actor_ref, event_time,
  recorded_at`

func scanSnap(r pgx.Row) (Snapshot, error) {
	var s Snapshot
	err := r.Scan(&s.ID, &s.PeriodID, &s.No, &s.PeriodFrom, &s.PeriodTo, &s.WindowFrom, &s.TargetCalendarID, &s.TargetCalendarVersion,
		&s.TargetRule, &s.CrosswalkVersion, &s.PlanVersionID, &s.Closed, &s.FiguresHash, &s.StatusHash, &s.RowCount, &s.ValueCount,
		&s.ChangeCount, &s.ActorRef, &s.EventTime, &s.RecordedAt)
	return s, err
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

// InsertSnapshot ghi ảnh chụp (trigger: kỳ có thật, chưa đóng/hủy — check_violation khtc_snapshot_terminal); trả seq.
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Snapshot, idem string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_snapshot (id, tenant_id, stream_key, period_id, period_from, period_to,
	    window_from, target_calendar_id, target_calendar_version, target_rule, crosswalk_version, plan_version_id, closed, figures_hash,
	    status_hash, row_count, value_count, change_count, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2, 'khtc_snapshot:' || $3::uuid::text, $3::uuid, $4::date, $5::date, $6::date, $7, $8, $9, $10, $11, $12, $13,
	    $14, $15, $16, $17, $18, $19, $20, $21)
	  RETURNING seq, recorded_at`,
		s.ID, tid, s.PeriodID, day(s.PeriodFrom), day(s.PeriodTo), day(s.WindowFrom), s.TargetCalendarID, s.TargetCalendarVersion,
		s.TargetRule, s.CrosswalkVersion, s.PlanVersionID, s.Closed, s.FiguresHash, s.StatusHash, s.RowCount, s.ValueCount, s.ChangeCount,
		s.EventTime.UTC(), s.ActorRef, idem, domain.RuleVersion).Scan(&s.No, &s.RecordedAt)
}

func oneSnap(r pgx.Row) (*Snapshot, error) {
	s, err := scanSnap(r)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// SnapshotByIdem đọc ảnh chụp đã ghi với khóa idempotent (nil nếu chưa có).
func SnapshotByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Snapshot, error) {
	return oneSnap(tx.QueryRow(ctx, `SELECT `+snapCols+` FROM pf_extension.khtc_snapshot WHERE idem_key = $1`, idem))
}

// LatestSnapshot đọc ảnh chụp mới nhất của kỳ (nil nếu chưa chụp).
func LatestSnapshot(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (*Snapshot, error) {
	return oneSnap(tx.QueryRow(ctx, `SELECT `+snapCols+` FROM pf_extension.khtc_snapshot WHERE period_id = $1 ORDER BY seq DESC LIMIT 1`,
		periodID))
}

// Snapshots liệt kê ảnh chụp của kỳ, mới trước.
func Snapshots(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, limit int) ([]Snapshot, error) {
	rows, err := tx.Query(ctx, `SELECT `+snapCols+` FROM pf_extension.khtc_snapshot WHERE period_id = $1 ORDER BY seq DESC LIMIT $2`,
		periodID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Snapshot, error) { return scanSnap(r) })
}

// InsertRows ghi dòng nguồn của ảnh chụp.
func InsertRows(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Snapshot, rows []domain.Row, newID func() uuid.UUID, idem string) error {
	b := &pgx.Batch{}
	for i, r := range rows {
		var cells []byte
		if r.Cells != nil {
			cells, _ = json.Marshal(r.Cells)
		}
		var from, to *string
		if r.From != nil {
			x := day(*r.From)
			from = &x
		}
		if r.To != nil {
			x := day(*r.To)
			to = &x
		}
		form, native, ptype, cal, conf := nilIfEmpty(r.FormCode), nilIfEmpty(r.NativeKey), nilIfEmpty(r.PeriodType), nilIfEmpty(r.CalendarID),
			nilIfEmpty(r.Confirmation)
		b.Queue(`INSERT INTO pf_extension.khtc_snapshot_row (id, tenant_id, stream_key, snapshot_id, period_id, target_key, department,
		    source_module, form_code, calendar_id, calendar_version, native_key, source_period_type, source_from, source_to, closed, converted,
		    missing, unavailable, fallback, source_closed_at, source_snapshot_no, figures_hash, status_hash, confirmation_status, cells,
		    event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'khtc_snapshot_row:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::date, $14::date, $15,
		    $16, $17, $18, $19, $20, $21, $22, $23, $24, $25::jsonb, $26, $27, $28, $29)`,
			newID(), tid, s.ID, s.PeriodID, r.TargetKey, r.Department, r.SourceModule, form, cal, r.CalendarVersion, native, ptype, from, to,
			r.Closed, r.Converted, r.Missing, r.Unavailable, r.Fallback, r.ClosedAt, r.SnapshotNo, r.FiguresHash, r.StatusHash, conf, cells,
			s.EventTime.UTC(), s.ActorRef, idem+":row:"+strconv.Itoa(i), domain.RuleVersion)
	}
	return tx.SendBatch(ctx, b).Close()
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Rows đọc dòng nguồn của ảnh chụp (theo thứ tự ghi).
func Rows(ctx context.Context, tx pgx.Tx, snapshotID uuid.UUID) ([]domain.Row, error) {
	rows, err := tx.Query(ctx, `SELECT target_key, department, source_module, coalesce(form_code, ''), coalesce(calendar_id, ''), calendar_version,
	    coalesce(native_key, ''), coalesce(source_period_type, ''), source_from, source_to, closed, converted, missing, unavailable, fallback,
	    source_closed_at, source_snapshot_no, figures_hash, status_hash, coalesce(confirmation_status, ''), cells
	  FROM pf_extension.khtc_snapshot_row WHERE snapshot_id = $1 ORDER BY seq`, snapshotID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Row, error) {
		var x domain.Row
		var cells []byte
		err := r.Scan(&x.TargetKey, &x.Department, &x.SourceModule, &x.FormCode, &x.CalendarID, &x.CalendarVersion, &x.NativeKey, &x.PeriodType,
			&x.From, &x.To, &x.Closed, &x.Converted, &x.Missing, &x.Unavailable, &x.Fallback, &x.ClosedAt, &x.SnapshotNo, &x.FiguresHash,
			&x.StatusHash, &x.Confirmation, &cells)
		if err == nil && cells != nil {
			err = json.Unmarshal(cells, &x.Cells)
		}
		return x, err
	})
}

// Value là số đã chụp của một khóa nguồn module.
type Value struct {
	SourceKey string
	EntryRefs []uuid.UUID
	Invalid   bool // khóa nguồn sai dạng (hạng mục vẫn hiện, không có số)
	V         domain.Value
}

// detail là phần JSON của dòng số: kỳ nguồn, hai ngày giá, số từng tuần.
type detail struct {
	SourceKeys []string          `json:"source_keys"`
	PriceDays  []domain.PriceDay `json:"price_days,omitempty"`
	Weeks      []weekDetail      `json:"weeks,omitempty"`
}

type weekDetail struct {
	Key           string `json:"key"`
	Cumulative    string `json:"cumulative"`
	Base          string `json:"base"`
	BaseRef       string `json:"base_ref,omitempty"`
	Increment     string `json:"increment"`
	Sum           string `json:"sum"`
	Closed        bool   `json:"closed"`
	NoSource      bool   `json:"no_source,omitempty"`
	Frozen        bool   `json:"frozen,omitempty"`
	SourceChanged bool   `json:"source_changed,omitempty"`
}

func decArg(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func decOf(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	return &d, err
}

// InsertValues ghi số của các khóa nguồn.
func InsertValues(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Snapshot, vals []Value, newID func() uuid.UUID, idem string) error {
	b := &pgx.Batch{}
	for i, v := range vals {
		d := detail{SourceKeys: v.V.SourceKeys, PriceDays: v.V.PriceDays}
		if d.SourceKeys == nil {
			d.SourceKeys = []string{}
		}
		for _, w := range v.V.Weeks {
			d.Weeks = append(d.Weeks, weekDetail{Key: w.Key, Cumulative: w.Cumulative.String(), Base: w.Base.String(), BaseRef: w.BaseRef,
				Increment: w.Increment.String(), Sum: w.Sum.String(), Closed: w.Closed, NoSource: w.NoSource, Frozen: w.Frozen,
				SourceChanged: w.SourceChanged})
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		mode := v.V.Mode
		if v.Invalid {
			mode = "invalid"
		}
		var priceDay *string
		if v.V.PriceDay != "" {
			priceDay = &v.V.PriceDay
		}
		refs := v.EntryRefs
		if refs == nil {
			refs = []uuid.UUID{}
		}
		b.Queue(`INSERT INTO pf_extension.khtc_snapshot_value (id, tenant_id, stream_key, snapshot_id, period_id, source_key, entry_refs, mode,
		    cumulative, base, base_ref, increment, year_to_date, sum_value, price_min, price_max, price_day, unconfirmed, closed, no_source,
		    frozen, source_changed, fallback, unavailable, detail, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'khtc_snapshot_value:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8::numeric, $9::numeric, $10, $11::numeric,
		    $12::numeric, $13::numeric, $14::numeric, $15::numeric, $16::date, $17::numeric, $18, $19, $20, $21, $22, $23, $24::jsonb, $25, $26,
		    $27, $28)`,
			newID(), tid, s.ID, s.PeriodID, v.SourceKey, refs, mode, decArg(v.V.Cumulative), decArg(v.V.Base), nilIfEmpty(v.V.BaseRef),
			decArg(v.V.Increment), decArg(v.V.YearToDate), decArg(v.V.Sum), decArg(v.V.Min), decArg(v.V.Max), priceDay, decArg(v.V.Unconfirmed),
			v.V.Closed, v.V.NoSource, v.V.Frozen, v.V.SourceChanged, v.V.Fallback, v.V.Unavailable, raw, s.EventTime.UTC(), s.ActorRef,
			idem+":val:"+strconv.Itoa(i), domain.RuleVersion)
	}
	return tx.SendBatch(ctx, b).Close()
}

// Values đọc số đã chụp của ảnh chụp (theo thứ tự ghi).
func Values(ctx context.Context, tx pgx.Tx, snapshotID uuid.UUID) ([]Value, error) {
	return values(ctx, tx, `snapshot_id = $1`, snapshotID)
}

func values(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]Value, error) {
	rows, err := tx.Query(ctx, `SELECT snapshot_id, source_key, entry_refs, mode, cumulative::text, base::text, coalesce(base_ref, ''),
	    increment::text, year_to_date::text, sum_value::text, price_min::text, price_max::text, coalesce(to_char(price_day, 'YYYY-MM-DD'), ''),
	    unconfirmed::text, closed, no_source, frozen, source_changed, fallback, unavailable, detail
	  FROM pf_extension.khtc_snapshot_value WHERE `+where+` ORDER BY snapshot_id, seq`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Value, error) {
		var v Value
		var sid uuid.UUID
		var cum, base, inc, ytd, sum, lo, hi, unc *string
		var raw []byte
		if err := r.Scan(&sid, &v.SourceKey, &v.EntryRefs, &v.V.Mode, &cum, &base, &v.V.BaseRef, &inc, &ytd, &sum, &lo, &hi, &v.V.PriceDay, &unc,
			&v.V.Closed, &v.V.NoSource, &v.V.Frozen, &v.V.SourceChanged, &v.V.Fallback, &v.V.Unavailable, &raw); err != nil {
			return v, err
		}
		v.V.SourceKey = v.SourceKey
		if v.V.Mode == "invalid" {
			v.Invalid, v.V.Mode = true, ""
		}
		for _, p := range []struct {
			s   *string
			dst **decimal.Decimal
		}{{cum, &v.V.Cumulative}, {base, &v.V.Base}, {inc, &v.V.Increment}, {ytd, &v.V.YearToDate}, {sum, &v.V.Sum}, {lo, &v.V.Min},
			{hi, &v.V.Max}, {unc, &v.V.Unconfirmed}} {
			d, err := decOf(p.s)
			if err != nil {
				return v, err
			}
			*p.dst = d
		}
		var d detail
		if err := json.Unmarshal(raw, &d); err != nil {
			return v, err
		}
		v.V.SourceKeys, v.V.PriceDays = d.SourceKeys, d.PriceDays
		for _, w := range d.Weeks {
			wk := domain.Week{Key: w.Key, BaseRef: w.BaseRef, Closed: w.Closed, NoSource: w.NoSource, Frozen: w.Frozen, SourceChanged: w.SourceChanged}
			wk.Cumulative, _ = decimal.NewFromString(w.Cumulative)
			wk.Base, _ = decimal.NewFromString(w.Base)
			wk.Increment, _ = decimal.NewFromString(w.Increment)
			wk.Sum, _ = decimal.NewFromString(w.Sum)
			v.V.Weeks = append(v.V.Weeks, wk)
		}
		return v, nil
	})
}

// Change là một dòng lịch sử thay đổi (người, thời điểm của ảnh chụp).
type Change struct {
	SnapshotID     uuid.UUID
	PrevSnapshotID uuid.UUID
	SourceKey      string
	EntryRefs      []uuid.UUID
	Field          string
	Old, New       *decimal.Decimal
	ActorRef       uuid.UUID
	EventTime      time.Time
}

// InsertChanges ghi lịch sử thay đổi của ảnh chụp.
func InsertChanges(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Snapshot, chs []Change, newID func() uuid.UUID, idem string) error {
	b := &pgx.Batch{}
	for i, c := range chs {
		refs := c.EntryRefs
		if refs == nil {
			refs = []uuid.UUID{}
		}
		b.Queue(`INSERT INTO pf_extension.khtc_snapshot_change (id, tenant_id, stream_key, snapshot_id, prev_snapshot_id, period_id, source_key,
		    entry_refs, field, old_value, new_value, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'khtc_snapshot_change:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11, $12, $13, $14)`,
			newID(), tid, s.ID, c.PrevSnapshotID, s.PeriodID, c.SourceKey, refs, c.Field, decArg(c.Old), decArg(c.New), s.EventTime.UTC(),
			s.ActorRef, idem+":chg:"+strconv.Itoa(i), domain.RuleVersion)
	}
	return tx.SendBatch(ctx, b).Close()
}

// Changes đọc lịch sử thay đổi của kỳ, ảnh chụp mới trước.
func Changes(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) ([]Change, error) {
	rows, err := tx.Query(ctx, `SELECT c.snapshot_id, c.prev_snapshot_id, c.source_key, c.entry_refs, c.field, c.old_value::text,
	    c.new_value::text, c.actor_ref, c.event_time
	  FROM pf_extension.khtc_snapshot_change c WHERE c.period_id = $1 ORDER BY c.event_time DESC, c.snapshot_id DESC, c.seq`, periodID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Change, error) {
		var c Change
		var o, n *string
		if err := r.Scan(&c.SnapshotID, &c.PrevSnapshotID, &c.SourceKey, &c.EntryRefs, &c.Field, &o, &n, &c.ActorRef, &c.EventTime); err != nil {
			return c, err
		}
		var err error
		if c.Old, err = decOf(o); err != nil {
			return c, err
		}
		c.New, err = decOf(n)
		return c, err
	})
}

// FrozenWeeks trả số đã giữ của các tuần KHTC đã đóng trong weekKeys: kỳ KHTC loại tuần, đã đóng (không hủy), ảnh chụp
// cuối của kỳ → khóa tuần → khóa nguồn → số (Q7: tuần đã đóng giữ số).
func FrozenWeeks(ctx context.Context, tx pgx.Tx, weekKeys []string) (map[string]map[string]domain.Frozen, error) {
	out := map[string]map[string]domain.Frozen{}
	if len(weekKeys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT p.period_key, v.source_key, v.cumulative::text, v.increment::text, v.year_to_date::text, v.unconfirmed::text
	  FROM pf_extension.khtc_period p
	  JOIN LATERAL (SELECT s.id FROM pf_extension.khtc_snapshot s WHERE s.tenant_id = p.tenant_id AND s.period_id = p.id
	                 ORDER BY s.seq DESC LIMIT 1) ls ON true
	  JOIN pf_extension.khtc_snapshot_value v ON v.tenant_id = p.tenant_id AND v.snapshot_id = ls.id
	 WHERE p.report_type = 'week' AND p.period_key = ANY($1::text[]) AND v.cumulative IS NOT NULL
	   AND EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = p.tenant_id AND e.period_id = p.id AND e.action = 'closed')`,
		weekKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var week, key string
		var cum, inc, ytd, unc *string
		if err := rows.Scan(&week, &key, &cum, &inc, &ytd, &unc); err != nil {
			return nil, err
		}
		c, err := decOf(cum)
		if err != nil || c == nil {
			return nil, err
		}
		f := domain.Frozen{Cumulative: *c}
		if f.Increment, err = decOf(inc); err != nil {
			return nil, err
		}
		if f.YearToDate, err = decOf(ytd); err != nil {
			return nil, err
		}
		if f.Unconfirmed, err = decOf(unc); err != nil {
			return nil, err
		}
		if out[week] == nil {
			out[week] = map[string]domain.Frozen{}
		}
		out[week][key] = f
	}
	return out, rows.Err()
}
