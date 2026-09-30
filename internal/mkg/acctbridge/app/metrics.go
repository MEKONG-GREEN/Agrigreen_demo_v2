package app

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Metric của mkg/acctbridge (OTLP → Collector → Prometheus: dấu chấm → gạch dưới, counter thêm _total).
// Luật cảnh báo: deploy/observability/prometheus/agrigreen-mkg-acctbridge.rules.yml (R-62).
const (
	// MetricPushAttempts: lần gọi bộ điều hợp, theo adapter + outcome (succeeded | unavailable | rejected).
	MetricPushAttempts = "ag.acctbridge.push.attempts"
	// MetricPushDeadLettered: bút toán vào DLQ của module (push_failed), theo reason (rejected | retry_exhausted).
	MetricPushDeadLettered = "ag.acctbridge.push.dead_lettered"
	// MetricJournalsPushed: bút toán tổng hợp đã được phần mềm kế toán nhận.
	MetricJournalsPushed = "ag.acctbridge.journals.pushed"
	// MetricBuildFailed: job dựng bút toán kết thúc lỗi, theo code (mapping_missing, source_mismatch…).
	MetricBuildFailed = "ag.acctbridge.build.failed"
)

// Metrics là bộ công cụ đo của module. Số đếm tăng trong handler trước khi giao dịch commit: giao dịch
// hỏng thì hàng đợi giao lại và có thể đếm hai lần — chấp nhận cho cảnh báo (không dùng để đối soát).
type Metrics struct {
	attempts     metric.Int64Counter
	deadLettered metric.Int64Counter
	pushed       metric.Int64Counter
	buildFailed  metric.Int64Counter

	mu     sync.Mutex
	counts map[string]int64 // bản sao trong tiến trình cho kiểm thử
}

// NewMetrics tạo công cụ đo trên meter (nil → noop).
func NewMetrics(m metric.Meter) *Metrics {
	if m == nil {
		m = noop.NewMeterProvider().Meter("acctbridge")
	}
	x := &Metrics{counts: map[string]int64{}}
	x.attempts, _ = m.Int64Counter(MetricPushAttempts, metric.WithDescription("Lần đẩy bút toán tổng hợp tới phần mềm kế toán"))
	x.deadLettered, _ = m.Int64Counter(MetricPushDeadLettered, metric.WithDescription("Bút toán vào DLQ (push_failed) — cần vận hành xử lý (R-62)"))
	x.pushed, _ = m.Int64Counter(MetricJournalsPushed, metric.WithDescription("Bút toán tổng hợp đã được phần mềm kế toán nhận"))
	x.buildFailed, _ = m.Int64Counter(MetricBuildFailed, metric.WithDescription("Job dựng bút toán kết thúc lỗi"))
	return x
}

func (m *Metrics) bump(key string) {
	m.mu.Lock()
	m.counts[key]++
	m.mu.Unlock()
}

// Count trả số đếm trong tiến trình (kiểm thử): vd "dead_lettered:rejected", "attempts:unavailable".
func (m *Metrics) Count(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key]
}

func (m *Metrics) attempt(ctx context.Context, adapter, outcome string) {
	m.attempts.Add(ctx, 1, metric.WithAttributes(attribute.String("adapter", adapter), attribute.String("outcome", outcome)))
	m.bump("attempts:" + outcome)
}

func (m *Metrics) dead(ctx context.Context, reason string) {
	m.deadLettered.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	m.bump("dead_lettered:" + reason)
}

func (m *Metrics) journalPushed(ctx context.Context) {
	m.pushed.Add(ctx, 1)
	m.bump("pushed")
}

func (m *Metrics) buildFail(ctx context.Context, code string) {
	m.buildFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("code", code)))
	m.bump("build_failed:" + code)
}
