// Package store: SQL của bản đồ vùng nuôi, vùng dịch CNTS (pf_extension.cnts_map_zone — sổ chỉ ghi thêm qua
// shared.apply_ledger, hình học PostGIS EPSG:4326) và các lớp đọc của bản đồ: ổ dịch BM-10 (cnts_outbreak, W11.49), điểm
// trình diễn và hộ mô hình có tọa độ (cnts_demo_site, cnts_model_household, cnts_model_point — W11.47). Tất cả là bảng
// của pf/extension; KHÔNG đọc bảng HTX (lớp HTX/thửa chỉ qua cổng PlotStatistics đọc pf_cooperative_pub). Chạy trên pool
// vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS) — W11.52.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ.
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cntsmap:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func refStrings(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func collect[T any](rows pgx.Rows, err error, scan func(pgx.Row) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func one[T any](v T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ---------------------------------------------------------------------------
// Vùng
// ---------------------------------------------------------------------------

// Zone là một phiên bản vùng.
type Zone struct {
	Meta
	Ref         uuid.UUID
	Event       string
	Province    string
	Communes    []string
	Kind        string
	Name        string
	Mode        string
	GeoJSON     json.RawMessage // ST_AsGeoJSON(geom) khi đọc
	Center      *domain.Point
	RadiusM     *float64
	AreaHa      *decimal.Decimal
	OutbreakRef *uuid.UUID
	ParentRef   *uuid.UUID
	Species     string
	Disease     string
	EffectiveOn time.Time
	Note        string
	Reason      string
}

const zoneCols = `id, seq, zone_ref, event, province_code, communes, kind, name, draw_mode, ST_AsGeoJSON(geom, 7), ST_Y(center),
  ST_X(center), radius_m::float8, area_ha::text, outbreak_ref, parent_zone_ref, coalesce(species_code, ''), coalesce(disease_code, ''),
  effective_on, coalesce(note, ''), coalesce(reason, ''), event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanZone(row pgx.Row) (Zone, error) {
	var z Zone
	var gj string
	var lat, lng *float64
	var area *string
	err := row.Scan(&z.ID, &z.Seq, &z.Ref, &z.Event, &z.Province, &z.Communes, &z.Kind, &z.Name, &z.Mode, &gj, &lat, &lng, &z.RadiusM,
		&area, &z.OutbreakRef, &z.ParentRef, &z.Species, &z.Disease, &z.EffectiveOn, &z.Note, &z.Reason, &z.EventTime, &z.RecordedAt,
		&z.ActorRef, &z.OnBehalfOf, &z.SupportReason, &z.IdemKey)
	if err != nil {
		return z, err
	}
	z.GeoJSON = json.RawMessage(gj)
	if lat != nil && lng != nil {
		z.Center = &domain.Point{Lat: *lat, Lng: *lng}
	}
	if area != nil {
		d, err := decimal.NewFromString(*area)
		if err != nil {
			return z, err
		}
		z.AreaHa = &d
	}
	return z, nil
}

// GeometryProblem trả lý do hình GeoJSON không hợp lệ theo PostGIS (ST_IsValidReason), "" nếu hợp lệ. Chạy trong
// SAVEPOINT để GeoJSON hỏng không làm hỏng giao dịch.
func GeometryProblem(ctx context.Context, tx pgx.Tx, geojson []byte) (string, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return "", err
	}
	var valid, empty bool
	var reason string
	err = sp.QueryRow(ctx, `SELECT ST_IsValid(g), ST_IsEmpty(g), ST_IsValidReason(g)
	  FROM (SELECT ST_SetSRID(ST_GeomFromGeoJSON($1::text), 4326) AS g) x`, string(geojson)).Scan(&valid, &empty, &reason)
	if err != nil {
		_ = sp.Rollback(ctx)
		return "GeoJSON không đọc được", nil //nolint:nilerr // lỗi phân tích của PostGIS là lỗi dữ liệu vào (422)
	}
	if err := sp.Commit(ctx); err != nil {
		return "", err
	}
	switch {
	case empty:
		return "hình rỗng", nil
	case !valid:
		return reason, nil
	}
	return "", nil
}

// InsertZone ghi một phiên bản vùng. Hình: CopyFrom (mã phiên bản cũ — ngưng/khôi phục giữ nguyên hình), hoặc
// ST_Buffer(geography) quanh Center với ModeRadius, hoặc GeoJSON. Trả lại hình đã lưu và diện tích.
func InsertZone(ctx context.Context, tx pgx.Tx, tid uuid.UUID, z *Zone, copyFrom *uuid.UUID) error {
	var lat, lng *float64
	if z.Center != nil {
		lat, lng = &z.Center.Lat, &z.Center.Lng
	}
	var gj *string
	if len(z.GeoJSON) > 0 && copyFrom == nil && z.Mode != domain.ModeRadius {
		s := string(z.GeoJSON)
		gj = &s
	}
	var out string
	var area *string
	err := tx.QueryRow(ctx, `WITH g AS (
	    SELECT CASE
	      WHEN $27::uuid IS NOT NULL THEN (SELECT z.geom FROM pf_extension.cnts_map_zone z WHERE z.id = $27::uuid)
	      WHEN $10 = 'radius' THEN ST_Buffer(ST_SetSRID(ST_MakePoint($12::float8, $11::float8), 4326)::geography, $13::float8,
	        'quad_segs=16')::geometry
	      ELSE ST_SetSRID(ST_GeomFromGeoJSON($14::text), 4326) END AS geom)
	  INSERT INTO pf_extension.cnts_map_zone (id, tenant_id, stream_key, zone_ref, event, province_code, communes, kind, name,
	    draw_mode, geom, center, radius_m, area_ha, outbreak_ref, parent_zone_ref, species_code, disease_code, effective_on, note,
	    reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, g.geom,
	    CASE WHEN $10 = 'radius' THEN ST_SetSRID(ST_MakePoint($12::float8, $11::float8), 4326) END,
	    CASE WHEN $10 = 'radius' THEN $13::float8::numeric END,
	    CASE WHEN GeometryType(g.geom) IN ('POLYGON', 'MULTIPOLYGON') THEN round((ST_Area(g.geom::geography) / 10000)::numeric, 4) END,
	    $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $28
	  FROM g
	  RETURNING seq, recorded_at, ST_AsGeoJSON(geom, 7), area_ha::text`,
		z.ID, tid, "czone:"+z.Ref.String(), z.Ref, z.Event, z.Province, z.Communes, z.Kind, z.Name,
		z.Mode, lat, lng, z.RadiusM, gj,
		z.OutbreakRef, z.ParentRef, optText(z.Species), optText(z.Disease), z.EffectiveOn.Format(time.DateOnly), optText(z.Note),
		optText(z.Reason), z.EventTime.UTC(), z.ActorRef, z.OnBehalfOf, z.SupportReason, z.IdemKey, copyFrom, domain.RuleVersion,
	).Scan(&z.Seq, &z.RecordedAt, &out, &area)
	if err != nil {
		return err
	}
	z.GeoJSON = json.RawMessage(out)
	if area != nil {
		d, err := decimal.NewFromString(*area)
		if err != nil {
			return err
		}
		z.AreaHa = &d
	}
	return nil
}

// LatestZone trả phiên bản hiện hành (nil = không có).
func LatestZone(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Zone, error) {
	return one(scanZone(tx.QueryRow(ctx, "SELECT "+zoneCols+` FROM pf_extension.cnts_map_zone
	  WHERE zone_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// ZoneByIdem trả phiên bản đã ghi với khóa idem.
func ZoneByIdem(ctx context.Context, tx pgx.Tx, key string) (*Zone, error) {
	return one(scanZone(tx.QueryRow(ctx, "SELECT "+zoneCols+" FROM pf_extension.cnts_map_zone WHERE idem_key = $1 LIMIT 1", key)))
}

// ZoneVersions trả mọi phiên bản (cũ trước).
func ZoneVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Zone, error) {
	rows, err := tx.Query(ctx, "SELECT "+zoneCols+" FROM pf_extension.cnts_map_zone WHERE zone_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanZone)
}

// ZoneFilter lọc vùng tại một ngày.
type ZoneFilter struct {
	Day         time.Time   // ngày xem (effective_on <= Day)
	Province    string      // "" = mọi tỉnh
	Kinds       []string    // nil = mọi loại
	OutbreakRef *uuid.UUID  // nil = mọi
	Refs        []uuid.UUID // nil = mọi
	WithRetired bool        // giữ cả vùng mà phiên bản tại ngày đó là "retired"
}

// ZonesAt trả, với mỗi vùng, phiên bản mới nhất có ngày áp dụng <= f.Day (FN-18 BR-01, AC-03).
func ZonesAt(ctx context.Context, tx pgx.Tx, f ZoneFilter) ([]Zone, error) {
	var refs []string
	if f.Refs != nil {
		refs = refStrings(f.Refs)
	}
	rows, err := tx.Query(ctx, "SELECT "+zoneCols+` FROM (
	    SELECT DISTINCT ON (zone_ref) * FROM pf_extension.cnts_map_zone
	    WHERE effective_on <= $1::date AND ($2 = '' OR province_code = $2)
	      AND ($3::text[] IS NULL OR kind = ANY ($3::text[]))
	      AND ($4::uuid IS NULL OR outbreak_ref = $4::uuid)
	      AND ($5::text[] IS NULL OR zone_ref = ANY ($5::text[]::uuid[]))
	    ORDER BY zone_ref, effective_on DESC, seq DESC) z
	  WHERE $6 OR event <> 'retired'
	  ORDER BY effective_on DESC, name, zone_ref`, f.Day.Format(time.DateOnly), f.Province, f.Kinds, f.OutbreakRef, refs, f.WithRetired)
	return collect(rows, err, scanZone)
}

// ---------------------------------------------------------------------------
// Ổ dịch (W11.49) — chỉ đọc
// ---------------------------------------------------------------------------

// Outbreak là trạng thái một ổ dịch BM-10 tại một ngày.
type Outbreak struct {
	Ref       uuid.UUID
	Version   int64
	Event     string
	Commune   string
	Disease   string
	Species   string
	Location  domain.Point
	AsOf      time.Time
	HerdSize  int
	Dead      int
	DistanceM *float64 // tới tâm vùng (khi đọc trong vùng)
}

const outbreakCols = `outbreak_ref, seq, event, commune_code, disease_code, species_code, ST_Y(location), ST_X(location), as_of,
  herd_size, dead`

// OutbreaksAt trả, với mỗi ổ dịch đã có ở ngày day, phiên bản diễn biến mới nhất có as_of <= day. inZone khác nil →
// chỉ ổ dịch nằm trong hình của phiên bản vùng đó.
func OutbreaksAt(ctx context.Context, tx pgx.Tx, day time.Time, inZone *uuid.UUID) ([]Outbreak, error) {
	rows, err := tx.Query(ctx, `WITH z AS (SELECT geom FROM pf_extension.cnts_map_zone WHERE $2::uuid IS NOT NULL AND id = $2::uuid)
	  SELECT `+outbreakCols+`,
	    CASE WHEN $2::uuid IS NOT NULL THEN round(ST_Distance(o.location::geography,
	      ST_Centroid((SELECT geom FROM z))::geography)::numeric, 0)::float8 END
	  FROM (SELECT DISTINCT ON (outbreak_ref) * FROM pf_extension.cnts_outbreak WHERE as_of <= $1::date
	        ORDER BY outbreak_ref, as_of DESC, seq DESC) o
	  WHERE $2::uuid IS NULL OR ST_Covers((SELECT geom FROM z), o.location)
	  ORDER BY as_of DESC, outbreak_ref`, day.Format(time.DateOnly), inZone)
	return collect(rows, err, func(r pgx.Row) (Outbreak, error) {
		var o Outbreak
		err := r.Scan(&o.Ref, &o.Version, &o.Event, &o.Commune, &o.Disease, &o.Species, &o.Location.Lat, &o.Location.Lng, &o.AsOf,
			&o.HerdSize, &o.Dead, &o.DistanceM)
		return o, err
	})
}

// LatestOutbreak trả bản hiện hành của ổ dịch (nil = không có).
func LatestOutbreak(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Outbreak, error) {
	return one(func() (Outbreak, error) {
		var o Outbreak
		err := tx.QueryRow(ctx, `SELECT `+outbreakCols+` FROM pf_extension.cnts_outbreak WHERE outbreak_ref = $1
		  ORDER BY seq DESC LIMIT 1`, ref).Scan(&o.Ref, &o.Version, &o.Event, &o.Commune, &o.Disease, &o.Species, &o.Location.Lat,
			&o.Location.Lng, &o.AsOf, &o.HerdSize, &o.Dead)
		return o, err
	}())
}

// ---------------------------------------------------------------------------
// Điểm trình diễn, hộ mô hình có tọa độ (W11.47) — chỉ đọc
// ---------------------------------------------------------------------------

// Place là một điểm trình diễn hoặc hộ mô hình đang hoạt động có tọa độ.
type Place struct {
	Kind        string // site | household
	Ref         uuid.UUID
	SiteRef     uuid.UUID // household: điểm của hộ; site: chính nó
	ProjectRef  uuid.UUID
	Commune     string
	Name        string // tên điểm (hộ: tên điểm của hộ — không có họ tên chủ hộ)
	ModelType   string
	Scale       *decimal.Decimal
	ScaleUnit   string
	Location    domain.Point
	DistanceM   *float64
	ProjectName string
}

// Places trả điểm trình diễn (chưa đóng) và hộ mô hình (chưa rút) có tọa độ. inZone khác nil → chỉ những điểm nằm
// trong hình của phiên bản vùng đó (ST_Covers), kèm khoảng cách tới tâm vùng.
func Places(ctx context.Context, tx pgx.Tx, inZone *uuid.UUID) ([]Place, error) {
	rows, err := tx.Query(ctx, `WITH z AS (SELECT geom FROM pf_extension.cnts_map_zone WHERE $1::uuid IS NOT NULL AND id = $1::uuid),
	  site AS (SELECT DISTINCT ON (site_ref) * FROM pf_extension.cnts_demo_site ORDER BY site_ref, seq DESC),
	  hh AS (SELECT DISTINCT ON (household_ref) * FROM pf_extension.cnts_model_household ORDER BY household_ref, seq DESC),
	  prj AS (SELECT DISTINCT ON (project_ref) project_ref, name FROM pf_extension.cnts_project ORDER BY project_ref, seq DESC),
	  x AS (
	    SELECT 'site' AS kind, s.site_ref AS ref, s.site_ref, s.project_ref, s.commune_code, s.name, s.model_type_code,
	      s.scale::text AS scale, coalesce(s.scale_unit, '') AS unit, p.point
	    FROM site s JOIN pf_extension.cnts_model_point p ON p.kind = 'site' AND p.ref = s.site_ref
	    WHERE s.event <> 'closed'
	    UNION ALL
	    SELECT 'household', h.household_ref, h.site_ref, h.project_ref, h.commune_code, s.name, s.model_type_code,
	      h.scale::text, coalesce(h.scale_unit, ''), p.point
	    FROM hh h JOIN site s ON s.site_ref = h.site_ref
	      JOIN pf_extension.cnts_model_point p ON p.kind = 'household' AND p.ref = h.household_ref
	    WHERE h.event <> 'withdrawn')
	  SELECT x.kind, x.ref, x.site_ref, x.project_ref, x.commune_code, x.name, x.model_type_code, x.scale, x.unit,
	    ST_Y(x.point), ST_X(x.point), coalesce(prj.name, ''),
	    CASE WHEN $1::uuid IS NOT NULL THEN round(ST_Distance(x.point::geography,
	      ST_Centroid((SELECT geom FROM z))::geography)::numeric, 0)::float8 END
	  FROM x LEFT JOIN prj ON prj.project_ref = x.project_ref
	  WHERE $1::uuid IS NULL OR ST_Covers((SELECT geom FROM z), x.point)
	  ORDER BY x.commune_code, x.name, x.kind DESC, x.ref`, inZone)
	return collect(rows, err, func(r pgx.Row) (Place, error) {
		var p Place
		var scale *string
		if err := r.Scan(&p.Kind, &p.Ref, &p.SiteRef, &p.ProjectRef, &p.Commune, &p.Name, &p.ModelType, &scale, &p.ScaleUnit,
			&p.Location.Lat, &p.Location.Lng, &p.ProjectName, &p.DistanceM); err != nil {
			return p, err
		}
		if scale != nil {
			d, err := decimal.NewFromString(*scale)
			if err != nil {
				return p, err
			}
			p.Scale = &d
		}
		return p, nil
	})
}
