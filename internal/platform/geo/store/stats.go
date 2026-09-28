package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// StatRow là số thửa chưa ngừng dùng và tổng diện tích của một nhóm.
type StatRow struct {
	Key    string
	Plots  int64
	AreaHa decimal.Decimal
}

func scanStats(rows pgx.Rows, err error) ([]StatRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatRow
	for rows.Next() {
		var r StatRow
		var area string
		if err := rows.Scan(&r.Key, &r.Plots, &area); err != nil {
			return nil, err
		}
		d, err := decimal.NewFromString(area)
		if err != nil {
			return nil, err
		}
		r.AreaHa = d
		out = append(out, r)
	}
	return out, rows.Err()
}

// StatsByCommune đếm thửa chưa ngừng dùng theo xã; all = mọi xã.
func StatsByCommune(ctx context.Context, tx pgx.Tx, all bool, communes []string) ([]StatRow, error) {
	return scanStats(tx.Query(ctx, `SELECT commune_code, count(*), coalesce(sum(area_ha), 0)::numeric(14,4)::text
	  FROM pf_geo.plot WHERE status <> 'retired' AND ($1::boolean OR commune_code = ANY($2::text[]))
	  GROUP BY commune_code ORDER BY commune_code`, all, units(communes)))
}

// StatsByCooperative đếm thửa chưa ngừng dùng theo HTX.
func StatsByCooperative(ctx context.Context, tx pgx.Tx, coops []uuid.UUID) ([]StatRow, error) {
	ss := make([]string, 0, len(coops))
	for _, c := range coops {
		ss = append(ss, c.String())
	}
	return scanStats(tx.Query(ctx, `SELECT cooperative_ref::text, count(*), coalesce(sum(area_ha), 0)::numeric(14,4)::text
	  FROM pf_geo.plot WHERE status <> 'retired' AND cooperative_ref = ANY($1::text[]::uuid[])
	  GROUP BY cooperative_ref`, ss))
}

// AdminBoundaries đọc hình hiện hành của các đơn vị; simplifyDeg > 0 thì đơn giản hóa.
func AdminBoundaries(ctx context.Context, tx pgx.Tx, codes []string, simplifyDeg float64) ([]AdminBoundary, error) {
	rows, err := tx.Query(ctx, `SELECT id, unit_code, level, name, source, imported_by, imported_at,
	    (ST_Area(geom::geography) / 10000)::numeric(14,4)::text,
	    ST_AsGeoJSON(CASE WHEN $2::float8 > 0 THEN ST_SimplifyPreserveTopology(geom, $2::float8) ELSE geom END, 7)
	  FROM pf_geo.admin_boundary WHERE unit_code = ANY($1::text[]) AND superseded_at IS NULL ORDER BY unit_code`,
		units(codes), simplifyDeg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminBoundary
	for rows.Next() {
		var b AdminBoundary
		var area string
		if err := rows.Scan(&b.ID, &b.UnitCode, &b.Level, &b.Name, &b.Source, &b.ImportedBy, &b.ImportedAt, &area, &b.GeoJSON); err != nil {
			return nil, err
		}
		if b.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AdminBoundariesByLevel đọc hình hiện hành của mọi đơn vị cùng cấp.
func AdminBoundariesByLevel(ctx context.Context, tx pgx.Tx, level string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT unit_code FROM pf_geo.admin_boundary WHERE level = $1 AND superseded_at IS NULL ORDER BY unit_code`, level)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
