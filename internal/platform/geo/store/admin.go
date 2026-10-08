package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// Ranh giới hành chính (GIS FN-07)
// ---------------------------------------------------------------------------

// AdminBoundary là hình ranh giới hiện hành của một đơn vị hành chính.
type AdminBoundary struct {
	ID         uuid.UUID
	UnitCode   string
	Level      string
	Name       string
	Source     string
	ImportedBy uuid.UUID
	ImportedAt time.Time
	AreaHa     decimal.Decimal
	GeoJSON    string
}

// ReplaceAdminBoundary đóng hình hiện hành (nếu có) rồi ghi hình mới cho unit_code.
func ReplaceAdminBoundary(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, b AdminBoundary, geojson string) (replaced bool, err error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_geo.admin_boundary SET superseded_at = now(), superseded_by = $2::uuid
	  WHERE unit_code = $1::text AND superseded_at IS NULL`, b.UnitCode, b.ID)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_geo.admin_boundary (tenant_id, id, unit_code, level, name, geom, source, imported_by)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, `+geomSQL("$6")+`, $7::text, $8::uuid)`,
		tenantID, b.ID, b.UnitCode, b.Level, b.Name, geojson, b.Source, b.ImportedBy)
	return tag.RowsAffected() > 0, err
}

// AdminBoundaryByCode đọc hình hiện hành; simplifyDeg > 0 thì đơn giản hóa.
func AdminBoundaryByCode(ctx context.Context, tx pgx.Tx, code string, simplifyDeg float64) (*AdminBoundary, error) {
	var b AdminBoundary
	var area string
	err := tx.QueryRow(ctx, `SELECT id, unit_code, level, name, source, imported_by, imported_at,
	    (ST_Area(geom::geography) / 10000)::numeric(14,4)::text,
	    ST_AsGeoJSON(CASE WHEN $2::float8 > 0 THEN ST_SimplifyPreserveTopology(geom, $2::float8) ELSE geom END, 7)
	  FROM pf_geo.admin_boundary WHERE unit_code = $1::text AND superseded_at IS NULL`, code, simplifyDeg).
		Scan(&b.ID, &b.UnitCode, &b.Level, &b.Name, &b.Source, &b.ImportedBy, &b.ImportedAt, &area, &b.GeoJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.AreaHa, err = decimal.NewFromString(area)
	return &b, err
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

// AdminBoundariesInBBox đọc hình hiện hành của các đơn vị cấp level giao khung nhìn (chỉ mục
// gist admin_boundary_geom), tối đa limit, sắp theo mã; simplifyDeg > 0 thì đơn giản hóa.
func AdminBoundariesInBBox(ctx context.Context, tx pgx.Tx, b BBox, level string, simplifyDeg float64, limit int) ([]AdminBoundary, error) {
	rows, err := tx.Query(ctx, `SELECT id, unit_code, level, name, source, imported_by, imported_at,
	    (ST_Area(geom::geography) / 10000)::numeric(14,4)::text,
	    ST_AsGeoJSON(CASE WHEN $6::float8 > 0 THEN ST_SimplifyPreserveTopology(geom, $6::float8) ELSE geom END, 7)
	  FROM pf_geo.admin_boundary
	  WHERE superseded_at IS NULL AND level = $5 AND geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)
	  ORDER BY unit_code LIMIT $7`, b.MinLon, b.MinLat, b.MaxLon, b.MaxLat, level, simplifyDeg, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminBoundary
	for rows.Next() {
		var ab AdminBoundary
		var area string
		if err := rows.Scan(&ab.ID, &ab.UnitCode, &ab.Level, &ab.Name, &ab.Source, &ab.ImportedBy, &ab.ImportedAt, &area, &ab.GeoJSON); err != nil {
			return nil, err
		}
		if ab.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		out = append(out, ab)
	}
	return out, rows.Err()
}
