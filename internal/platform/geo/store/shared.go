// Package store: SQL của pf/geo (schema pf_geo: ranh giới hành chính, kho/Hub, sức chứa, đường bộ, log GPS),
// chạy dưới vai r_geo (db.ForModule, R-05) và luôn trong giao dịch có ngữ cảnh tenant (tenant.WithTenantTx) —
// RLS áp dụng. Thửa và ranh giới HTX ở pf/cooperative (schema pf_coop, W3.3).
//
// Mọi phép hình học chính xác (ST_IsValid, diện tích trắc địa ST_Area(geography))
// chạy trong PostGIS. Hình đi vào dưới dạng GeoJSON MultiPolygon do
// domain dựng; tham số luôn ép kiểu tường minh vì pool dùng chế độ thực thi của
// PgBouncer (QueryExecModeExec).
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// geomSQL dựng hình từ tham số GeoJSON thứ n.
func geomSQL(n string) string {
	return "ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(" + n + "::text), 4326))"
}

// geomOr dựng hình từ tham số GeoJSON thứ n; rỗng → giữ biểu thức cur.
func geomOr(n, cur string) string {
	return "coalesce(ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(nullif(" + n + "::text, '')), 4326)), (" + cur + "))"
}

func decOf(s string) (decimal.Decimal, error) { return decimal.NewFromString(s) }

// refStrs chuyển danh sách UUID sang []string (tham số $n::text[]::uuid[]).
func refStrs(r []uuid.UUID) []string {
	out := make([]string, 0, len(r))
	for _, u := range r {
		out = append(out, u.String())
	}
	return out
}

func nilIfZero(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// Phân tích hình
// ---------------------------------------------------------------------------

// Analysis là kết quả kiểm hình trong PostGIS.
type Analysis struct {
	Valid  bool
	Reason string          // lý do không hợp lệ (ST_IsValidReason)
	AreaHa decimal.Decimal // trắc địa trên ellipsoid WGS84
}

// Analyze kiểm tính hợp lệ (tự cắt, vòng sai...) và tính diện tích trắc địa.
func Analyze(ctx context.Context, tx pgx.Tx, geojson string) (Analysis, error) {
	var a Analysis
	var area string
	err := tx.QueryRow(ctx, `SELECT ST_IsValid(g), ST_IsValidReason(g), (ST_Area(g::geography) / 10000)::numeric(14,4)::text
	  FROM (SELECT `+geomSQL("$1")+` AS g) s`, geojson).Scan(&a.Valid, &a.Reason, &area)
	if err != nil {
		return a, err
	}
	a.AreaHa, err = decimal.NewFromString(area)
	return a, err
}

// OutsideCommunePct trả phần trăm diện tích hình nằm ngoài ranh giới xã hiện
// hành; nil nếu chưa nhập ranh giới xã đó.
func OutsideCommunePct(ctx context.Context, tx pgx.Tx, geojson, commune string) (*decimal.Decimal, error) {
	var s *string
	err := tx.QueryRow(ctx, `WITH n AS (SELECT `+geomSQL("$1")+` AS g)
	  SELECT CASE WHEN ST_Area(n.g::geography) = 0 THEN 0
	              ELSE ST_Area(ST_Difference(n.g, b.geom)::geography) * 100 / ST_Area(n.g::geography) END::numeric(14,4)::text
	  FROM pf_geo.admin_boundary b, n
	  WHERE b.unit_code = $2::text AND b.superseded_at IS NULL`, geojson, commune).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseDec(s)
}

// Filter là điều kiện đọc theo phạm vi.
type Filter struct {
	All   bool
	Units []string
	// CoopOnly: người gọi là quản lý HTX — chỉ bản ghi gắn với HTX Coop.
	CoopOnly bool
	Coop     uuid.UUID
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}
