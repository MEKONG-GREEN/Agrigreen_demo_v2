package telemetry

import (
	"context"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Tên metric dùng chung. Chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19) —
// xem docs/runbooks/README.md cho ngưỡng cảnh báo đề xuất.
const (
	MetricHTTPDuration  = "http.server.request.duration" // giây, theo semconv HTTP
	MetricOutboxBacklog = "ag.outbox.backlog"            // số sự kiện chưa phát, theo cluster
	MetricOutboxAge     = "ag.outbox.oldest_age"         // giây, tuổi sự kiện chưa phát lâu nhất
	MetricOutboxParked  = "ag.outbox.parked"             // số sự kiện bị gác (broker từ chối lặp lại), > 0 → cảnh báo
	MetricDLQDepth      = "ag.rabbitmq.dlq.depth"        // số thông điệp trong DLQ, theo queue (R-62)
	MetricProjectionLag = "ag.projection.lag"            // giây, độ trễ bảng chiếu so với sổ (R-21)
	MetricJobRuns       = "ag.job.runs"                  // số lần chạy job vận hành, theo job + outcome
	// MetricStandbyLag: giây, độ trễ áp WAL của bản sao chờ theo cluster (ADR-0006:
	// standby_replication_lag_seconds có cảnh báo) — db.RegisterStandbyLag.
	MetricStandbyLag = "ag.db.standby_replication_lag"
)

type httpInstruments struct {
	duration metric.Float64Histogram
}

var (
	instMu  sync.Mutex
	httpIns *httpInstruments
)

func resetInstruments() {
	instMu.Lock()
	httpIns = nil
	jobRuns = nil
	instMu.Unlock()
}

func httpInst() *httpInstruments {
	instMu.Lock()
	defer instMu.Unlock()
	if httpIns == nil {
		h, _ := Meter().Float64Histogram(MetricHTTPDuration,
			metric.WithUnit("s"), metric.WithDescription("Thời gian xử lý request HTTP"),
			metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10))
		httpIns = &httpInstruments{duration: h}
	}
	return httpIns
}

// RecordHTTP ghi thời gian một request. route là mẫu route (vd /v1/consents/:id),
// KHÔNG phải đường dẫn thật — giữ cardinality thấp và không lộ định danh.
func RecordHTTP(ctx context.Context, method, route string, status int, d time.Duration) {
	if !Enabled() {
		return
	}
	httpInst().duration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("http.request.method", method),
		attribute.String("http.route", route),
		attribute.String("http.response.status_code", strconv.Itoa(status)),
	))
}

var jobRuns metric.Int64Counter

// RecordJobRun đếm một lần chạy job vận hành (neo chuỗi, dọn idempotency, ...).
func RecordJobRun(ctx context.Context, job, outcome string) {
	if !Enabled() {
		return
	}
	instMu.Lock()
	if jobRuns == nil {
		jobRuns, _ = Meter().Int64Counter(MetricJobRuns, metric.WithDescription("Số lần chạy job vận hành"))
	}
	c := jobRuns
	instMu.Unlock()
	c.Add(ctx, 1, metric.WithAttributes(attribute.String("job", job), attribute.String("outcome", outcome)))
}

// LagSource trả độ trễ của từng bảng chiếu so với sổ nguồn, tính bằng giây,
// khóa là tên bảng chiếu (vd "mkg_measurement.weigh_ticket_current").
type LagSource func(ctx context.Context) (map[string]float64, error)

// RegisterProjectionLag đăng ký nguồn độ trễ bảng chiếu (mục 19: chỉ số bắt
// buộc có cảnh báo). Module có bảng chiếu `*_current` (G2: measurement,
// costing) gọi hàm này lúc khởi động. No-op khi OTel tắt.
func RegisterProjectionLag(src LagSource) (metric.Registration, error) {
	g, err := Meter().Float64ObservableGauge(MetricProjectionLag, metric.WithUnit("s"),
		metric.WithDescription("Độ trễ bảng chiếu so với sổ nguồn"))
	if err != nil {
		return nil, err
	}
	return Meter().RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		m, err := src(ctx)
		if err != nil {
			return err
		}
		for name, v := range m {
			o.ObserveFloat64(g, v, metric.WithAttributes(attribute.String("projection", name)))
		}
		return nil
	}, g)
}
