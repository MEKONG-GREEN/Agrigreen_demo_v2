package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func parseDecPtr(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ActiveCooperativeSites liệt kê HTX đang hoạt động kèm vị trí hồ sơ và số hộ thành viên đang mở.
func ActiveCooperativeSites(ctx context.Context, tx pgx.Tx) ([]port.CooperativeSite, error) {
	rows, err := tx.Query(ctx, `SELECT o.ref, o.code, coalesce(o.admin_unit_code, ''),
	    ST_X(c.location)::numeric(10,7)::text, ST_Y(c.location)::numeric(10,7)::text,
	    (SELECT count(*) FROM pf_party.household_membership m
	       JOIN pf_party.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref AND h.active
	      WHERE m.tenant_id = o.tenant_id AND m.org_ref = o.ref AND m.until_date IS NULL)
	  FROM pf_party.org o LEFT JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref
	  WHERE o.kind = 'cooperative' AND o.active ORDER BY o.ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.CooperativeSite
	for rows.Next() {
		var s port.CooperativeSite
		var lon, lat *string
		if err := rows.Scan(&s.Ref, &s.Code, &s.CommuneCode, &lon, &lat, &s.Members); err != nil {
			return nil, err
		}
		if s.Lon, err = parseDecPtr(lon); err != nil {
			return nil, err
		}
		if s.Lat, err = parseDecPtr(lat); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CoopMarkersInBBox đọc điểm vị trí hồ sơ của HTX đang hoạt động trong khung nhìn WGS84.
func CoopMarkersInBBox(ctx context.Context, tx pgx.Tx, minLon, minLat, maxLon, maxLat float64, limit int) ([]geoport.CoopMarker, error) {
	rows, err := tx.Query(ctx, `SELECT o.ref, o.code, o.name, coalesce(o.admin_unit_code, ''),
	    ST_X(c.location)::numeric(10,7)::text, ST_Y(c.location)::numeric(10,7)::text
	  FROM pf_party.cooperative c JOIN pf_party.org o ON o.tenant_id = c.tenant_id AND o.ref = c.org_ref
	  WHERE o.kind = 'cooperative' AND o.active AND c.location IS NOT NULL
	    AND c.location && ST_MakeEnvelope($1, $2, $3, $4, 4326)
	  ORDER BY o.ref LIMIT $5`, minLon, minLat, maxLon, maxLat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []geoport.CoopMarker
	for rows.Next() {
		var m geoport.CoopMarker
		var lon, lat string
		if err := rows.Scan(&m.Ref, &m.Code, &m.Name, &m.CommuneCode, &lon, &lat); err != nil {
			return nil, err
		}
		if m.Lon, err = decimal.NewFromString(lon); err != nil {
			return nil, err
		}
		if m.Lat, err = decimal.NewFromString(lat); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
