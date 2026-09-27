package telemetry

import (
	"context"
	"testing"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jackc/pgx/v5"
)

func TestSetupDisabledWhenNoEndpoint(t *testing.T) {
	t.Setenv("AG_OTLP_ENDPOINT", "")
	shutdown, err := Setup(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("không có AG_OTLP_ENDPOINT thì phải tắt")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Tắt: không có span, TraceID chỉ lấy từ context như G0.
	ctx, span := Tracer().Start(context.Background(), "x")
	defer span.End()
	if span.SpanContext().IsValid() || TraceID(ctx) != "" || Traceparent(ctx) != "" {
		t.Fatal("khi tắt không được sinh span hợp lệ")
	}
}

func TestConfigFromEnvRejectsBadValues(t *testing.T) {
	t.Setenv("AG_OTLP_SAMPLE_RATIO", "2")
	if _, err := ConfigFromEnv("x"); err == nil {
		t.Fatal("tỷ lệ lấy mẫu > 1 phải bị từ chối")
	}
	t.Setenv("AG_OTLP_SAMPLE_RATIO", "")
	t.Setenv("AG_OTLP_PROTOCOL", "carrier-pigeon")
	t.Setenv("AG_OTLP_ENDPOINT", "localhost:4317")
	if _, err := Setup(context.Background(), "x"); err == nil {
		t.Fatal("giao thức lạ phải bị từ chối")
	}
	if Enabled() {
		t.Fatal("cấu hình lỗi không được bật OTel")
	}
}

func TestContextWithParent(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx := ContextWithParent(context.Background(), tp, "")
	sc := trace.SpanContextFromContext(ctx)
	if sc.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || sc.SpanID().String() != "00f067aa0ba902b7" || !sc.IsRemote() {
		t.Fatalf("traceparent không được khôi phục: %v", sc)
	}
	ctx = ContextWithParent(context.Background(), "", "4bf92f3577b34da6a3ce929d0e0e4736")
	if trace.SpanContextFromContext(ctx).TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatal("chỉ có trace_id vẫn phải giữ trace_id")
	}
	if trace.SpanContextFromContext(ContextWithParent(context.Background(), "rác", "trace-abc")).IsValid() {
		t.Fatal("giá trị không hợp lệ phải bị bỏ qua")
	}
}

func TestSQLOperation(t *testing.T) {
	cases := map[string]string{
		"SELECT 1":                            "SELECT",
		"  insert into x values ($1)":         "INSERT",
		"-- chú thích\nUPDATE t SET a=1":      "UPDATE",
		"/* x */ WITH a AS (SELECT 1) SELECT": "WITH",
		"VACUUM":                              "OTHER",
		"":                                    "OTHER",
	}
	for in, want := range cases {
		if got := SQLOperation(in); got != want {
			t.Errorf("%q → %q, mong %q", in, got, want)
		}
	}
}

// Span SQL chỉ có tên thao tác — không văn bản SQL, không tham số (R-14).
func TestPgxTracerNoSQLNoArgs(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	UseProviders(tp, metricnoop.NewMeterProvider())
	t.Cleanup(Disable)

	tr := PgxTracer()
	// Không có span cha → không tạo span.
	c0 := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(c0, nil, pgx.TraceQueryEndData{})
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("không có span cha thì không tạo span, có %d", n)
	}

	ctx, parent := Tracer().Start(context.Background(), "parent")
	c1 := tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM x WHERE phone = $1", Args: []any{"0909123456"}})
	tr.TraceQueryEnd(c1, nil, pgx.TraceQueryEndData{})
	parent.End()
	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("mong 2 span, có %d", len(spans))
	}
	for _, s := range spans {
		for _, a := range s.Attributes {
			v := a.Value.String()
			if v == "0909123456" || len(v) > 40 {
				t.Fatalf("thuộc tính %s=%q có thể lộ SQL/tham số", a.Key, v)
			}
		}
	}
	if spans[0].Name != "db SELECT" {
		t.Fatalf("tên span %q", spans[0].Name)
	}
}
