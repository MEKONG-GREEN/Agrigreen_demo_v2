package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// PlotHarvest là một dòng bảng đệm order_plot_harvest_current.
type PlotHarvest struct {
	OrderID   uuid.UUID
	PlotRef   uuid.UUID
	Check     string     // ok | unknown | unavailable
	Date      *time.Time // ngày gặt dự kiến (gốc hạn FM-02)
	OpenDate  *time.Time // ngày mở cửa sổ gặt (mốc FM-01); nil = dùng Date
	SeasonRef *uuid.UUID
	FetchedAt time.Time
}

// PutPlotHarvest ghi bảng đệm ngày gặt. Kết quả "unavailable" (nền tảng không gọi được)
// không đè một ngày gặt đã biết.
func PutPlotHarvest(ctx context.Context, tx pgx.Tx, tid uuid.UUID, h PlotHarvest) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.order_plot_harvest_current AS c
	    (tenant_id, order_id, plot_ref, harvest_check, harvest_date, harvest_open_date, season_ref, fetched_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	  ON CONFLICT (tenant_id, order_id, plot_ref) DO UPDATE SET
	    harvest_check = EXCLUDED.harvest_check, harvest_date = EXCLUDED.harvest_date, season_ref = EXCLUDED.season_ref,
	    harvest_open_date = EXCLUDED.harvest_open_date, fetched_at = EXCLUDED.fetched_at, updated_at = now()
	  WHERE NOT (EXCLUDED.harvest_check = 'unavailable' AND c.harvest_check = 'ok')`,
		tid, h.OrderID, h.PlotRef, h.Check, h.Date, h.OpenDate, h.SeasonRef, h.FetchedAt.UTC())
	return err
}

const plotHarvestCols = `order_id, plot_ref, harvest_check, harvest_date, harvest_open_date, season_ref, fetched_at`

func scanPlotHarvest(row pgx.Row) (PlotHarvest, error) {
	var h PlotHarvest
	err := row.Scan(&h.OrderID, &h.PlotRef, &h.Check, &h.Date, &h.OpenDate, &h.SeasonRef, &h.FetchedAt)
	return h, err
}

// GetPlotHarvest đọc ngày gặt đệm của (lệnh gom, thửa).
func GetPlotHarvest(ctx context.Context, tx pgx.Tx, orderID, plot uuid.UUID) (*PlotHarvest, error) {
	h, err := scanPlotHarvest(tx.QueryRow(ctx, "SELECT "+plotHarvestCols+` FROM mkg_fieldops.order_plot_harvest_current
	    WHERE order_id = $1 AND plot_ref = $2`, orderID, plot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// PlotHarvests đọc ngày gặt đệm của mọi thửa của một lệnh gom.
func PlotHarvests(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (map[uuid.UUID]PlotHarvest, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotHarvestCols+" FROM mkg_fieldops.order_plot_harvest_current WHERE order_id = $1", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]PlotHarvest{}
	for rows.Next() {
		h, err := scanPlotHarvest(rows)
		if err != nil {
			return nil, err
		}
		out[h.PlotRef] = h
	}
	return out, rows.Err()
}

// HarvestCheck là một dòng sổ roll_count_harvest (kết quả FM-01 của bản ghi cuộn gốc).
type HarvestCheck struct {
	RollCountID uuid.UUID
	OrderID     uuid.UUID
	PlotRef     uuid.UUID
	BaleDate    time.Time
	Check       string
	HarvestDate *time.Time
	OpenDate    *time.Time
	SeasonRef   *uuid.UUID
	Source      string // partner | cache | none
	RuleVersion string
}

// InsertHarvestCheck ghi kết quả kiểm ngày gặt của bản ghi cuộn r (cùng giao dịch).
func InsertHarvestCheck(ctx context.Context, tx pgx.Tx, r RollCount, h *HarvestCheck) error {
	h.RollCountID, h.OrderID, h.PlotRef, h.RuleVersion = r.ID, r.OrderID, r.PlotRef, domain.RuleHarvestCheck
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count_harvest
	    (id, tenant_id, stream_key, roll_count_id, order_id, plot_ref, bale_date, harvest_check, harvest_date, harvest_open_date, season_ref,
	     harvest_source, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		ids.New(), r.TenantID, "harvest:"+r.ID.String(), r.ID, r.OrderID, r.PlotRef, h.BaleDate, h.Check, h.HarvestDate, h.OpenDate, h.SeasonRef,
		h.Source, r.EventTime, r.ActorRef, r.DeviceID, r.IdemKey+":harvest", h.RuleVersion)
	return err
}

// HarvestChecksOf đọc kết quả kiểm ngày gặt của mọi bản ghi cuộn của một lệnh gom.
func HarvestChecksOf(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (map[uuid.UUID]HarvestCheck, error) {
	rows, err := tx.Query(ctx, `SELECT roll_count_id, order_id, plot_ref, bale_date, harvest_check, harvest_date, harvest_open_date, season_ref,
	    harvest_source, rule_version FROM mkg_fieldops.roll_count_harvest WHERE order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]HarvestCheck{}
	for rows.Next() {
		var h HarvestCheck
		if err := rows.Scan(&h.RollCountID, &h.OrderID, &h.PlotRef, &h.BaleDate, &h.Check, &h.HarvestDate, &h.OpenDate, &h.SeasonRef,
			&h.Source, &h.RuleVersion); err != nil {
			return nil, err
		}
		out[h.RollCountID] = h
	}
	return out, rows.Err()
}

// BalingRow là một (lệnh gom đang mở, thửa) để tính hạn cuộn FM-02.
type BalingRow struct {
	OrderID      uuid.UUID
	PlotRef      uuid.UUID
	HTXRef       uuid.UUID
	CrewRef      *uuid.UUID
	OrderStatus  string
	HarvestCheck *string
	HarvestDate  *time.Time
	SeasonRef    *uuid.UUID
	HarvestOpen  *time.Time
	// BaledAt: event_time của bản ghi cuộn GỐC đầu tiên còn hiệu lực có số (nil = chưa cuộn).
	BaledAt *time.Time
}

// BalingRows trả (lệnh gom approved/assigned, thửa) sau con trỏ (order_id, plot_ref), theo
// thứ tự đó; orderIDs khác nil → chỉ các lệnh đó (mọi trạng thái trừ planned).
func BalingRows(ctx context.Context, tx pgx.Tx, orderIDs []uuid.UUID, after [2]uuid.UUID, limit int) ([]BalingRow, error) {
	var only []string
	if orderIDs != nil {
		only = uuidStrings(orderIDs)
	}
	rows, err := tx.Query(ctx, `
	  SELECT o.order_id, p.plot_ref, o.htx_ref, o.crew_ref, o.status, h.harvest_check, h.harvest_date, h.season_ref, h.harvest_open_date,
	         CASE WHEN t.plot_ref IS NOT NULL AND (t.rolls > 0 OR t.loose_kg IS NOT NULL) THEN (
	           SELECT min(r.event_time) FROM mkg_fieldops.roll_count r
	           WHERE r.order_id = o.order_id AND r.plot_ref = p.plot_ref AND r.stage = 'bale' AND r.supersedes_id IS NULL) END
	  FROM mkg_fieldops.collection_order_current o
	  CROSS JOIN LATERAL unnest(o.plot_refs) AS p(plot_ref)
	  LEFT JOIN mkg_fieldops.order_plot_harvest_current h
	         ON h.tenant_id = o.tenant_id AND h.order_id = o.order_id AND h.plot_ref = p.plot_ref
	  LEFT JOIN mkg_fieldops.roll_total_current t
	         ON t.tenant_id = o.tenant_id AND t.order_id = o.order_id AND t.plot_ref = p.plot_ref AND t.stage = 'bale'
	  WHERE (($1::uuid[] IS NULL AND o.status IN ('approved', 'assigned'))
	         OR ($1::uuid[] IS NOT NULL AND o.order_id = ANY($1::uuid[]) AND o.status <> 'planned'))
	    AND (o.order_id, p.plot_ref) > ($2::uuid, $3::uuid)
	  ORDER BY o.order_id, p.plot_ref
	  LIMIT $4`, only, after[0], after[1], limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalingRow
	for rows.Next() {
		var b BalingRow
		if err := rows.Scan(&b.OrderID, &b.PlotRef, &b.HTXRef, &b.CrewRef, &b.OrderStatus, &b.HarvestCheck, &b.HarvestDate, &b.SeasonRef,
			&b.HarvestOpen, &b.BaledAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// OpenOrderPlots trả (lệnh gom approved/assigned, thửa) để làm mới bảng đệm ngày gặt.
func OpenOrderPlots(ctx context.Context, tx pgx.Tx) ([]Order, error) {
	rows, err := tx.Query(ctx, "SELECT "+orderCols+` FROM mkg_fieldops.collection_order_current
	    WHERE status IN ('approved', 'assigned') ORDER BY order_id`)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}
