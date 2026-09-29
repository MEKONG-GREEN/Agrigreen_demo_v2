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

// GpsFix là một dòng log toạ độ (HTX US-GPS-03).
type GpsFix struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Seq             int64
	ActorRef        uuid.UUID
	DeviceID        string
	Purpose         string
	TargetRef       uuid.UUID
	PlotRef         uuid.UUID
	CommuneCode     string
	CooperativeRef  uuid.UUID
	Lon, Lat        *float64
	AccuracyM       *decimal.Decimal
	Source          string
	DistanceToPlotM *decimal.Decimal
	EventTime       time.Time
	RecordedAt      time.Time
	IdemKey         string
}

// InsertGpsFix ghi log; DB tính seq, băm và khoảng cách trắc địa tới ranh giới
// hiện hành của lô (0 khi điểm nằm trong lô). pointGJ rỗng = không có điểm.
func InsertGpsFix(ctx context.Context, tx pgx.Tx, f *GpsFix, pointGJ string) error {
	var dist *string
	err := tx.QueryRow(ctx, `WITH pt AS (SELECT ST_SetSRID(ST_GeomFromGeoJSON(nullif($11::text, '')), 4326) AS g)
	  INSERT INTO pf_geo.gps_fix
	    (id, tenant_id, stream_key, actor_ref, device_id, purpose, target_ref, plot_ref, commune_code, cooperative_ref, point,
	     accuracy_m, source, distance_to_plot_m, event_time, idem_key, rule_version)
	  SELECT $1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::uuid, $8::uuid, $9::text, $10::uuid, pt.g,
	         $12::numeric, $13::text,
	         (SELECT round(ST_Distance(p.geom::geography, pt.g::geography)::numeric, 1) FROM pf_geo.plot p
	           WHERE pt.g IS NOT NULL AND p.ref = $8::uuid),
	         $14::timestamptz, $15::text, $16::text
	  FROM pt
	  RETURNING seq, recorded_at, distance_to_plot_m::text`,
		f.ID, f.TenantID, domain.GpsStreamKey(f.ActorRef), f.ActorRef, nilIfEmpty(f.DeviceID), f.Purpose, nilIfZero(f.TargetRef),
		nilIfZero(f.PlotRef), nilIfEmpty(f.CommuneCode), nilIfZero(f.CooperativeRef), pointGJ, decPtr(f.AccuracyM), f.Source,
		f.EventTime.UTC(), f.IdemKey, domain.RuleGpsFix).Scan(&f.Seq, &f.RecordedAt, &dist)
	if err != nil {
		return err
	}
	f.DistanceToPlotM, err = parseDec(dist)
	return err
}

const gpsCols = `g.id, g.tenant_id, g.seq, g.actor_ref, coalesce(g.device_id, ''), g.purpose, g.target_ref, g.plot_ref,
  coalesce(g.commune_code, ''), g.cooperative_ref, ST_X(g.point), ST_Y(g.point), g.accuracy_m::text, g.source,
  g.distance_to_plot_m::text, g.event_time, g.recorded_at, g.idem_key`

func scanGps(row pgx.Row) (GpsFix, error) {
	var f GpsFix
	var target, plot, coop *uuid.UUID
	var acc, dist *string
	if err := row.Scan(&f.ID, &f.TenantID, &f.Seq, &f.ActorRef, &f.DeviceID, &f.Purpose, &target, &plot, &f.CommuneCode, &coop,
		&f.Lon, &f.Lat, &acc, &f.Source, &dist, &f.EventTime, &f.RecordedAt, &f.IdemKey); err != nil {
		return f, err
	}
	f.TargetRef, f.PlotRef, f.CooperativeRef = deref(target), deref(plot), deref(coop)
	var err error
	if f.AccuracyM, err = parseDec(acc); err != nil {
		return f, err
	}
	f.DistanceToPlotM, err = parseDec(dist)
	return f, err
}

// GpsFixByIdemKey tìm log đã ghi với idem_key.
func GpsFixByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*GpsFix, error) {
	f, err := scanGps(tx.QueryRow(ctx, "SELECT "+gpsCols+" FROM pf_geo.gps_fix g WHERE g.idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// CommuneAt trả mã xã có ranh giới hiện hành chứa điểm (rỗng nếu chưa nhập ranh giới).
func CommuneAt(ctx context.Context, tx pgx.Tx, pointGJ string) (string, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT b.unit_code FROM pf_geo.admin_boundary b
	  WHERE b.level = 'commune' AND b.superseded_at IS NULL
	    AND ST_Intersects(b.geom, ST_SetSRID(ST_GeomFromGeoJSON($1::text), 4326))
	  ORDER BY b.unit_code LIMIT 1`, pointGJ).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, err
}

// GpsQuery là truy vấn log theo phạm vi, phân trang cursor theo id giảm dần (R-53).
type GpsQuery struct {
	Filter
	ActorRef  uuid.UUID
	TargetRef uuid.UUID
	PlotRef   uuid.UUID
	Purpose   string
	Before    uuid.UUID // Nil = từ mới nhất
	Limit     int
}

// ListGpsFixes trả log mới nhất trước. Log không có xã chỉ phạm vi toàn quốc thấy.
func ListGpsFixes(ctx context.Context, tx pgx.Tx, q GpsQuery) ([]GpsFix, error) {
	rows, err := tx.Query(ctx, "SELECT "+gpsCols+` FROM pf_geo.gps_fix g
	  WHERE ($1::boolean OR g.commune_code = ANY($2::text[]))
	    AND (NOT $3::boolean OR g.cooperative_ref = $4::uuid)
	    AND ($5::uuid IS NULL OR g.actor_ref = $5::uuid) AND ($6::uuid IS NULL OR g.target_ref = $6::uuid)
	    AND ($7::uuid IS NULL OR g.plot_ref = $7::uuid) AND ($8::text = '' OR g.purpose = $8::text)
	    AND ($9::uuid IS NULL OR g.id < $9::uuid)
	  ORDER BY g.id DESC LIMIT $10::int`,
		q.All, units(q.Units), q.CoopOnly, q.Coop, nilIfZero(q.ActorRef), nilIfZero(q.TargetRef), nilIfZero(q.PlotRef), q.Purpose,
		nilIfZero(q.Before), q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GpsFix
	for rows.Next() {
		f, err := scanGps(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
