package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
)

// Bảng chiếu fieldops dựng lại được từ sổ của chính module (R-22, G4 — docs/dev/g4-conventions.md §8).
//
// KHÔNG dựng lại (bảng đệm của nguồn NGOÀI module, không có sổ riêng): weighed_loading_current (khối lượng cân
// đọc qua measurement/port) và order_plot_harvest_current (ngày gặt đọc qua bề mặt partner). Hai bảng này giữ
// nguyên khi dựng lại; job/consumer làm mới chúng như cũ.
const (
	ProjOrders      = "collection_order_current"
	ProjTotals      = "roll_total_current"
	ProjRecon       = "buyside_reconciliation_current"
	ProjCrews       = "crew_current"
	ProjAssignments = "crew_assignment_current"
	ProjParams      = "param_current"
)

// RebuildProjections xóa và dựng lại sáu bảng chiếu của tenant (giao dịch tenant do app/ mở) thẳng từ sổ:
// collection_order_event (+ valve_export không cần: cờ published suy từ trạng thái như ApplyOrderEvent),
// roll_count, valve_export htx_collection_statement + htx_confirmation, crew_event, crew_assignment,
// param_version. sync_seq của lệnh gom GIỮ giá trị cũ khi lệnh đã có (con trỏ pull của app không lùi); lệnh
// không còn ở bảng chiếu nhận số mới (app kéo lại). Trả số dòng mỗi bảng.
func RebuildProjections(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	out := map[string]int64{}
	n, err := rebuildOrders(ctx, tx)
	if err != nil {
		return nil, err
	}
	out[ProjOrders] = n
	steps := []struct {
		table string
		del   string
		ins   string
	}{
		{ProjTotals, `DELETE FROM mkg_fieldops.roll_total_current`,
			`INSERT INTO mkg_fieldops.roll_total_current
			    (tenant_id, order_id, plot_ref, stage, htx_ref, rolls, loose_kg, roll_count_id, discrepancy_reason, event_time, last_seq)
			  SELECT DISTINCT ON (order_id, plot_ref, stage) tenant_id, order_id, plot_ref, stage, htx_ref, rolls, loose_kg, id,
			         discrepancy_reason, event_time, seq
			    FROM mkg_fieldops.roll_count WHERE order_id IS NOT NULL AND stage IS NOT NULL
			   ORDER BY order_id, plot_ref, stage, seq DESC`},
		// Đối soát chiều mua: bảng kê mới nhất (van) của (lệnh gom, thửa) + xác nhận mới nhất của HTX (như
		// statement() / Confirm / htxfeed giữ số HTX qua các bản sửa của MKG).
		{ProjRecon, `DELETE FROM mkg_fieldops.buyside_reconciliation_current`,
			`INSERT INTO mkg_fieldops.buyside_reconciliation_current
			    (tenant_id, order_id, plot_ref, htx_ref, statement_ref, statement_revision, mkg_rolls, mkg_roll_count_id, htx_rolls,
			     outcome, confirmation_id, confirmed_revision, status)
			  SELECT s.tenant_id, s.order_id, s.plot_ref, s.htx_ref, s.statement_ref, s.revision, s.rolls, t.roll_count_id,
			         c.htx_rolls, c.outcome, c.id, c.statement_revision,
			         CASE WHEN c.id IS NULL THEN 'pending' WHEN c.htx_rolls = s.rolls THEN 'match' ELSE 'mismatch' END
			    FROM (SELECT DISTINCT ON (order_id, (payload->>'plot_ref')::uuid) tenant_id, order_id,
			                 (payload->>'plot_ref')::uuid AS plot_ref, (payload->>'htx_ref')::uuid AS htx_ref,
			                 (payload->>'statement_ref')::uuid AS statement_ref, (payload->>'revision')::int AS revision,
			                 (payload->>'rolls')::int AS rolls
			            FROM mkg_fieldops.valve_export WHERE contract = 'htx_collection_statement'
			           ORDER BY order_id, (payload->>'plot_ref')::uuid, (payload->>'revision')::int DESC, seq DESC) s
			    JOIN mkg_fieldops.roll_total_current t
			      ON t.order_id = s.order_id AND t.plot_ref = s.plot_ref AND t.stage = 'boat_load'
			    LEFT JOIN LATERAL (SELECT h.id, h.htx_rolls, h.outcome, h.statement_revision FROM mkg_fieldops.htx_confirmation h
			                        WHERE h.order_id = s.order_id AND h.plot_ref = s.plot_ref ORDER BY h.seq DESC LIMIT 1) c ON true`},
		{ProjCrews, `DELETE FROM mkg_fieldops.crew_current`,
			`INSERT INTO mkg_fieldops.crew_current
			    (tenant_id, crew_id, code, name, member_refs, capacity_unit, capacity_per_day, service_communes, status, version, last_seq, created_at)
			  SELECT DISTINCT ON (e.crew_id) e.tenant_id, e.crew_id, e.code, e.name, e.member_refs, e.capacity_unit, e.capacity_per_day,
			         e.service_communes, e.status, e.version, e.seq,
			         (SELECT f.event_time FROM mkg_fieldops.crew_event f WHERE f.crew_id = e.crew_id ORDER BY f.seq LIMIT 1)
			    FROM mkg_fieldops.crew_event e ORDER BY e.crew_id, e.seq DESC`},
		{ProjAssignments, `DELETE FROM mkg_fieldops.crew_assignment_current`,
			`INSERT INTO mkg_fieldops.crew_assignment_current
			    (tenant_id, order_id, assignment_id, crew_id, crew_version, capacity_unit, planned_from, planned_to, expected_qty, daily_load,
			     over_capacity, override_reason, mode)
			  SELECT DISTINCT ON (order_id) tenant_id, order_id, id, crew_id, crew_version, capacity_unit, planned_from, planned_to,
			         expected_qty, daily_load, over_capacity, override_reason, mode
			    FROM mkg_fieldops.crew_assignment ORDER BY order_id, seq DESC`},
		{ProjParams, `DELETE FROM mkg_fieldops.param_current`,
			`INSERT INTO mkg_fieldops.param_current
			    (tenant_id, param_key, version_id, version_seq, value, effective_from, provisional, status, proposed_by, proposal_reason,
			     decided_by, decided_at, decision_reason)
			  SELECT p.tenant_id, p.param_key, p.version_id, p.version_seq, p.value, p.effective_from, p.provisional,
			         coalesce(d.kind, 'pending'), p.proposer_ref, p.reason, d.actor_ref, d.event_time, d.reason
			    FROM mkg_fieldops.param_version p
			    LEFT JOIN LATERAL (SELECT x.kind, x.actor_ref, x.event_time, x.reason FROM mkg_fieldops.param_version x
			                        WHERE x.version_id = p.version_id AND x.kind <> 'proposed' ORDER BY x.seq DESC LIMIT 1) d ON true
			   WHERE p.kind = 'proposed'`},
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.del); err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, s.ins)
		if err != nil {
			return nil, err
		}
		out[s.table] = tag.RowsAffected()
	}
	return out, nil
}

// rebuildOrders gấp sổ lệnh gom theo seq đúng như ApplyOrderEvent (duyệt → người duyệt; về planned → xóa người
// duyệt; published = đã từng công bố theo domain.ScheduleStatus; created_at = event_time sự kiện đầu).
func rebuildOrders(ctx context.Context, tx pgx.Tx) (int64, error) {
	old := map[uuid.UUID]int64{}
	rows, err := tx.Query(ctx, "SELECT order_id, sync_seq FROM mkg_fieldops.collection_order_current")
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id uuid.UUID
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			rows.Close()
			return 0, err
		}
		old[id] = seq
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows, err = tx.Query(ctx, "SELECT "+orderEventCols+" FROM mkg_fieldops.collection_order_event ORDER BY order_id, seq")
	if err != nil {
		return 0, err
	}
	var folded []Order
	var cur *Order
	for rows.Next() {
		e, err := scanOrderEvent(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		if cur == nil || cur.OrderID != e.OrderID {
			folded = append(folded, Order{TenantID: e.TenantID, OrderID: e.OrderID, CreatedAt: e.EventTime.UTC()})
			cur = &folded[len(folded)-1]
		}
		_, publish := domain.ScheduleStatus(e.Kind, cur.Published)
		cur.Status, cur.Revision, cur.HTXRef, cur.CommuneCode = e.Kind, e.Revision, e.HTXRef, e.CommuneCode
		cur.WindowFrom, cur.WindowTo, cur.PlotRefs, cur.CrewRef, cur.Reason, cur.CreatorRef = e.WindowFrom, e.WindowTo, e.PlotRefs, e.CrewRef, e.Reason, e.CreatorRef
		if e.Kind == domain.OrderApproved {
			a, t := e.ActorRef, e.EventTime.UTC()
			cur.ApproverRef, cur.ApprovedAt = &a, &t
		}
		if e.Kind == domain.OrderPlanned {
			cur.ApproverRef, cur.ApprovedAt = nil, nil
		}
		cur.Published = cur.Published || publish
		cur.LastSeq = e.Seq
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_fieldops.collection_order_current"); err != nil {
		return 0, err
	}
	for _, o := range folded {
		var sync *int64
		if s, ok := old[o.OrderID]; ok {
			sync = &s
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.collection_order_current
		    (tenant_id, order_id, status, revision, htx_ref, commune_code, window_from, window_to, plot_refs, crew_ref, reason,
		     creator_ref, approver_ref, approved_at, published, last_seq, sync_seq, created_at)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10, $11, $12, $13, $14, $15, $16,
		          coalesce($17::bigint, nextval('mkg_fieldops.sync_seq')), $18)`,
			o.TenantID, o.OrderID, o.Status, o.Revision, o.HTXRef, o.CommuneCode, o.WindowFrom, o.WindowTo, uuidStrings(o.PlotRefs),
			o.CrewRef, o.Reason, o.CreatorRef, o.ApproverRef, o.ApprovedAt, o.Published, o.LastSeq, sync, o.CreatedAt.UTC()); err != nil {
			return 0, err
		}
	}
	return int64(len(folded)), nil
}
