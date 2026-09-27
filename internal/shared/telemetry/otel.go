package telemetry

// OpenTelemetry (R-90, ADR-0013): trace + metric xuất OTLP tới bộ thu tự vận
// hành trong lãnh thổ (Tempo/Prometheus qua OpenTelemetry Collector — RB-7).
// Log vẫn là JSON qua slog ra stdout; bộ gom log (Loki) nối log với trace
// bằng trường trace_id.
//
// AG_OTLP_ENDPOINT rỗng → TẮT: không cài provider, không propagator, hành vi
// y như G0 (chỉ slog + trace_id tự sinh).
//
// Không đưa dữ liệu cá nhân vào thuộc tính span/metric (R-14): chỉ route mẫu,
// mã trạng thái, tên thao tác SQL, mã tham chiếu. Không bao giờ ghi tham số SQL.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// InstrumentationName là tên thư viện đo của repo.
const InstrumentationName = "github.com/MEKONG-GREEN/Agrigreen_demo_v2"

var enabled atomic.Bool

// Enabled cho biết OpenTelemetry đang bật (Setup có endpoint, hoặc UseProviders).
func Enabled() bool { return enabled.Load() }

// Config là cấu hình xuất OTLP.
type Config struct {
	Service     string
	Version     string
	Environment string
	// Endpoint: host:port, hoặc URL đầy đủ (http://otel-collector:4318). Rỗng = tắt.
	Endpoint string
	// Protocol: "grpc" (mặc định) hoặc "http" (http/protobuf).
	Protocol string
	// Insecure: không TLS (chỉ trong cụm có mTLS ở tầng mesh, hoặc Local).
	Insecure bool
	// SampleRatio trong [0,1] (ConfigFromEnv mặc định 1). Span con theo quyết định của span cha.
	SampleRatio float64
	// MetricInterval là chu kỳ đẩy metric, mặc định 15 giây.
	MetricInterval time.Duration
}

// ConfigFromEnv đọc AG_OTLP_ENDPOINT, AG_OTLP_PROTOCOL, AG_OTLP_INSECURE,
// AG_OTLP_SAMPLE_RATIO, AG_ENV, AG_VERSION.
func ConfigFromEnv(service string) (Config, error) {
	c := Config{
		Service:     service,
		Version:     os.Getenv("AG_VERSION"),
		Environment: os.Getenv("AG_ENV"),
		Endpoint:    strings.TrimSpace(os.Getenv("AG_OTLP_ENDPOINT")),
		Protocol:    strings.ToLower(strings.TrimSpace(os.Getenv("AG_OTLP_PROTOCOL"))),
		SampleRatio: 1,
	}
	if v := os.Getenv("AG_OTLP_INSECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("telemetry: AG_OTLP_INSECURE: %w", err)
		}
		c.Insecure = b
	}
	if v := os.Getenv("AG_OTLP_SAMPLE_RATIO"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			return c, errors.New("telemetry: AG_OTLP_SAMPLE_RATIO phải trong [0,1]")
		}
		c.SampleRatio = f
	}
	return c, nil
}

// Setup bật OpenTelemetry theo biến môi trường. Luôn trả hàm shutdown dùng
// được (no-op khi tắt); gọi nó trước khi thoát để đẩy nốt span/metric.
func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	c, err := ConfigFromEnv(service)
	if err != nil {
		return noopShutdown, err
	}
	return SetupWith(ctx, c)
}

func noopShutdown(context.Context) error { return nil }

// SetupWith như Setup với cấu hình tường minh.
func SetupWith(ctx context.Context, c Config) (func(context.Context) error, error) {
	if c.Endpoint == "" {
		return noopShutdown, nil
	}
	if c.Service == "" {
		return noopShutdown, errors.New("telemetry: thiếu tên service")
	}
	res, err := resource.New(ctx, resource.WithAttributes(resourceAttrs(c)...))
	if err != nil {
		return noopShutdown, err
	}
	te, me, err := exporters(ctx, c)
	if err != nil {
		return noopShutdown, err
	}
	ratio := c.SampleRatio
	interval := c.MetricInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(te),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(interval))),
		sdkmetric.WithResource(res),
	)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Default().Warn("otel lỗi xuất", "event", "telemetry.export", "outcome", "error", "error", err.Error())
	}))
	UseProviders(tp, mp)
	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

func resourceAttrs(c Config) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String("service.name", c.Service), attribute.String("service.namespace", "agrigreen")}
	if c.Version != "" {
		attrs = append(attrs, attribute.String("service.version", c.Version))
	}
	if c.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", c.Environment))
	}
	return attrs
}

func exporters(ctx context.Context, c Config) (sdktrace.SpanExporter, sdkmetric.Exporter, error) {
	isURL := strings.Contains(c.Endpoint, "://")
	switch c.Protocol {
	case "", "grpc":
		var to []otlptracegrpc.Option
		var mo []otlpmetricgrpc.Option
		if isURL {
			to = append(to, otlptracegrpc.WithEndpointURL(c.Endpoint))
			mo = append(mo, otlpmetricgrpc.WithEndpointURL(c.Endpoint))
		} else {
			to = append(to, otlptracegrpc.WithEndpoint(c.Endpoint))
			mo = append(mo, otlpmetricgrpc.WithEndpoint(c.Endpoint))
		}
		if c.Insecure {
			to = append(to, otlptracegrpc.WithInsecure())
			mo = append(mo, otlpmetricgrpc.WithInsecure())
		}
		te, err := otlptracegrpc.New(ctx, to...)
		if err != nil {
			return nil, nil, fmt.Errorf("telemetry: exporter trace gRPC: %w", err)
		}
		me, err := otlpmetricgrpc.New(ctx, mo...)
		if err != nil {
			return nil, nil, fmt.Errorf("telemetry: exporter metric gRPC: %w", err)
		}
		return te, me, nil
	case "http", "http/protobuf":
		var to []otlptracehttp.Option
		var mo []otlpmetrichttp.Option
		if isURL {
			to = append(to, otlptracehttp.WithEndpointURL(strings.TrimRight(c.Endpoint, "/")+"/v1/traces"))
			mo = append(mo, otlpmetrichttp.WithEndpointURL(strings.TrimRight(c.Endpoint, "/")+"/v1/metrics"))
		} else {
			to = append(to, otlptracehttp.WithEndpoint(c.Endpoint))
			mo = append(mo, otlpmetrichttp.WithEndpoint(c.Endpoint))
		}
		if c.Insecure {
			to = append(to, otlptracehttp.WithInsecure())
			mo = append(mo, otlpmetrichttp.WithInsecure())
		}
		te, err := otlptracehttp.New(ctx, to...)
		if err != nil {
			return nil, nil, fmt.Errorf("telemetry: exporter trace HTTP: %w", err)
		}
		me, err := otlpmetrichttp.New(ctx, mo...)
		if err != nil {
			return nil, nil, fmt.Errorf("telemetry: exporter metric HTTP: %w", err)
		}
		return te, me, nil
	}
	return nil, nil, fmt.Errorf("telemetry: AG_OTLP_PROTOCOL %q không hỗ trợ (grpc|http)", c.Protocol)
}

// UseProviders cài provider và propagator W3C traceparent làm toàn cục rồi
// bật Enabled. Dùng bởi SetupWith và kiểm thử (provider trong bộ nhớ).
// Chỉ propagator TraceContext — KHÔNG dùng Baggage vì baggage có thể mang dữ
// liệu người dùng qua ranh giới dịch vụ.
func UseProviders(tp trace.TracerProvider, mp metric.MeterProvider) {
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	resetInstruments()
	enabled.Store(true)
}

// Disable trả về trạng thái tắt: provider no-op, không propagator. Dùng trong
// kiểm thử hoặc khi dừng dịch vụ sau shutdown.
func Disable() {
	enabled.Store(false)
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(metricnoop.NewMeterProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	resetInstruments()
}

// Tracer trả tracer của repo từ provider toàn cục (no-op khi tắt).
func Tracer() trace.Tracer { return otel.Tracer(InstrumentationName) }

// Meter trả meter của repo từ provider toàn cục (no-op khi tắt).
func Meter() metric.Meter { return otel.Meter(InstrumentationName) }

// Traceparent trả header W3C traceparent của span đang hoạt động, rỗng nếu không có.
func Traceparent(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID(), sc.SpanID(), sc.TraceFlags())
}

// Inject ghi ngữ cảnh trace của ctx vào carrier (no-op khi tắt).
func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// Extract đọc ngữ cảnh trace từ carrier (no-op khi tắt).
func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// ContextWithParent dựng ngữ cảnh cha từ traceparent đã lưu; không có thì từ
// traceID (32 hex) với span cha tổng hợp — giữ được trace_id xuyên suốt dù
// nơi ghi không có span. Không có gì hợp lệ → trả ctx.
func ContextWithParent(ctx context.Context, traceparent, traceID string) context.Context {
	if traceparent != "" {
		out := propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
		if trace.SpanContextFromContext(out).IsValid() {
			return out
		}
	}
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil || !tid.IsValid() {
		return ctx
	}
	var sid trace.SpanID
	if _, err := rand.Read(sid[:]); err != nil {
		return ctx
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled, Remote: true})
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// spanTraceID trả trace_id của span đang hoạt động, rỗng nếu không có.
func spanTraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	id := sc.TraceID()
	return hex.EncodeToString(id[:])
}
