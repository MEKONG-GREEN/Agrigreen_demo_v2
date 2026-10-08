package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MetricPlotsDigitised là số thửa đã số hoá (pf/metrics, defs/geo.plots_digitised.v1.yaml).
const MetricPlotsDigitised metricsport.Key = "geo.plots_digitised"

// MetricsSource thực hiện metrics/port.Source (+ ScopeLister) trên sổ plot_event và
// plot_boundary (recorded_at < Cutoff(D)), phạm vi theo xã; và metrics/port.GeometryResolver
// (hình học GeoJSON theo id dòng sổ bất biến — tra as-of cho phát lại lớp GIS).
type MetricsSource struct{ Svc *Service }

// AsOf thực hiện metrics/port.Source.
func (m MetricsSource) AsOf(ctx context.Context, key metricsport.Key, d clock.Date, sc metricsport.Scope) (metricsport.Value, error) {
	if m.Svc.Clock.Now().Before(metricsport.Cutoff(d)) {
		return metricsport.Value{}, metricsport.ErrNotClosed
	}
	return m.AsOfAt(ctx, key, d, metricsport.Cutoff(d), sc)
}

// AsOfAt tính với mốc recorded_at tường minh.
func (m MetricsSource) AsOfAt(ctx context.Context, key metricsport.Key, d clock.Date, cutoff time.Time, sc metricsport.Scope) (metricsport.Value, error) {
	v := metricsport.Value{Key: key, BusinessDate: d, Scope: sc, Cutoff: cutoff}
	if def, ok := metricsport.DefinitionFrom(ctx); ok {
		v.DefinitionVersion, v.Unit = def.Version, def.Unit
	}
	if key != MetricPlotsDigitised {
		return v, metricsport.ErrUnknownMetric
	}
	var units []string
	if len(sc.Units) > 0 {
		units = sc.Units
	}
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, sc.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var n int64
		var wmID *uuid.UUID
		var wmAt *time.Time
		if err := tx.QueryRow(ctx, `WITH ev AS (
		    SELECT DISTINCT ON (plot_ref) plot_ref, id, recorded_at, status, commune_code
		    FROM pf_coop.plot_event WHERE recorded_at < $1 ORDER BY plot_ref, seq DESC),
		  b AS (SELECT DISTINCT plot_ref FROM pf_coop.plot_boundary WHERE recorded_at < $1 AND became_current)
		  SELECT count(*), (array_agg(ev.id ORDER BY ev.recorded_at DESC, ev.id DESC))[1], max(ev.recorded_at)
		  FROM ev JOIN b ON b.plot_ref = ev.plot_ref
		  WHERE ev.status <> 'retired' AND ($2::text[] IS NULL OR ev.commune_code = ANY($2))`, cutoff.UTC(), units).Scan(&n, &wmID, &wmAt); err != nil {
			return err
		}
		v.Value = decimal.NewFromInt(n)
		if wmID != nil {
			v.Watermark.LastEventID = *wmID
		}
		if wmAt != nil {
			v.Watermark.RecordedAt = wmAt.UTC()
		}
		return nil
	})
	return v, err
}

// Scopes thực hiện metrics/port.ScopeLister: toàn tenant + từng xã có thửa.
func (m MetricsSource) Scopes(ctx context.Context, _ metricsport.Key, d clock.Date, tid uuid.UUID) ([]metricsport.Scope, error) {
	out := []metricsport.Scope{{TenantID: tid}}
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT commune_code FROM pf_coop.plot_event WHERE recorded_at < $1 ORDER BY 1`, metricsport.Cutoff(d).UTC())
		if err != nil {
			return err
		}
		codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, c := range codes {
			out = append(out, metricsport.Scope{TenantID: tid, Units: []string{c}})
		}
		return err
	})
	return out, err
}

// geometryTables: loại hình học (metricsport.GeometryRef.Kind) → sổ của module. facility_event do pf/geo phân
// giải (geo.Module.LayerGeometry) — gốc lắp ráp gộp.
var geometryTables = map[string]string{
	"plot_boundary":        "pf_coop.plot_boundary",
	"cooperative_boundary": "pf_coop.cooperative_boundary",
}

// Geometries thực hiện metrics/port.GeometryResolver. Tenant lấy từ context.
func (m MetricsSource) Geometries(ctx context.Context, refs []metricsport.GeometryRef) (map[metricsport.GeometryRef]json.RawMessage, error) {
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
