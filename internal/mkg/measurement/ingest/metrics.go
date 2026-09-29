package ingest

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Tên metric của cmd/ingest (OTLP, ADR-0013; mục 19). Nhãn giữ cardinality thấp:
// KHÔNG có device_id/tenant_id trong nhãn — tra thiết bị qua log có cấu trúc.
const (
	MetricBatches          = "ag.ingest.batches"              // lô nhận, theo outcome (accepted|duplicate|rejected) + code
	MetricReadings         = "ag.ingest.readings"             // số đo ghi mới
	MetricClockSkew        = "ag.ingest.clock_skew"           // ms, theo kind (ntp_offset|device_vs_received)
	MetricClockSkewFlagged = "ag.ingest.clock_skew.flagged"   // lô có độ lệch vượt ngưỡng, theo reason
	MetricCounterGaps      = "ag.ingest.counter_gaps"         // số bộ đếm thiếu giữa các lô (mất lô)
	MetricIntegrations     = "ag.ingest.integrations"         // lần tích phân phiên cân, theo outcome + code
	MetricIntegrationLag   = "ag.ingest.integration.lag"      // giây, từ lúc phiên cân đóng tới lúc ghi phiếu
	MetricIntegrationStuck = "ag.ingest.integration.stuck"    // báo động: phiên kẹt quá ngưỡng (thiếu lô, không ổn định…)
	MetricPendingOldest    = "ag.ingest.integration.oldest_s" // giây, tuổi phiên chờ lâu nhất ở lượt gần nhất
)

// Metrics là bộ công cụ đo của ingest.
type Metrics struct {
	batches      metric.Int64Counter
	readings     metric.Int64Counter
	skew         metric.Int64Histogram
	skewFlagged  metric.Int64Counter
	gaps         metric.Int64Counter
	integrations metric.Int64Counter
	lag          metric.Int64Histogram
	stuck        metric.Int64Counter
	oldest       metric.Int64Gauge
}

// NewMetrics tạo công cụ đo trên meter (nil → noop).
func NewMetrics(m metric.Meter) *Metrics {
	if m == nil {
		m = noop.NewMeterProvider().Meter("ingest")
	}
	x := &Metrics{}
	x.batches, _ = m.Int64Counter(MetricBatches, metric.WithDescription("Lô số đo nhận từ agent biên"))
	x.readings, _ = m.Int64Counter(MetricReadings, metric.WithDescription("Số đo thô ghi mới vào ts_weigh"))
	x.skew, _ = m.Int64Histogram(MetricClockSkew, metric.WithUnit("ms"),
		metric.WithDescription("Độ lệch đồng hồ thiết bị, trị tuyệt đối (chỉ giám sát, không dùng làm thứ tự — ADR-0019)"))
	x.skewFlagged, _ = m.Int64Counter(MetricClockSkewFlagged, metric.WithDescription("Lô có độ lệch đồng hồ vượt ngưỡng"))
	x.gaps, _ = m.Int64Counter(MetricCounterGaps, metric.WithDescription("Bộ đếm bị thiếu giữa các lô (mất lô)"))
	x.integrations, _ = m.Int64Counter(MetricIntegrations, metric.WithDescription("Lần tích phân phiên cân thành phiếu"))
	x.lag, _ = m.Int64Histogram(MetricIntegrationLag, metric.WithUnit("s"),
		metric.WithDescription("Độ trễ tích phân: phiên cân đóng → phiếu cân ghi"))
	x.stuck, _ = m.Int64Counter(MetricIntegrationStuck, metric.WithDescription("Phiên cân kẹt quá ngưỡng — cần người xử lý"))
	x.oldest, _ = m.Int64Gauge(MetricPendingOldest, metric.WithUnit("s"), metric.WithDescription("Tuổi phiên cân chờ tích phân lâu nhất"))
	return x
}

func (m *Metrics) batch(ctx context.Context, outcome, code string) {
	m.batches.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome), attribute.String("code", code)))
}

// skewMs ghi độ lệch ms có dấu dưới dạng trị tuyệt đối + nhãn sign (pos|neg).
// kind=ntp_offset: ntp_offset_ms thiết bị báo; kind=device_vs_received:
// received_at − device_time cuối lô (dương gồm cả thời gian đệm tại thiết bị).
func (m *Metrics) skewMs(ctx context.Context, kind string, ms int64) {
	sign := "pos"
	if ms < 0 {
		sign, ms = "neg", -ms
	}
	m.skew.Record(ctx, ms, metric.WithAttributes(attribute.String("kind", kind), attribute.String("sign", sign)))
}

func (m *Metrics) flagSkew(ctx context.Context, reason string) {
	m.skewFlagged.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

func (m *Metrics) integration(ctx context.Context, outcome, code string) {
	m.integrations.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome), attribute.String("code", code)))
}

func (m *Metrics) integrationLag(ctx context.Context, d time.Duration) {
	m.lag.Record(ctx, int64(d/time.Second))
}
