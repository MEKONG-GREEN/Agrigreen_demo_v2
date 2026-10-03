package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Backlog là tồn đọng outbox của một cụm.
type Backlog struct {
	Unpublished int64         // chưa phát và chưa bị gác (publisher còn thử)
	OldestAge   time.Duration // 0 khi không tồn đọng
	Parked      int64         // bị gác sau ParkAfterRejections lần broker từ chối — cần người xử lý
}

// ReadBacklog đếm sự kiện chưa phát. pool phải là vai ag_outbox_publisher
// (BYPASSRLS) — vai ứng dụng chỉ thấy tenant của mình.
func ReadBacklog(ctx context.Context, pool *pgxpool.Pool) (Backlog, error) {
	var b Backlog
	var age float64
	err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE parked_at IS NULL),
	    coalesce(extract(epoch FROM now() - min(recorded_at) FILTER (WHERE parked_at IS NULL)), 0)::float8,
	    count(*) FILTER (WHERE parked_at IS NOT NULL)
	  FROM shared.outbox WHERE published_at IS NULL`).Scan(&b.Unpublished, &age, &b.Parked)
	b.OldestAge = time.Duration(age * float64(time.Second))
	return b, err
}

// RegisterBacklogMetrics đăng ký gauge ag.outbox.backlog và ag.outbox.oldest_age
// theo cluster (mục 19). No-op khi OTel tắt.
func RegisterBacklogMetrics(pools map[string]*pgxpool.Pool) (metric.Registration, error) {
	m := telemetry.Meter()
	backlog, err := m.Int64ObservableGauge(telemetry.MetricOutboxBacklog, metric.WithDescription("Số sự kiện outbox chưa phát"))
	if err != nil {
		return nil, err
	}
	age, err := m.Float64ObservableGauge(telemetry.MetricOutboxAge, metric.WithUnit("s"), metric.WithDescription("Tuổi sự kiện outbox chưa phát lâu nhất"))
	if err != nil {
		return nil, err
	}
	parked, err := m.Int64ObservableGauge(telemetry.MetricOutboxParked, metric.WithDescription("Số sự kiện outbox bị gác (broker từ chối lặp lại)"))
	if err != nil {
		return nil, err
	}
	return m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		var errs []error
		for cluster, pool := range pools {
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			b, err := ReadBacklog(qctx, pool)
			cancel()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			attrs := metric.WithAttributes(attribute.String("cluster", cluster))
			o.ObserveInt64(backlog, b.Unpublished, attrs)
			o.ObserveFloat64(age, b.OldestAge.Seconds(), attrs)
			o.ObserveInt64(parked, b.Parked, attrs)
		}
		return errors.Join(errs...)
	}, backlog, age, parked)
}
