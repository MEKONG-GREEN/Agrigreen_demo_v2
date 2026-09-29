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

// RoadSegment là một phiên bản tuyến đường bộ (một dòng sổ).
type RoadSegment struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	RoadRef          uuid.UUID
	Version          int64
	Action           string
	Name             string
	RoadClass        string
	MaxLoadTons      decimal.Decimal
	LengthM          decimal.Decimal
	PointCount       int
	ProvinceCode     string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	GeoJSON          string
}

// InsertRoadSegment ghi một phiên bản tuyến; DB tính seq, băm và chiều dài trắc
// địa (GIS US-ROAD-02 AC-1: tự tính, không nhập tay). geojson rỗng → mang lại hình hiện hành.
func InsertRoadSegment(ctx context.Context, tx pgx.Tx, s *RoadSegment, geojson string) error {
	var length string
	err := tx.QueryRow(ctx, `INSERT INTO pf_geo.road_segment
	    (id, tenant_id, stream_key, road_ref, action, name, road_class, max_load_tons, geom, length_m, point_count, province_code,
	     event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  SELECT $1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::numeric, s.g,
	         round(ST_Length(s.g::geography)::numeric, 2), $10::int, $11::text, $12::timestamptz, $13::uuid, $14::text,
	         $15::uuid, $16::text, $17::text, $18::text
	  FROM (SELECT coalesce(ST_SetSRID(ST_GeomFromGeoJSON(nullif($9::text, '')), 4326),
	                        (SELECT r.geom FROM pf_geo.road r WHERE r.ref = $4::uuid)) AS g) s
	  RETURNING seq, recorded_at, length_m::text`,
		s.ID, s.TenantID, domain.RoadStreamKey(s.RoadRef), s.RoadRef, s.Action, s.Name, s.RoadClass, s.MaxLoadTons.String(), geojson,
		s.PointCount, s.ProvinceCode, s.EventTime.UTC(), s.SupersedesID, s.CorrectionReason, s.ActorRef, s.DeviceID, s.IdemKey,
		domain.RuleRoad).Scan(&s.Version, &s.RecordedAt, &length)
	if err != nil {
		return err
	}
	s.LengthM, err = decOf(length)
	return err
}

const roadSegmentCols = `id, tenant_id, road_ref, seq, action, name, road_class, max_load_tons::text, length_m::text, point_count,
  province_code, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanRoadSegment(row pgx.Row, withGeom bool) (RoadSegment, error) {
	var s RoadSegment
	var load, length string
	dest := []any{&s.ID, &s.TenantID, &s.RoadRef, &s.Version, &s.Action, &s.Name, &s.RoadClass, &load, &length, &s.PointCount,
		&s.ProvinceCode, &s.EventTime, &s.RecordedAt, &s.SupersedesID, &s.CorrectionReason, &s.ActorRef, &s.DeviceID, &s.IdemKey}
	if withGeom {
		dest = append(dest, &s.GeoJSON)
	}
	if err := row.Scan(dest...); err != nil {
		return s, err
	}
	var err error
	if s.MaxLoadTons, err = decOf(load); err != nil {
		return s, err
	}
	s.LengthM, err = decOf(length)
	return s, err
}

// RoadSegmentByIdemKey tìm phiên bản đã ghi với idem_key.
func RoadSegmentByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*RoadSegment, error) {
	s, err := scanRoadSegment(tx.QueryRow(ctx, "SELECT "+roadSegmentCols+" FROM pf_geo.road_segment WHERE idem_key = $1::text", key), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// RoadSegments trả mọi phiên bản của tuyến, kèm hình.
func RoadSegments(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]RoadSegment, error) {
	rows, err := tx.Query(ctx, "SELECT "+roadSegmentCols+`, ST_AsGeoJSON(geom, 7)
	  FROM pf_geo.road_segment WHERE road_ref = $1::uuid ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoadSegment
	for rows.Next() {
		s, err := scanRoadSegment(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Road là trạng thái hiện tại của tuyến.
type Road struct {
	Ref              uuid.UUID
	Name             string
	RoadClass        string
	MaxLoadTons      decimal.Decimal
	LengthM          decimal.Decimal
	ProvinceCode     string
	Status           string
	StatusReason     string
	CurrentVersion   int64
	CurrentSegmentID uuid.UUID
	Revision         int64
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
	GeoJSON          string
}

const roadCols = `r.ref, r.name, r.road_class, r.max_load_tons::text, r.length_m::text, r.province_code, r.status,
  coalesce(r.status_reason, ''), r.current_version, r.current_segment_id, r.revision, r.created_by, r.created_at, r.updated_at`

func scanRoad(row pgx.Row) (Road, error) {
	var r Road
	var load, length string
	if err := row.Scan(&r.Ref, &r.Name, &r.RoadClass, &load, &length, &r.ProvinceCode, &r.Status, &r.StatusReason,
		&r.CurrentVersion, &r.CurrentSegmentID, &r.Revision, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &r.GeoJSON); err != nil {
		return r, err
	}
	var err error
	if r.MaxLoadTons, err = decOf(load); err != nil {
		return r, err
	}
	r.LengthM, err = decOf(length)
	return r, err
}

// RoadByRef đọc tuyến kèm hình; forUpdate khóa dòng.
func RoadByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Road, error) {
	q := "SELECT " + roadCols + ", ST_AsGeoJSON(r.geom, 7) FROM pf_geo.road r WHERE r.ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	r, err := scanRoad(tx.QueryRow(ctx, q, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// PutRoad ghi bảng chiếu theo phiên bản vừa ghi (R-21).
func PutRoad(ctx context.Context, tx pgx.Tx, segmentID uuid.UUID, status, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_geo.road
	    (tenant_id, ref, name, road_class, max_load_tons, geom, length_m, province_code, status, status_reason,
	     current_version, current_segment_id, created_by)
	  SELECT s.tenant_id, s.road_ref, s.name, s.road_class, s.max_load_tons, s.geom, s.length_m, s.province_code, $2::text,
	         $3::text, s.seq, s.id, s.actor_ref
	  FROM pf_geo.road_segment s WHERE s.id = $1::uuid
	  ON CONFLICT (tenant_id, ref) DO UPDATE SET name = EXCLUDED.name, road_class = EXCLUDED.road_class,
	    max_load_tons = EXCLUDED.max_load_tons, geom = EXCLUDED.geom, length_m = EXCLUDED.length_m,
	    province_code = EXCLUDED.province_code, status = EXCLUDED.status, status_reason = EXCLUDED.status_reason,
	    current_version = EXCLUDED.current_version, current_segment_id = EXCLUDED.current_segment_id,
	    revision = pf_geo.road.revision + 1, updated_at = now()`, segmentID, status, nilIfEmpty(reason))
	return err
}

// RoadQuery là truy vấn tuyến theo khung nhìn (GIS US-ROAD-01, BR-30).
type RoadQuery struct {
	BBox
	MinLoadTons *decimal.Decimal // chỉ tuyến có tải trọng cho phép ≥ giá trị
	RoadClass   string
	SimplifyDeg float64
	Limit       int
}

// ListRoadsBBox trả tuyến đang dùng giao khung nhìn, lọc theo tải trọng tối thiểu.
func ListRoadsBBox(ctx context.Context, tx pgx.Tx, q RoadQuery) ([]Road, error) {
	rows, err := tx.Query(ctx, "SELECT "+roadCols+`,
	    ST_AsGeoJSON(CASE WHEN $7::float8 > 0 THEN ST_SimplifyPreserveTopology(r.geom, $7::float8) ELSE r.geom END, 7)
	  FROM pf_geo.road r
	  WHERE r.status = 'active' AND r.geom && ST_MakeEnvelope($1::float8, $2::float8, $3::float8, $4::float8, 4326)
	    AND ($5::numeric IS NULL OR r.max_load_tons >= $5::numeric) AND ($6::text = '' OR r.road_class = $6::text)
	  ORDER BY r.max_load_tons DESC, r.ref LIMIT $8::int`,
		q.MinLon, q.MinLat, q.MaxLon, q.MaxLat, decPtr(q.MinLoadTons), q.RoadClass, q.SimplifyDeg, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Road
	for rows.Next() {
		r, err := scanRoad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
