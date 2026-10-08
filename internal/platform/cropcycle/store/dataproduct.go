package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VolumeRow là một bảng kê thu gom hiện hành (revision cao nhất) cho sản phẩm tổng hợp
// collection_volume (G5): thửa (chỉ để tra xã/nông hộ/diện tích ở pf/cooperative), ngày gom, số cuộn, vụ.
// W3.3: vụ ở pf_coop (pf/cooperative) — store chỉ trả season_ref, app tra mã vụ qua cooperativeport.Seasons.Codes
// (không JOIN chéo schema, vai r_cropcycle không có quyền trên pf_coop).
type VolumeRow struct {
	PlotRef     uuid.UUID
	CollectedOn time.Time
	Rolls       int64
	ReceivedAt  time.Time
	SeasonRef   *uuid.UUID // nil = bảng kê chưa gắn vụ
}

// VolumeRows trả mọi bảng kê hiện hành đã nhận tới asOf (zero = không chặn) của tenant trong giao dịch.
func VolumeRows(ctx context.Context, tx pgx.Tx, asOf time.Time) ([]VolumeRow, error) {
	var at *time.Time
	if !asOf.IsZero() {
		t := asOf.UTC()
		at = &t
	}
	rows, err := tx.Query(ctx, `SELECT s.plot_ref, s.collected_on, s.rolls, s.received_at, s.season_ref
	  FROM pf_cropcycle.collection_statement_view s
	  WHERE ($1::timestamptz IS NULL OR s.received_at <= $1::timestamptz)
	  ORDER BY s.statement_ref`, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VolumeRow
	for rows.Next() {
		var r VolumeRow
		var rolls int32
		if err := rows.Scan(&r.PlotRef, &r.CollectedOn, &rolls, &r.ReceivedAt, &r.SeasonRef); err != nil {
			return nil, err
		}
		r.Rolls = int64(rolls)
		out = append(out, r)
	}
	return out, rows.Err()
}
