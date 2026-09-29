package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// HouseholdsInCommunes trả nông hộ (không trùng) có thửa chưa ngừng dùng trong các
// xã; all = mọi xã của tenant.
func HouseholdsInCommunes(ctx context.Context, tx pgx.Tx, all bool, communes []string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT household_ref FROM pf_geo.plot
	  WHERE status <> 'retired' AND household_ref IS NOT NULL AND ($1::boolean OR commune_code = ANY($2::text[]))
	  ORDER BY household_ref`, all, units(communes))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// CoopCentroid là tâm ranh giới HTX đang dùng.
type CoopCentroid struct {
	CooperativeRef uuid.UUID
	Lon, Lat       decimal.Decimal
}

// CoopCentroids đọc tâm (ST_PointOnSurface — luôn nằm trong vùng) của ranh giới HTX
// đang dùng; all = mọi HTX.
func CoopCentroids(ctx context.Context, tx pgx.Tx, all bool, coops []uuid.UUID) ([]CoopCentroid, error) {
	rows, err := tx.Query(ctx, `SELECT cooperative_ref, ST_X(c)::numeric(10,7)::text, ST_Y(c)::numeric(10,7)::text
	  FROM (SELECT cooperative_ref, ST_PointOnSurface(geom) AS c FROM pf_geo.cooperative_area
	        WHERE status = 'active' AND ($1::boolean OR cooperative_ref = ANY($2::text[]::uuid[]))) s
	  ORDER BY cooperative_ref`, all, refStrs(coops))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopCentroid
	for rows.Next() {
		var c CoopCentroid
		var lon, lat string
		if err := rows.Scan(&c.CooperativeRef, &lon, &lat); err != nil {
			return nil, err
		}
		var err error
		if c.Lon, err = decimal.NewFromString(lon); err != nil {
			return nil, err
		}
		if c.Lat, err = decimal.NewFromString(lat); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// refStrs chuyển danh sách UUID sang []string (tham số $n::text[]::uuid[]).
func refStrs(r []uuid.UUID) []string {
	out := make([]string, 0, len(r))
	for _, u := range r {
		out = append(out, u.String())
	}
	return out
}
