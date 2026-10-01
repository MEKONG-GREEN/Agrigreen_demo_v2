package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VolumeRow là một bảng kê thu gom hiện hành (revision cao nhất) cho sản phẩm tổng hợp
// collection_volume (G5): thửa (chỉ để tra xã/nông hộ/diện tích ở pf/geo), ngày gom, số cuộn, vụ.
type VolumeRow struct {
	PlotRef     uuid.UUID
	CollectedOn time.Time
	Rolls       int64
	ReceivedAt  time.Time
	SeasonCode  *string // mã vụ của season_current (nil = bảng kê chưa gắn vụ / vụ không có)
	SeasonYear  *int
}

// VolumeRows trả mọi bảng kê hiện hành đã nhận tới asOf (zero = không chặn) của tenant trong giao dịch.
func VolumeRows(ctx context.Context, tx pgx.Tx, asOf time.Time) ([]VolumeRow, error) {
	var at *time.Time
	if !asOf.IsZero() {
		t := asOf.UTC()
		at = &t
	}
	rows, err := tx.Query(ctx, `SELECT s.plot_ref, s.collected_on, s.rolls, s.received_at, c.season_code, c.season_year
	  FROM pf_cropcycle.collection_statement_view s
	  LEFT JOIN pf_cropcycle.season_current c ON c.tenant_id = s.tenant_id AND c.season_ref = s.season_ref
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
		if err := rows.Scan(&r.PlotRef, &r.CollectedOn, &rolls, &r.ReceivedAt, &r.SeasonCode, &r.SeasonYear); err != nil {
			return nil, err
		}
		r.Rolls = int64(rolls)
		out = append(out, r)
	}
	return out, rows.Err()
}
