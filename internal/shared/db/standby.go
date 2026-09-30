package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// EnvStandbyURL là DSN tới bản sao chờ (standby) của một cụm — tùy chọn. Tên kết thúc
// bằng _URL nên residency.Check (RB-7) kiểm đích ở production.
const EnvStandbyURL = "AG_DB_STANDBY_URL"

// StandbyLag đo độ trễ áp WAL của máy chủ đang nối (ADR-0006: chỉ số nghiệp vụ
// standby_replication_lag_seconds có cảnh báo). standby = false khi máy chủ là primary
// (không đo). Trên standby: 0 khi đã áp hết WAL đã nhận, ngược lại là số giây từ giao
// dịch cuối đã áp. Hàm chỉ đọc hàm hệ thống, không cần quyền đặc biệt.
func StandbyLag(ctx context.Context, pool *pgxpool.Pool) (seconds float64, standby bool, err error) {
	err = pool.QueryRow(ctx, `SELECT pg_is_in_recovery(),
	  CASE WHEN NOT pg_is_in_recovery() THEN 0
	       WHEN pg_last_wal_receive_lsn() IS NOT DISTINCT FROM pg_last_wal_replay_lsn() THEN 0
	       ELSE COALESCE(EXTRACT(EPOCH FROM now() - pg_last_xact_replay_timestamp()), 0)::float8 END`).Scan(&standby, &seconds)
	return seconds, standby, err
}

// RegisterStandbyLag đăng ký gauge telemetry.MetricStandbyLag (giây, theo cluster) đọc từ
// pool nối tới standby. Máy chủ không phải standby → không ghi điểm đo (cảnh báo "thiếu
// số đo" ở hệ quan trắc sẽ bắt cấu hình sai).
func RegisterStandbyLag(pool *pgxpool.Pool, cluster string) (metric.Registration, error) {
	m := telemetry.Meter()
	g, err := m.Float64ObservableGauge(telemetry.MetricStandbyLag, metric.WithUnit("s"),
		metric.WithDescription("Độ trễ áp WAL của bản sao chờ (ADR-0006)"))
	if err != nil {
		return nil, err
	}
	return m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		lag, standby, err := StandbyLag(ctx, pool)
		if err != nil || !standby {
			return err
		}
		o.ObserveFloat64(g, lag, metric.WithAttributes(attribute.String("cluster", cluster)))
		return nil
	}, g)
}
