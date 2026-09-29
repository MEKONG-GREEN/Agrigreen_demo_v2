package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
)

// geomOr dựng hình MultiPolygon từ tham số GeoJSON n; rỗng → lấy hình hiện hành
// (biểu thức cur) — dùng khi dòng sổ ngừng dùng/khôi phục mang lại đúng hình đang có.
func geomOr(n, cur string) string {
	return "coalesce(ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(nullif(" + n + "::text, '')), 4326)), (" + cur + "))"
}

func decOf(s string) (decimal.Decimal, error) { return decimal.NewFromString(s) }

// ---------------------------------------------------------------------------
// Sổ cooperative_boundary (GIS US-BND-02/03/07)
// ---------------------------------------------------------------------------

// CoopBoundary là một phiên bản ranh giới HTX (một dòng sổ).
type CoopBoundary struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	CooperativeRef   uuid.UUID
	Version          int64
	Action           string
	AreaHa           decimal.Decimal
	PointCount       int
	Source           string
	CommuneCode      string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	GeoJSON          string // chỉ khi đọc kèm hình
}

// InsertCoopBoundary ghi một phiên bản; DB tính seq, băm và diện tích trắc địa.
// geojson rỗng: mang lại hình hiện hành của HTX (ngừng dùng/khôi phục).
func InsertCoopBoundary(ctx context.Context, tx pgx.Tx, b *CoopBoundary, geojson string) error {
	var area string
	err := tx.QueryRow(ctx, `INSERT INTO pf_geo.cooperative_boundary
	    (id, tenant_id, stream_key, cooperative_ref, action, geom, area_ha, point_count, source, commune_code, event_time,
	     supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  SELECT $1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, s.g, (ST_Area(s.g::geography) / 10000)::numeric(14,4), $7::int,
	         $8::text, $9::text, $10::timestamptz, $11::uuid, $12::text, $13::uuid, $14::text, $15::text, $16::text
	  FROM (SELECT `+geomOr("$6", "SELECT a.geom FROM pf_geo.cooperative_area a WHERE a.cooperative_ref = $4::uuid")+` AS g) s
	  RETURNING seq, recorded_at, area_ha::text`,
		b.ID, b.TenantID, domain.CoopStreamKey(b.CooperativeRef), b.CooperativeRef, b.Action, geojson, b.PointCount, b.Source,
		b.CommuneCode, b.EventTime.UTC(), b.SupersedesID, b.CorrectionReason, b.ActorRef, b.DeviceID, b.IdemKey,
		domain.RuleCooperativeBoundary).Scan(&b.Version, &b.RecordedAt, &area)
	if err != nil {
		return err
	}
	b.AreaHa, err = decOf(area)
	return err
}

const coopBoundaryCols = `id, tenant_id, cooperative_ref, seq, action, area_ha::text, point_count, source, commune_code,
  event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanCoopBoundary(row pgx.Row, withGeom bool) (CoopBoundary, error) {
	var b CoopBoundary
	var area string
	dest := []any{&b.ID, &b.TenantID, &b.CooperativeRef, &b.Version, &b.Action, &area, &b.PointCount, &b.Source, &b.CommuneCode,
		&b.EventTime, &b.RecordedAt, &b.SupersedesID, &b.CorrectionReason, &b.ActorRef, &b.DeviceID, &b.IdemKey}
	if withGeom {
		dest = append(dest, &b.GeoJSON)
	}
	if err := row.Scan(dest...); err != nil {
		return b, err
	}
	var err error
	b.AreaHa, err = decOf(area)
	return b, err
}

// CoopBoundaryByIdemKey tìm phiên bản đã ghi với idem_key (gọi lặp).
func CoopBoundaryByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*CoopBoundary, error) {
	b, err := scanCoopBoundary(tx.QueryRow(ctx, "SELECT "+coopBoundaryCols+" FROM pf_geo.cooperative_boundary WHERE idem_key = $1::text", key), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CoopBoundaries trả mọi phiên bản của HTX theo thứ tự, kèm hình (lịch sử chỉ đọc).
func CoopBoundaries(ctx context.Context, tx pgx.Tx, coop uuid.UUID) ([]CoopBoundary, error) {
	rows, err := tx.Query(ctx, "SELECT "+coopBoundaryCols+`, ST_AsGeoJSON(geom, 7)
	  FROM pf_geo.cooperative_boundary WHERE cooperative_ref = $1::uuid ORDER BY seq`, coop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopBoundary
	for rows.Next() {
		b, err := scanCoopBoundary(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu cooperative_area
// ---------------------------------------------------------------------------

// CoopArea là ranh giới HTX hiện hành.
type CoopArea struct {
	CooperativeRef    uuid.UUID
	CommuneCode       string
	Status            string
	StatusReason      string
	CurrentVersion    int64
	CurrentBoundaryID uuid.UUID
	AreaHa            decimal.Decimal
	Source            string
	Revision          int64
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	GeoJSON           string
	// Marker: điểm nằm trong vùng (ST_PointOnSurface) — dự phòng khi party chưa có location.
	MarkerLon, MarkerLat float64
}

const coopAreaCols = `a.cooperative_ref, a.commune_code, a.status, coalesce(a.status_reason, ''), a.current_version,
  a.current_boundary_id, a.area_ha::text, a.source, a.revision, a.created_by, a.created_at, a.updated_at,
  ST_X(ST_PointOnSurface(a.geom)), ST_Y(ST_PointOnSurface(a.geom))`

func scanCoopArea(row pgx.Row) (CoopArea, error) {
	var a CoopArea
	var area string
	if err := row.Scan(&a.CooperativeRef, &a.CommuneCode, &a.Status, &a.StatusReason, &a.CurrentVersion, &a.CurrentBoundaryID,
		&area, &a.Source, &a.Revision, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.MarkerLon, &a.MarkerLat, &a.GeoJSON); err != nil {
		return a, err
	}
	var err error
	a.AreaHa, err = decOf(area)
	return a, err
}

// CoopAreaByRef đọc ranh giới HTX hiện hành kèm hình; forUpdate khóa dòng.
func CoopAreaByRef(ctx context.Context, tx pgx.Tx, coop uuid.UUID, forUpdate bool) (*CoopArea, error) {
	q := "SELECT " + coopAreaCols + ", ST_AsGeoJSON(a.geom, 7) FROM pf_geo.cooperative_area a WHERE a.cooperative_ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanCoopArea(tx.QueryRow(ctx, q, coop))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// PutCoopArea ghi bảng chiếu theo dòng sổ vừa ghi (R-21: cùng giao dịch). Hình lấy
// từ chính dòng sổ nên bảng chiếu luôn trùng phiên bản hiện hành.
func PutCoopArea(ctx context.Context, tx pgx.Tx, b CoopBoundary, status, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_geo.cooperative_area
	    (tenant_id, cooperative_ref, commune_code, status, status_reason, current_version, current_boundary_id, geom, area_ha,
	     source, created_by)
	  SELECT b.tenant_id, b.cooperative_ref, b.commune_code, $2::text, $3::text, b.seq, b.id, b.geom, b.area_ha, b.source, b.actor_ref
	  FROM pf_geo.cooperative_boundary b WHERE b.id = $1::uuid
	  ON CONFLICT (tenant_id, cooperative_ref) DO UPDATE SET commune_code = EXCLUDED.commune_code, status = EXCLUDED.status,
	    status_reason = EXCLUDED.status_reason, current_version = EXCLUDED.current_version,
	    current_boundary_id = EXCLUDED.current_boundary_id, geom = EXCLUDED.geom, area_ha = EXCLUDED.area_ha,
	    source = EXCLUDED.source, revision = pf_geo.cooperative_area.revision + 1, updated_at = now()`,
		b.ID, status, nilIfEmpty(reason))
	return err
}

// CoopBBoxQuery là truy vấn ranh giới HTX theo khung nhìn.
type CoopBBoxQuery struct {
	Filter
	MinLon, MinLat, MaxLon, MaxLat float64
	SimplifyDeg                    float64
	Limit                          int
}

// ListCoopAreasBBox trả ranh giới HTX đang dùng giao khung nhìn, trong phạm vi.
func ListCoopAreasBBox(ctx context.Context, tx pgx.Tx, q CoopBBoxQuery) ([]CoopArea, error) {
	rows, err := tx.Query(ctx, "SELECT "+coopAreaCols+`,
	    ST_AsGeoJSON(ST_Multi(CASE WHEN $9::float8 > 0 THEN ST_SimplifyPreserveTopology(a.geom, $9::float8) ELSE a.geom END), 7)
	  FROM pf_geo.cooperative_area a
	  WHERE ($1::boolean OR a.commune_code = ANY($2::text[])) AND (NOT $3::boolean OR a.cooperative_ref = $4::uuid)
	    AND a.status = 'active' AND a.geom && ST_MakeEnvelope($5::float8, $6::float8, $7::float8, $8::float8, 4326)
	  ORDER BY a.cooperative_ref LIMIT $10::int`,
		q.All, units(q.Units), q.CoopOnly, q.Coop, q.MinLon, q.MinLat, q.MaxLon, q.MaxLat, q.SimplifyDeg, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopArea
	for rows.Next() {
		a, err := scanCoopArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
