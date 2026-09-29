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

// pointOr dựng điểm từ tham số GeoJSON n; rỗng → biểu thức cur.
func pointOr(n, cur string) string {
	return "coalesce(ST_SetSRID(ST_GeomFromGeoJSON(nullif(" + n + "::text, '')), 4326), (" + cur + "))"
}

// FacilityEvent là một dòng sổ biến động cơ sở (ảnh chụp sau sự việc).
type FacilityEvent struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	FacilityRef      uuid.UUID
	Version          int64 // seq
	Action           string
	Kind             string
	Code             string
	Name             string
	Lon, Lat         float64
	HasPolygon       bool
	AreaHa           *decimal.Decimal
	ProvinceCode     string
	CommuneCode      string
	Capacity         decimal.Decimal
	Unit             string
	Status           string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	GeoJSON          string // khuôn viên, khi đọc kèm hình
}

// InsertFacilityEvent ghi một dòng sổ. pointGJ rỗng → giữ điểm hiện hành, hoặc
// lấy điểm trong khuôn viên khi chưa có; polyGJ rỗng → giữ khuôn viên hiện hành
// (keepPoly = true) hoặc bỏ khuôn viên (keepPoly = false).
func InsertFacilityEvent(ctx context.Context, tx pgx.Tx, e *FacilityEvent, pointGJ, polyGJ string, keepPoly bool) error {
	cur := "SELECT f.point FROM pf_geo.facility f WHERE f.ref = $3::uuid"
	curPoly := "SELECT NULL::geometry"
	if keepPoly {
		curPoly = "SELECT f.geom FROM pf_geo.facility f WHERE f.ref = $3::uuid"
	}
	var area *string
	err := tx.QueryRow(ctx, `INSERT INTO pf_geo.facility_event
	    (id, tenant_id, stream_key, facility_ref, action, kind, code, name, point, geom, area_ha, province_code,
	     commune_code, capacity, unit, status, event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key,
	     rule_version)
	  SELECT $1::uuid, $2::uuid, $4::text, $3::uuid, $5::text, $6::text, $7::text, $8::text,
	         coalesce(s.p, ST_PointOnSurface(s.g)), s.g,
	         CASE WHEN s.g IS NULL THEN NULL ELSE (ST_Area(s.g::geography) / 10000)::numeric(14,4) END,
	         $11::text, $12::text, $13::numeric, $14::text, $15::text, $16::timestamptz, $17::uuid, $18::text, $19::uuid,
	         $20::text, $21::text, $22::text
	  FROM (SELECT `+pointOr("$9", cur)+` AS p, `+geomOr("$10", curPoly)+` AS g) s
	  RETURNING seq, recorded_at, area_ha::text, ST_X(point), ST_Y(point), geom IS NOT NULL`,
		e.ID, e.TenantID, e.FacilityRef, domain.FacilityStreamKey(e.FacilityRef), e.Action, e.Kind, e.Code, e.Name,
		pointGJ, polyGJ, e.ProvinceCode, e.CommuneCode, e.Capacity.String(), e.Unit, e.Status, e.EventTime.UTC(),
		e.SupersedesID, e.CorrectionReason, e.ActorRef, e.DeviceID, e.IdemKey, domain.RuleFacility).
		Scan(&e.Version, &e.RecordedAt, &area, &e.Lon, &e.Lat, &e.HasPolygon)
	if err != nil {
		return err
	}
	e.AreaHa, err = parseDec(area)
	return err
}

const facilityEventCols = `id, tenant_id, facility_ref, seq, action, kind, code, name, ST_X(point), ST_Y(point),
  geom IS NOT NULL, area_ha::text, province_code, commune_code, capacity::text, unit, status, event_time,
  recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanFacilityEvent(row pgx.Row) (FacilityEvent, error) {
	var e FacilityEvent
	var area *string
	var capacity string
	if err := row.Scan(&e.ID, &e.TenantID, &e.FacilityRef, &e.Version, &e.Action, &e.Kind, &e.Code, &e.Name, &e.Lon,
		&e.Lat, &e.HasPolygon, &area, &e.ProvinceCode, &e.CommuneCode, &capacity, &e.Unit, &e.Status, &e.EventTime,
		&e.RecordedAt, &e.SupersedesID, &e.CorrectionReason, &e.ActorRef, &e.DeviceID, &e.IdemKey); err != nil {
		return e, err
	}
	var err error
	if e.AreaHa, err = parseDec(area); err != nil {
		return e, err
	}
	e.Capacity, err = decOf(capacity)
	return e, err
}

// FacilityEventByIdemKey tìm dòng sổ đã ghi với idem_key.
func FacilityEventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*FacilityEvent, error) {
	e, err := scanFacilityEvent(tx.QueryRow(ctx, "SELECT "+facilityEventCols+" FROM pf_geo.facility_event WHERE idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// FacilityEvents trả lịch sử biến động của cơ sở (Event Log, BR-14/19).
func FacilityEvents(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]FacilityEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+facilityEventCols+" FROM pf_geo.facility_event WHERE facility_ref = $1::uuid ORDER BY seq", ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FacilityEvent
	for rows.Next() {
		e, err := scanFacilityEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Facility là trạng thái hiện tại của cơ sở.
type Facility struct {
	Ref            uuid.UUID
	Kind           string
	Code           string
	Name           string
	Lon, Lat       float64
	AreaHa         *decimal.Decimal
	ProvinceCode   string
	CommuneCode    string
	Capacity       decimal.Decimal
	Unit           string
	Status         string
	StatusReason   string
	CurrentVersion int64
	CurrentEventID uuid.UUID
	Revision       int64
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	GeoJSON        string // khuôn viên; rỗng nếu không có
}

const facilityCols = `f.ref, f.kind, f.code, f.name, ST_X(f.point), ST_Y(f.point), f.area_ha::text, f.province_code,
  f.commune_code, f.capacity::text, f.unit, f.status, coalesce(f.status_reason, ''), f.current_version,
  f.current_event_id, f.revision, f.created_by, f.created_at, f.updated_at, coalesce(ST_AsGeoJSON(f.geom, 7), '')`

func scanFacility(row pgx.Row) (Facility, error) {
	var f Facility
	var area *string
	var capacity string
	if err := row.Scan(&f.Ref, &f.Kind, &f.Code, &f.Name, &f.Lon, &f.Lat, &area, &f.ProvinceCode, &f.CommuneCode,
		&capacity, &f.Unit, &f.Status, &f.StatusReason, &f.CurrentVersion, &f.CurrentEventID, &f.Revision, &f.CreatedBy,
		&f.CreatedAt, &f.UpdatedAt, &f.GeoJSON); err != nil {
		return f, err
	}
	var err error
	if f.AreaHa, err = parseDec(area); err != nil {
		return f, err
	}
	f.Capacity, err = decOf(capacity)
	return f, err
}

// FacilityByRef đọc cơ sở; forUpdate khóa dòng.
func FacilityByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Facility, error) {
	q := "SELECT " + facilityCols + " FROM pf_geo.facility f WHERE f.ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	f, err := scanFacility(tx.QueryRow(ctx, q, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// FacilityByCode đọc cơ sở theo mã (tệp nhập sức chứa theo mã).
func FacilityByCode(ctx context.Context, tx pgx.Tx, code string) (*Facility, error) {
	f, err := scanFacility(tx.QueryRow(ctx, "SELECT "+facilityCols+" FROM pf_geo.facility f WHERE f.code = $1::text", code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// PutFacility ghi bảng chiếu theo dòng sổ vừa ghi (R-21).
func PutFacility(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, statusReason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_geo.facility
	    (tenant_id, ref, kind, code, name, point, geom, area_ha, province_code, commune_code, capacity, unit, status,
	     status_reason, current_version, current_event_id, created_by)
	  SELECT e.tenant_id, e.facility_ref, e.kind, e.code, e.name, e.point, e.geom, e.area_ha, e.province_code,
	         e.commune_code, e.capacity, e.unit, e.status, $2::text, e.seq, e.id, e.actor_ref
	  FROM pf_geo.facility_event e WHERE e.id = $1::uuid
	  ON CONFLICT (tenant_id, ref) DO UPDATE SET kind = EXCLUDED.kind, code = EXCLUDED.code, name = EXCLUDED.name,
	    point = EXCLUDED.point, geom = EXCLUDED.geom, area_ha = EXCLUDED.area_ha,
	    province_code = EXCLUDED.province_code, commune_code = EXCLUDED.commune_code, capacity = EXCLUDED.capacity,
	    unit = EXCLUDED.unit, status = EXCLUDED.status, status_reason = EXCLUDED.status_reason,
	    current_version = EXCLUDED.current_version, current_event_id = EXCLUDED.current_event_id,
	    revision = pf_geo.facility.revision + 1, updated_at = now()`, eventID, nilIfEmpty(statusReason))
	return err
}

// BBox là khung nhìn WGS84.
type BBox struct{ MinLon, MinLat, MaxLon, MaxLat float64 }

// FacilityQuery là truy vấn cơ sở theo khung nhìn.
type FacilityQuery struct {
	BBox
	Kind           string // rỗng = mọi loại
	IncludeRetired bool
	Limit          int
}

// ListFacilitiesBBox trả cơ sở có điểm trong khung nhìn.
func ListFacilitiesBBox(ctx context.Context, tx pgx.Tx, q FacilityQuery) ([]Facility, error) {
	rows, err := tx.Query(ctx, "SELECT "+facilityCols+` FROM pf_geo.facility f
	  WHERE f.point && ST_MakeEnvelope($1::float8, $2::float8, $3::float8, $4::float8, 4326)
	    AND ($5::text = '' OR f.kind = $5::text) AND ($6::boolean OR f.status = 'active')
	  ORDER BY f.ref LIMIT $7::int`, q.MinLon, q.MinLat, q.MaxLon, q.MaxLat, q.Kind, q.IncludeRetired, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Facility
	for rows.Next() {
		f, err := scanFacility(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Mô hình đọc hub MKG (hub_view)
// ---------------------------------------------------------------------------

// Hub là một hub MKG nhận qua van công bố.
type Hub struct {
	HubRef       uuid.UUID
	Code         string
	Name         string
	Kind         string
	Lon, Lat     float64
	ProvinceCode string
	CommuneCode  string
	Capacity     *decimal.Decimal
	Unit         string
	Status       string
	Version      int64
	PublishedAt  time.Time
	ReceivedAt   time.Time
	GeoJSON      string
}

// UpsertHub ghi hub nếu phiên bản mới hơn bản đang có; applied = false khi bản cũ/trùng.
func UpsertHub(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, h Hub, pointGJ, polyGJ string) (bool, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO pf_geo.hub_view
	    (tenant_id, hub_ref, code, name, kind, point, geom, province_code, commune_code, capacity, unit, status, version, published_at)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, ST_SetSRID(ST_GeomFromGeoJSON($6::text), 4326),
	    ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(nullif($7::text, '')), 4326)), $8::text, $9::text, $10::numeric, $11::text,
	    $12::text, $13::bigint, $14::timestamptz)
	  ON CONFLICT (tenant_id, hub_ref) DO UPDATE SET code = EXCLUDED.code, name = EXCLUDED.name, kind = EXCLUDED.kind,
	    point = EXCLUDED.point, geom = EXCLUDED.geom, province_code = EXCLUDED.province_code,
	    commune_code = EXCLUDED.commune_code, capacity = EXCLUDED.capacity, unit = EXCLUDED.unit, status = EXCLUDED.status,
	    version = EXCLUDED.version, published_at = EXCLUDED.published_at, received_at = now(), updated_at = now()
	  WHERE pf_geo.hub_view.version < EXCLUDED.version
	  RETURNING hub_ref`,
		tenantID, h.HubRef, h.Code, h.Name, h.Kind, pointGJ, polyGJ, nilIfEmpty(h.ProvinceCode), nilIfEmpty(h.CommuneCode),
		decPtr(h.Capacity), h.Unit, h.Status, h.Version, h.PublishedAt.UTC()).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

const hubCols = `h.hub_ref, h.code, h.name, h.kind, ST_X(h.point), ST_Y(h.point), coalesce(h.province_code, ''),
  coalesce(h.commune_code, ''), h.capacity::text, h.unit, h.status, h.version, h.published_at, h.received_at,
  coalesce(ST_AsGeoJSON(h.geom, 7), '')`

func scanHub(row pgx.Row) (Hub, error) {
	var h Hub
	var capacity *string
	if err := row.Scan(&h.HubRef, &h.Code, &h.Name, &h.Kind, &h.Lon, &h.Lat, &h.ProvinceCode, &h.CommuneCode, &capacity, &h.Unit,
		&h.Status, &h.Version, &h.PublishedAt, &h.ReceivedAt, &h.GeoJSON); err != nil {
		return h, err
	}
	var err error
	h.Capacity, err = parseDec(capacity)
	return h, err
}

// HubByRef đọc hub MKG.
func HubByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Hub, error) {
	h, err := scanHub(tx.QueryRow(ctx, "SELECT "+hubCols+" FROM pf_geo.hub_view h WHERE h.hub_ref = $1::uuid", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// ListHubsBBox trả hub MKG đang hoạt động có điểm trong khung nhìn.
func ListHubsBBox(ctx context.Context, tx pgx.Tx, q FacilityQuery) ([]Hub, error) {
	rows, err := tx.Query(ctx, "SELECT "+hubCols+` FROM pf_geo.hub_view h
	  WHERE h.point && ST_MakeEnvelope($1::float8, $2::float8, $3::float8, $4::float8, 4326)
	    AND ($5::text = '' OR h.kind = $5::text) AND ($6::boolean OR h.status = 'active')
	  ORDER BY h.hub_ref LIMIT $7::int`, q.MinLon, q.MinLat, q.MaxLon, q.MaxLat, q.Kind, q.IncludeRetired, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hub
	for rows.Next() {
		h, err := scanHub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
