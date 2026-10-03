package app

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Metric của pf/metering (OTLP → Collector → Prometheus: dấu chấm → gạch dưới, counter thêm _total).
// Nhãn cardinality thấp (R-91): kênh, quyết định, lý do, lớp trạng thái — không tenant, không khách hàng.
// Luật cảnh báo: deploy/observability/prometheus/agrigreen-egress.rules.yml.
const (
	MetricEgressRecords          = "ag.egress.records"           // {channel, decision} phần tử qua điểm kiểm soát
	MetricEgressBytes            = "ag.egress.bytes"             // {channel}
	MetricEgressDenied           = "ag.egress.denied"            // {channel, reason} lần bị từ chối (enforce)
	MetricEgressCheckpointErrors = "ag.egress.checkpoint_errors" // {channel} checkpoint lỗi hạ tầng
	MetricEgressAnomalies        = "ag.egress.anomalies"         // {channel} lần ĐẦU trong ngày vượt ngưỡng
	MetricPublicCalls            = "ag.public.calls"             // {status_class} dòng sổ sử dụng đã ghi
	MetricPublicUnits            = "ag.public.units"             // đơn vị tính phí đã ghi
	MetricPublicQuotaExceeded    = "ag.public.quota_exceeded"    // lời gọi bị từ chối vì hạn mức gói
	MetricPublicLedgerErrors     = "ag.public.ledger_errors"     // ghi sổ sử dụng lỗi → 503 metering.unavailable
)

// Metrics là bộ đo của module. Tăng SAU khi giao dịch commit (trừ số đếm lỗi).
type Metrics struct {
	egressRecords, egressBytes, egressDenied, egressErrors, egressAnomalies metric.Int64Counter
	calls, units, quotaExceeded, ledgerErrors                               metric.Int64Counter

	mu     sync.Mutex
	counts map[string]int64 // bản sao trong tiến trình cho kiểm thử
}

// NewMetrics tạo bộ đo trên meter (nil → noop).
func NewMetrics(m metric.Meter) *Metrics {
	if m == nil {
		m = noop.NewMeterProvider().Meter("metering")
	}
	x := &Metrics{counts: map[string]int64{}}
	x.egressRecords, _ = m.Int64Counter(MetricEgressRecords, metric.WithDescription("Phần tử dữ liệu qua điểm kiểm soát xuất"))
	x.egressBytes, _ = m.Int64Counter(MetricEgressBytes, metric.WithDescription("Byte dữ liệu qua điểm kiểm soát xuất"))
	x.egressDenied, _ = m.Int64Counter(MetricEgressDenied, metric.WithDescription("Lần xuất bị điểm kiểm soát từ chối (enforce)"))
	x.egressErrors, _ = m.Int64Counter(MetricEgressCheckpointErrors, metric.WithDescription("Điểm kiểm soát xuất lỗi hạ tầng"))
	x.egressAnomalies, _ = m.Int64Counter(MetricEgressAnomalies, metric.WithDescription("Lần đầu trong ngày khối lượng xuất vượt ngưỡng bất thường"))
	x.calls, _ = m.Int64Counter(MetricPublicCalls, metric.WithDescription("Lời gọi Public API đã ghi sổ sử dụng"))
	x.units, _ = m.Int64Counter(MetricPublicUnits, metric.WithDescription("Đơn vị tính phí đã ghi sổ (metering.units.v1)"))
	x.quotaExceeded, _ = m.Int64Counter(MetricPublicQuotaExceeded, metric.WithDescription("Lời gọi Public API vượt hạn mức gói"))
	x.ledgerErrors, _ = m.Int64Counter(MetricPublicLedgerErrors, metric.WithDescription("Ghi sổ sử dụng lỗi — không trả dữ liệu (503)"))
	return x
}

func (m *Metrics) bump(key string, n int64) {
	m.mu.Lock()
	m.counts[key] += n
	m.mu.Unlock()
}

// Count trả số đếm trong tiến trình (kiểm thử), vd "egress_anomalies:valve", "ledger_errors".
func (m *Metrics) Count(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key]
}

func (m *Metrics) egress(ctx context.Context, channel string, allowed bool, reason string, records, bytes int64, firstAnomaly bool) {
	decision := "allowed"
	if !allowed {
		decision = "denied"
	}
	m.egressRecords.Add(ctx, records, metric.WithAttributes(attribute.String("channel", channel), attribute.String("decision", decision)))
	m.egressBytes.Add(ctx, bytes, metric.WithAttributes(attribute.String("channel", channel)))
	m.bump("egress_records:"+channel+":"+decision, records)
	if !allowed {
		m.egressDenied.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", channel), attribute.String("reason", reason)))
		m.bump("egress_denied:"+channel, 1)
	}
	if firstAnomaly {
		m.egressAnomalies.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", channel)))
		m.bump("egress_anomalies:"+channel, 1)
	}
}

func (m *Metrics) checkpointError(ctx context.Context, channel string) {
	m.egressErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("channel", channel)))
	m.bump("egress_checkpoint_errors:"+channel, 1)
}

func (m *Metrics) call(ctx context.Context, statusClass string, units int64, quota bool) {
	m.calls.Add(ctx, 1, metric.WithAttributes(attribute.String("status_class", statusClass)))
	m.units.Add(ctx, units)
	m.bump("calls:"+statusClass, 1)
	m.bump("units", units)
	if quota {
		m.quotaExceeded.Add(ctx, 1)
		m.bump("quota_exceeded", 1)
	}
}

func (m *Metrics) ledgerError(ctx context.Context) {
	m.ledgerErrors.Add(ctx, 1)
	m.bump("ledger_errors", 1)
}
