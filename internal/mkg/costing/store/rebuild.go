package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
)

func parseDec(s string) (decimal.Decimal, error) { return decimal.NewFromString(s) }

// Projections là các bảng chiếu của costing (dựng lại từ sổ, R-22).
var Projections = []string{"lot_cost_current", "collection_current", "conversion_current", "allocation_current", "period_current",
	"settlement_current"}

// RebuildAll xóa và dựng lại MỌI bảng chiếu của tenant từ sổ, bằng đúng hàm chiếu dùng khi ghi
// (applyPeriodEvent, applySettlement, AggregateCollection) hoặc phép gộp tương đương (Σ delta).
func RebuildAll(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (map[string]int64, error) {
	out := map[string]int64{}
	for _, t := range Projections {
		if _, err := tx.Exec(ctx, "DELETE FROM mkg_costing."+t); err != nil {
			return nil, err
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_costing.lot_cost_current (tenant_id, lot_ref, category, amount, mass_kg, entries)
	  SELECT tenant_id, lot_ref, category, sum(delta_amount), sum(delta_mass_kg), count(*) FROM mkg_costing.cost_entry
	  GROUP BY tenant_id, lot_ref, category`)
	if err != nil {
		return nil, err
	}
	out["lot_cost_current"] = tag.RowsAffected()

	orders, err := func() ([]uuid.UUID, error) {
		rows, err := tx.Query(ctx, "SELECT DISTINCT collection_order_ref FROM mkg_costing.input_event ORDER BY 1")
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}()
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		ins, err := EffectiveInputsForOrder(ctx, tx, o)
		if err != nil {
			return nil, err
		}
		if c := AggregateCollection(o, ins); c != nil {
			if err := UpsertCollection(ctx, tx, tid, *c); err != nil {
				return nil, err
			}
			out["collection_current"]++
		}
	}

	if tag, err = tx.Exec(ctx, `INSERT INTO mkg_costing.conversion_current
	  (tenant_id, collection_order_ref, result_id, rolls, weighed_kg, yield_kg_per_roll, converted_kg, inputs_sha256, rule_version)
	  SELECT DISTINCT ON (collection_order_ref) tenant_id, collection_order_ref, id, rolls, weighed_kg, yield_kg_per_roll, converted_kg,
	         inputs_sha256, rule_version
	  FROM mkg_costing.conversion_result ORDER BY collection_order_ref, recorded_at DESC, seq DESC`); err != nil {
		return nil, err
	}
	out["conversion_current"] = tag.RowsAffected()

	if tag, err = tx.Exec(ctx, `INSERT INTO mkg_costing.allocation_current
	  (tenant_id, allocation_id, kind, source_ref, period_ref, total_amount, total_kg, result_sha256, rule_version, superseded_by)
	  SELECT a.tenant_id, a.id, a.kind, a.source_ref, a.period_ref, a.total_amount, a.total_kg, a.result_sha256, a.rule_version,
	         (SELECT s.id FROM mkg_costing.allocation_result s WHERE s.tenant_id = a.tenant_id AND s.supersedes_id = a.id
	           ORDER BY s.recorded_at, s.seq LIMIT 1)
	  FROM mkg_costing.allocation_result a`); err != nil {
		return nil, err
	}
	out["allocation_current"] = tag.RowsAffected()

	pes, err := func() ([]PeriodEvent, error) {
		rows, err := tx.Query(ctx, `SELECT id, period_id, period_code, action, from_at, to_at, close_run_id, journal_ref, external_ref,
		  event_time, actor_ref, idem_key, seq, recorded_at FROM mkg_costing.period_event ORDER BY recorded_at, stream_key, seq`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PeriodEvent, error) {
			var e PeriodEvent
			err := r.Scan(&e.ID, &e.PeriodID, &e.Code, &e.Action, &e.From, &e.To, &e.CloseRunID, &e.JournalRef, &e.ExternalRef, &e.EventTime,
				&e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
			return e, err
		})
	}()
	if err != nil {
		return nil, err
	}
	for _, e := range pes {
		if err := applyPeriodEvent(ctx, tx, tid, e); err != nil {
			return nil, err
		}
		if e.Action == "opened" {
			out["period_current"]++
		}
	}

	ses, err := func() ([]Settlement, error) {
		rows, err := tx.Query(ctx, `SELECT settlement_id, kind, status, collection_order_ref, htx_ref, harvest_date::text, contract_ref,
		  contract_version, basis, price_source, unit_price::text, rolls, mass_kg::text, gross_amount::text, deductions::text, amount::text,
		  blocked_reason, due_on::text, period_ref, prepared_by, approved_by,
		  CASE WHEN action = 'approved' THEN event_time END, supersedes_id, rule_version, seq
		  FROM mkg_costing.settlement_event ORDER BY recorded_at, stream_key, seq`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Settlement, error) { return scanSettlement(r) })
	}()
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, s := range ses {
		if err := applySettlement(ctx, tx, tid, s); err != nil {
			return nil, err
		}
		if !seen[s.ID] {
			seen[s.ID] = true
			out["settlement_current"]++
		}
	}
	return out, nil
}

// ProjectionDump đọc toàn bộ bảng chiếu của tenant dạng văn bản tất định (bỏ updated_at) — để so
// dựng lại với bảng chiếu hiện hành (kiểm thử golden, diễn tập R-22).
func ProjectionDump(ctx context.Context, tx pgx.Tx) (map[string][]string, error) {
	q := map[string]string{
		"lot_cost_current":   `SELECT concat_ws('|', lot_ref, category, amount, mass_kg, entries) FROM mkg_costing.lot_cost_current ORDER BY 1`,
		"collection_current": `SELECT concat_ws('|', collection_order_ref, htx_ref, rolls, weighed_kg, weigh_point, weighed_lots, last_event_time) FROM mkg_costing.collection_current ORDER BY 1`,
		"conversion_current": `SELECT concat_ws('|', collection_order_ref, result_id, rolls, weighed_kg, yield_kg_per_roll, converted_kg, inputs_sha256, rule_version) FROM mkg_costing.conversion_current ORDER BY 1`,
		"allocation_current": `SELECT concat_ws('|', allocation_id, kind, source_ref, period_ref, total_amount, total_kg, result_sha256, rule_version, superseded_by) FROM mkg_costing.allocation_current ORDER BY 1`,
		"period_current":     `SELECT concat_ws('|', period_id, code, from_at, to_at, status, opened_at, close_run_id, closed_by, closed_at, journal_ref, posted_at, last_seq) FROM mkg_costing.period_current ORDER BY 1`,
		"settlement_current": `SELECT concat_ws('|', settlement_id, kind, status, collection_order_ref, htx_ref, harvest_date, contract_ref, contract_version, basis, price_source, unit_price, rolls, mass_kg, gross_amount, deductions, amount, blocked_reason, due_on, period_ref, prepared_by, approved_by, approved_at, supersedes_id, rule_version, last_seq) FROM mkg_costing.settlement_current ORDER BY 1`,
	}
	out := map[string][]string{}
	for t, sql := range q {
		rows, err := tx.Query(ctx, sql)
		if err != nil {
			return nil, err
		}
		xs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		out[t] = xs
	}
	return out, nil
}

// ClosedPeriodRow gom kỳ + kết quả đóng kỳ cho port.ClosedPeriods.
type ClosedPeriodRow struct {
	Period  Period
	Summary *Summary
}

// Status là tiện ích cho app.
func (p Period) Closed() bool { return p.Status == port.PeriodClosed || p.Status == port.PeriodPosted }

// Ended cho biết kỳ đã hết (now ≥ to).
func (p Period) Ended(now time.Time) bool { return !now.Before(p.To) }
