package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Chỉ số CGH theo vụ (FN-09/10/11: kết quả cân đối đã lưu, xu hướng nhiều năm).
const (
	MetricCoopSufficiencyPct metricsport.Key = "cgh.coop_sufficiency_pct"
	MetricCoopsShortage      metricsport.Key = "cgh.coops_shortage"
)

// MetricsSource thực hiện metrics/port.Source (+ ScopeLister): lần cân đối mới nhất
// của vụ (Scope.Dimension) có recorded_at < Cutoff(D); mức đáp ứng tổng thể của mỗi
// HTX theo domain.Summarize — cùng quy tắc với dashboard FN-11. Không tính lại cân đối.
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
	if key != MetricCoopSufficiencyPct && key != MetricCoopsShortage {
		return v, metricsport.ErrUnknownMetric
	}
	code, year, ok := metricsport.ParseSeason(sc.Dimension)
	if !ok {
		return v, metricsport.ErrNoData
	}
	season := domain.Season{Code: code, Year: year}
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, sc.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var runID uuid.UUID
		var recorded time.Time
		err := tx.QueryRow(ctx, `SELECT id, recorded_at FROM pf_mechanization.balance_run
		  WHERE stream_key = $1 AND recorded_at < $2 ORDER BY seq DESC LIMIT 1`, domain.RunStreamKey(season), cutoff.UTC()).Scan(&runID, &recorded)
		if errors.Is(err, pgx.ErrNoRows) {
			return metricsport.ErrNoData
		}
		if err != nil {
			return err
		}
		run, err := store.RunByID(ctx, tx, runID)
		if err != nil {
			return err
		}
		if run == nil {
			return metricsport.ErrNoData
		}
		res, err := store.Results(ctx, tx, store.ResultFilter{RunID: runID, All: len(sc.Units) == 0, Units: sc.Units})
		if err != nil {
			return err
		}
		byCoop := map[uuid.UUID][]domain.ResultRow{}
		for _, r := range res {
			byCoop[r.CooperativeRef] = append(byCoop[r.CooperativeRef], r.Row())
		}
		var sufficient, shortage int64
		for _, rows := range byCoop {
			g, _ := domain.Summarize(rows, run.Thresholds)
			switch g.Status {
			case domain.CoverageSufficient, domain.CoverageSurplus:
				sufficient++
			case domain.CoverageShortage:
				shortage++
			}
		}
		switch {
		case key == MetricCoopsShortage:
			v.Value = decimal.NewFromInt(shortage)
		case len(byCoop) == 0:
			v.Value = decimal.Zero
		default:
			v.Value = decimal.NewFromInt(sufficient * 100).Div(decimal.NewFromInt(int64(len(byCoop)))).Round(2)
		}
		v.Watermark = metricsport.Watermark{LastEventID: runID, RecordedAt: recorded.UTC()}
		return nil
	})
	return v, err
}

// Scopes thực hiện metrics/port.ScopeLister: mỗi vụ có lần cân đối trước Cutoff(D),
// toàn tenant và từng tỉnh có kết quả.
func (m MetricsSource) Scopes(ctx context.Context, _ metricsport.Key, d clock.Date, tid uuid.UUID) ([]metricsport.Scope, error) {
	var out []metricsport.Scope
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT r.season_code, r.season_year, coalesce(x.province_code, '')
		  FROM pf_mechanization.balance_run r
		  LEFT JOIN pf_mechanization.balance_result x ON x.tenant_id = r.tenant_id AND x.run_id = r.id AND x.province_code IS NOT NULL
		  WHERE r.recorded_at < $1 ORDER BY 1, 2, 3`, metricsport.Cutoff(d).UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var code, prov string
			var year int
			if err := rows.Scan(&code, &year, &prov); err != nil {
				return err
			}
			dim := metricsport.SeasonDimension(code, year)
			if !seen[dim] {
				seen[dim] = true
				out = append(out, metricsport.Scope{TenantID: tid, Dimension: dim})
			}
			if prov != "" {
				out = append(out, metricsport.Scope{TenantID: tid, Dimension: dim, Units: []string{prov}})
			}
		}
		return rows.Err()
	})
	return out, err
}
