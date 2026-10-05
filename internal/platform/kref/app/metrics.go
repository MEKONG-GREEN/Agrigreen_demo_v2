package app

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

var (
	lagOnce sync.Once
	lagHist metric.Float64Histogram
)

// recordLag ghi độ trễ k_ref của một sự kiện (telemetry.MetricKRefLag, theo quan hệ). No-op khi OTel tắt.
func recordLag(ctx context.Context, relation string, d time.Duration) {
	if !telemetry.Enabled() {
		return
	}
	lagOnce.Do(func() {
		lagHist, _ = telemetry.Meter().Float64Histogram(telemetry.MetricKRefLag, metric.WithUnit("s"),
			metric.WithDescription("Độ trễ k_ref: từ lúc sự kiện nguồn được ghi tới lúc bản chiếu phản ánh (ADR-0029 §4)"),
			metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 30, 60, 300))
	})
	if lagHist == nil {
		return
	}
	if d < 0 {
		d = 0
	}
	lagHist.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("relation", relation)))
}
