package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// SupplyRow là sản lượng lúa theo HTX × vụ của một năm.
type SupplyRow struct {
	CooperativeRef  uuid.UUID
	SeasonCode      string
	SeasonYear      int
	AreaHa          decimal.Decimal
	PaddyKg         decimal.Decimal
	HarvestedAreaHa decimal.Decimal
	Seasons         int
}

// SeasonSupply gộp yield_summary (vụ chưa hủy) theo HTX × vụ của năm: thực tế nếu đã
// khai sản lượng, ngược lại dự kiến.
func SeasonSupply(ctx context.Context, tx pgx.Tx, year int) ([]SupplyRow, error) {
	rows, err := tx.Query(ctx, `SELECT cooperative_ref, season_code, season_year, sum(area_ha)::text,
	    coalesce(sum(coalesce(actual_kg, expected_kg, 0)), 0)::text,
	    coalesce(sum(area_ha) FILTER (WHERE actual_kg IS NOT NULL), 0)::text, count(*)
	  FROM pf_coop.yield_summary WHERE status <> 'cancelled' AND season_year = $1
	  GROUP BY cooperative_ref, season_code, season_year ORDER BY cooperative_ref, season_code`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SupplyRow
	for rows.Next() {
		var r SupplyRow
		var area, kg, harvested string
		if err := rows.Scan(&r.CooperativeRef, &r.SeasonCode, &r.SeasonYear, &area, &kg, &harvested, &r.Seasons); err != nil {
			return nil, err
		}
		for _, p := range []struct {
			s string
			d *decimal.Decimal
		}{{area, &r.AreaHa}, {kg, &r.PaddyKg}, {harvested, &r.HarvestedAreaHa}} {
			v, err := decimal.NewFromString(p.s)
			if err != nil {
				return nil, err
			}
			*p.d = v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
