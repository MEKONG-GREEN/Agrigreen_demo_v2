package httpx_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

const incoming = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func engine() *gin.Engine {
	r := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	r.GET("/v1/things/:id", func(c *gin.Context) {
		c.JSON(200, gin.H{"trace_id": telemetry.TraceID(c.Request.Context())})
	})
	r.GET("/v1/fail", func(c *gin.Context) { httpx.Fail(c, problem.NotFound) })
	return r
}

func do(r http.Handler, path, traceparent string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if traceparent != "" {
		req.Header.Set("traceparent", traceparent)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// OTel tắt: hành vi G0 giữ nguyên.
func TestTraceDisabledKeepsG0Behaviour(t *testing.T) {
	telemetry.Disable()
	r := engine()
	w := do(r, "/v1/things/1", incoming)
	if got := w.Header().Get(httpx.TraceHeader); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("X-Trace-Id = %q", got)
	}
	w = do(r, "/v1/things/1", "")
	if len(w.Header().Get(httpx.TraceHeader)) != 32 {
		t.Fatal("không có traceparent thì phải sinh trace_id")
	}
}

func TestTraceWithOTel(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	telemetry.UseProviders(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)), sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(telemetry.Disable)
	r := engine()

	w := do(r, "/v1/things/0909123456", incoming)
	var body struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := "4bf92f3577b34da6a3ce929d0e0e4736"
	if w.Header().Get(httpx.TraceHeader) != want || body.TraceID != want {
		t.Fatalf("trace_id phải theo span con của traceparent: header=%q ctx=%q", w.Header().Get(httpx.TraceHeader), body.TraceID)
	}
	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Parent.SpanID().String() != "00f067aa0ba902b7" || spans[0].Name != "GET /v1/things/:id" {
		t.Fatalf("span máy chủ sai: %+v", spans)
	}
	for _, a := range spans[0].Attributes {
		if a.Value.String() == "/v1/things/0909123456" {
			t.Fatal("span không được chứa đường dẫn thật (có thể có định danh)")
		}
	}

	// problem+json mang cùng trace_id với header.
	w = do(r, "/v1/fail", "")
	var p struct {
		TraceID string `json:"trace_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.TraceID == "" || p.TraceID != w.Header().Get(httpx.TraceHeader) {
		t.Fatalf("problem trace_id %q ≠ header %q", p.TraceID, w.Header().Get(httpx.TraceHeader))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == telemetry.MetricHTTPDuration {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("thiếu metric %s", telemetry.MetricHTTPDuration)
	}
}
