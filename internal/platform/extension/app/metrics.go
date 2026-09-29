package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Chỉ số KN US-DASH-01 có lịch sử (pf/metrics, internal/platform/metrics/defs/kn.*.yaml).
const (
	MetricSownAreaHa       metricsport.Key = "kn.sown_area_ha"
	MetricCoopsReported    metricsport.Key = "kn.cooperatives_reported"
	MetricOpenTasks        metricsport.Key = "kn.open_tasks"
	MetricDirectiveAckRate metricsport.Key = "kn.directive_ack_rate_pct"
)

// MetricsSource thực hiện metrics/port.Source (+ ScopeLister) trên SỔ của module
// (không trên bảng chiếu hiện tại): giá trị ngày D chỉ dùng dòng có recorded_at <
// Cutoff(D), nên bản ghi bù ghi nhận sau Cutoff không đổi giá trị của D (KN
// US-DASH-02: kỳ đã chốt không bị sửa). Đọc bằng giao dịch chỉ đọc của vai
// extension_app (R-05).
type MetricsSource struct{ Svc *Service }

// AsOf thực hiện metrics/port.Source.
func (m MetricsSource) AsOf(ctx context.Context, key metricsport.Key, d clock.Date, sc metricsport.Scope) (metricsport.Value, error) {
	if m.Svc.Clock.Now().Before(metricsport.Cutoff(d)) {
		return metricsport.Value{}, metricsport.ErrNotClosed
	}
	return m.AsOfAt(ctx, key, d, metricsport.Cutoff(d), sc)
}

func unitsArg(sc metricsport.Scope) []string {
	if len(sc.Units) == 0 {
		return nil
	}
	return sc.Units
}

// metricPeriod trả kỳ tháng "YYYY-MM" chứa d và [đầu kỳ, đầu kỳ sau) giờ VN.
func metricPeriod(d clock.Date) (string, time.Time, time.Time) {
	start := time.Date(d.Year, d.Month, 1, 0, 0, 0, 0, clock.Zone())
	return start.Format("2006-01"), start.UTC(), start.AddDate(0, 1, 0).UTC()
}

// AsOfAt tính với mốc recorded_at tường minh (kiểm thử "bản ghi ghi nhận muộn bị loại").
func (m MetricsSource) AsOfAt(ctx context.Context, key metricsport.Key, d clock.Date, cutoff time.Time, sc metricsport.Scope) (metricsport.Value, error) {
	v := metricsport.Value{Key: key, BusinessDate: d, Scope: sc, Cutoff: cutoff}
	if def, ok := metricsport.DefinitionFrom(ctx); ok {
		v.DefinitionVersion, v.Unit = def.Version, def.Unit
	}
	period, pStart, pEnd := metricPeriod(d)
	units := unitsArg(sc)
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, sc.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var wmID *uuid.UUID
		var wmAt *time.Time
		switch key {
		case MetricSownAreaHa, MetricCoopsReported:
			var sum string
			var coops int64
			if err := tx.QueryRow(ctx, `WITH cur AS (
			    SELECT DISTINCT ON (stream_key) id, recorded_at, report_kind, period, sown_area_ha, cooperative_ref, province_code, commune_code
			    FROM pf_extension.field_report WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
			  SELECT coalesce(sum(sown_area_ha), 0)::text, count(DISTINCT cooperative_ref),
			         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
			  FROM cur WHERE report_kind = 'sowing_progress' AND period = $2
			    AND ($3::text[] IS NULL OR province_code = ANY($3) OR commune_code = ANY($3))`,
				cutoff.UTC(), period, units).Scan(&sum, &coops, &wmID, &wmAt); err != nil {
				return err
			}
			if key == MetricCoopsReported {
				v.Value = decimal.NewFromInt(coops)
			} else {
				var err error
				if v.Value, err = decimal.NewFromString(sum); err != nil {
					return err
				}
			}
		case MetricOpenTasks:
			var open int64
			if err := tx.QueryRow(ctx, `WITH last AS (
			    SELECT DISTINCT ON (task_id) task_id, id, recorded_at, status_after
			    FROM pf_extension.task_event WHERE recorded_at < $1 ORDER BY task_id, seq DESC)
			  SELECT count(*) FILTER (WHERE l.status_after <> 'resolved'),
			         (array_agg(l.id ORDER BY l.recorded_at DESC, l.id DESC))[1], max(l.recorded_at)
			  FROM last l JOIN pf_extension.task_current t ON t.task_id = l.task_id
			  WHERE ($2::text[] IS NULL OR t.province_code = ANY($2) OR t.commune_code = ANY($2))`,
				cutoff.UTC(), units).Scan(&open, &wmID, &wmAt); err != nil {
				return err
			}
			v.Value = decimal.NewFromInt(open)
		case MetricDirectiveAckRate:
			var targets, acks int64
			if err := tx.QueryRow(ctx, `SELECT count(*), count(a.actor_ref), greatest(max(d.recorded_at), max(a.recorded_at))
			  FROM pf_extension.directive_target dt
			  JOIN pf_extension.directive d ON d.tenant_id = dt.tenant_id AND d.directive_id = dt.directive_id
			  LEFT JOIN pf_extension.directive_ack a ON a.tenant_id = dt.tenant_id AND a.directive_id = dt.directive_id
			       AND a.actor_ref = dt.actor_ref AND a.recorded_at < $1
			  WHERE d.recorded_at < $1 AND d.issued_at >= $2 AND d.issued_at < $3
			    AND ($4::text[] IS NULL OR dt.province_code = ANY($4) OR dt.unit_code = ANY($4))`,
				cutoff.UTC(), pStart, pEnd, units).Scan(&targets, &acks, &wmAt); err != nil {
				return err
			}
			v.Value = decimal.Zero
			if targets > 0 {
				v.Value = decimal.NewFromInt(acks * 100).Div(decimal.NewFromInt(targets)).Round(2)
			}
		default:
			return metricsport.ErrUnknownMetric
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

// Scopes thực hiện metrics/port.ScopeLister: toàn tenant + từng tỉnh có dữ liệu.
func (m MetricsSource) Scopes(ctx context.Context, _ metricsport.Key, d clock.Date, tid uuid.UUID) ([]metricsport.Scope, error) {
	out := []metricsport.Scope{{TenantID: tid}}
	cutoff := metricsport.Cutoff(d)
	err := tenant.WithTenantTxOptions(ctx, m.Svc.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT province_code FROM pf_extension.field_report WHERE recorded_at < $1
		  UNION SELECT province_code FROM pf_extension.task_current WHERE created_at < $1
		  UNION SELECT province_code FROM pf_extension.directive_target WHERE issued_at < $1
		  ORDER BY 1`, cutoff.UTC())
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
