package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// GeometrySource thực hiện metrics/port.GeometryResolver cho sổ facility_event của pf/geo (hình học GeoJSON theo id
// dòng sổ bất biến — tra as-of cho phát lại lớp GIS). Loại plot_boundary, cooperative_boundary và chỉ số
// geo.plots_digitised do pf/cooperative phục vụ (W3.3) — gốc lắp ráp gộp hai bộ phân giải.
type GeometrySource struct{ Svc *Service }

var _ metricsport.GeometryResolver = GeometrySource{}

var geometryTables = map[string]string{
	"facility_event": "pf_geo.facility_event",
}

// Geometries thực hiện metrics/port.GeometryResolver. Tenant lấy từ context.
func (m GeometrySource) Geometries(ctx context.Context, refs []metricsport.GeometryRef) (map[metricsport.GeometryRef]json.RawMessage, error) {
	byKind := map[string][]string{}
	for _, r := range refs {
		if _, ok := geometryTables[r.Kind]; ok {
			byKind[r.Kind] = append(byKind[r.Kind], r.ID.String())
		}
	}
	out := map[metricsport.GeometryRef]json.RawMessage{}
	if len(byKind) == 0 {
		return out, nil
	}
	err := tenant.FromCtxTx(ctx, m.Svc.DB, func(tx pgx.Tx) error {
		for kind, ids := range byKind {
			rows, err := tx.Query(ctx, `SELECT id, ST_AsGeoJSON(geom, 7) FROM `+geometryTables[kind]+` WHERE id = ANY($1::uuid[]) AND geom IS NOT NULL`, ids)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uuid.UUID
				var g string
				if err := rows.Scan(&id, &g); err != nil {
					rows.Close()
					return err
				}
				out[metricsport.GeometryRef{Kind: kind, ID: id}] = json.RawMessage(g)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
