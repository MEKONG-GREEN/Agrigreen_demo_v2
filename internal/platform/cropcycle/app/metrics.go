package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Chỉ số App HTX theo vụ (pf/metrics, defs/cc.*.yaml).
const (
	MetricSeasonAreaHa  metricsport.Key = "cc.season_area_ha"
	MetricYieldTPerHa   metricsport.Key = "cc.yield_t_per_ha"
	metricSeasonHistory                 = 2 // job ghi các vụ của năm D và 2 năm trước
)

// MetricsSource thực hiện metrics/port.Source (+ ScopeLister) trên sổ season_event và
// harvest_declaration: sự việc mới nhất (seq) của mỗi vụ thửa có recorded_at <
// Cutoff(D). Phạm vi theo xã (commune_code). Đọc chỉ đọc bằng vai cropcycle_app.
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
	if key != MetricSeasonAreaHa && key != MetricYieldTPerHa {
		return v, metricsport.ErrUnknownMetric
	}
	code, year, ok := metricsport.ParseSeason(sc.Dimension)
	if !ok {
		return v, metricsport.ErrNoData
	}
	var units []string
	if len(sc.Units) > 0 {
		units = sc.Units
	}
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, sc.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var n int64
		var a, b string
		var wmID *uuid.UUID
		var wmAt *time.Time
		q := `WITH last AS (
		    SELECT DISTINCT ON (stream_key) id, recorded_at, action, area_ha, commune_code, season_code, season_year
		    FROM pf_cropcycle.season_event WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
		  SELECT count(*) FILTER (WHERE action <> 'cancelled'), coalesce(sum(area_ha) FILTER (WHERE action <> 'cancelled'), 0)::text, '0',
		         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
		  FROM last WHERE upper(season_code) = $2 AND season_year = $3 AND ($4::text[] IS NULL OR commune_code = ANY($4))`
		if key == MetricYieldTPerHa {
			q = `WITH last AS (
			    SELECT DISTINCT ON (stream_key) id, recorded_at, mass_kg, area_ha, commune_code, season_code, season_year
			    FROM pf_cropcycle.harvest_declaration WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
			  SELECT count(*), coalesce(sum(mass_kg), 0)::text, coalesce(sum(area_ha), 0)::text,
			         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
			  FROM last WHERE upper(season_code) = $2 AND season_year = $3 AND ($4::text[] IS NULL OR commune_code = ANY($4))`
		}
		if err := tx.QueryRow(ctx, q, cutoff.UTC(), strings.ToUpper(code), year, units).Scan(&n, &a, &b, &wmID, &wmAt); err != nil {
			return err
		}
		if n == 0 {
			return metricsport.ErrNoData
		}
		x, err := decimal.NewFromString(a)
		if err != nil {
			return err
		}
		y, err := decimal.NewFromString(b)
		if err != nil {
			return err
		}
		v.Value = x
		if key == MetricYieldTPerHa {
			if !y.IsPositive() {
				return metricsport.ErrNoData
			}
			v.Value = x.Div(decimal.NewFromInt(1000)).Div(y).Round(4)
		}
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

// Scopes thực hiện metrics/port.ScopeLister: mỗi vụ (năm D và hai năm trước) có dữ
// liệu trước Cutoff(D), toàn tenant và từng xã.
func (m MetricsSource) Scopes(ctx context.Context, key metricsport.Key, d clock.Date, tid uuid.UUID) ([]metricsport.Scope, error) {
	table := "pf_cropcycle.season_event"
	if key == MetricYieldTPerHa {
		table = "pf_cropcycle.harvest_declaration"
	}
	var out []metricsport.Scope
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT upper(season_code), season_year, commune_code FROM `+table+`
		  WHERE recorded_at < $1 AND season_year >= $2 ORDER BY 1, 2, 3`, metricsport.Cutoff(d).UTC(), d.Year-metricSeasonHistory)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var code, commune string
			var year int
			if err := rows.Scan(&code, &year, &commune); err != nil {
				return err
			}
			dim := metricsport.SeasonDimension(code, year)
			if !seen[dim] {
				seen[dim] = true
				out = append(out, metricsport.Scope{TenantID: tid, Dimension: dim})
			}
			out = append(out, metricsport.Scope{TenantID: tid, Dimension: dim, Units: []string{commune}})
		}
		return rows.Err()
	})
	return out, err
}
