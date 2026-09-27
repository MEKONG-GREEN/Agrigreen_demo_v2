package inbox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// headerBroker giả lập Rabbit.Publish: dựng header như thật và ghi lại.
type headerBroker struct {
	mu      sync.Mutex
	headers map[string]amqp.Table
	envs    map[string]outbox.Envelope
}

func (b *headerBroker) Publish(ctx context.Context, e outbox.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.headers[e.EventID.String()] = outbox.MessageHeaders(ctx, e)
	b.envs[e.EventID.String()] = e
	return nil
}

// R-90: trace đi xuyên API → outbox → hàng đợi → consumer: cùng trace_id, và
// span consumer là con của span publish, span publish là con của span request.
func TestTracePropagatesOutboxToInbox(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	telemetry.UseProviders(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)), metricnoop.NewMeterProvider())
	t.Cleanup(telemetry.Disable)

	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	tid := ids.New()

	ctx, root := telemetry.Tracer().Start(context.Background(), "POST /v1/consents")
	rootSC := root.SpanContext()
	evt := outbox.Event{ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded", SchemaVersion: 1,
		OccurredAt: time.Now(), Producer: "pf/party", Payload: map[string]string{}}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error { return outbox.Enqueue(ctx, tx, evt) }); err != nil {
		t.Fatal(err)
	}
	root.End()

	var stored string
	if err := pub.QueryRow(context.Background(), "SELECT trace_parent FROM shared.outbox WHERE id = $1", evt.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, rootSC.TraceID().String()) {
		t.Fatalf("trace_parent lưu sai: %q", stored)
	}

	b := &headerBroker{headers: map[string]amqp.Table{}, envs: map[string]outbox.Envelope{}}
	if _, err := (&outbox.Publisher{Pool: pub, Broker: b}).PublishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, ok := b.headers[evt.ID.String()]
	if !ok {
		t.Fatal("sự kiện chưa được phát")
	}
	if h["trace_id"] != rootSC.TraceID().String() {
		t.Fatalf("header trace_id = %v, mong %s (lấy từ span khi Event.TraceID rỗng)", h["trace_id"], rootSC.TraceID())
	}
	tp, _ := h["traceparent"].(string)
	if !strings.HasPrefix(tp, "00-"+rootSC.TraceID().String()+"-") {
		t.Fatalf("header traceparent = %q", tp)
	}

	mctx, span := inbox.StartConsume(context.Background(), "test.trace", h, b.envs[evt.ID.String()])
	if telemetry.TraceID(mctx) != rootSC.TraceID().String() {
		t.Fatalf("consumer mất trace_id: %q", telemetry.TraceID(mctx))
	}
	span.End()

	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range exp.GetSpans().Snapshots() {
		if s.SpanContext().TraceID() == rootSC.TraceID() {
			byName[strings.Fields(s.Name())[0]] = s
		}
	}
	pubSpan, conSpan := byName["publish"], byName["process"]
	if pubSpan == nil || conSpan == nil {
		t.Fatalf("thiếu span publish/process: %v", byName)
	}
	if pubSpan.Parent().SpanID() != rootSC.SpanID() {
		t.Fatal("span publish phải là con của span request")
	}
	if conSpan.Parent().SpanID() != pubSpan.SpanContext().SpanID() || conSpan.SpanKind() != trace.SpanKindConsumer {
		t.Fatal("span consumer phải là con của span publish")
	}
}

// OTel tắt: header chỉ có trace_id + schema_version như G0.
func TestHeadersWithoutOTel(t *testing.T) {
	telemetry.Disable()
	e := outbox.Envelope{EventID: ids.New(), TraceID: "trace-abc", SchemaVersion: 2}
	h := outbox.MessageHeaders(context.Background(), e)
	if len(h) != 2 || h["trace_id"] != "trace-abc" || h["schema_version"] != int32(2) {
		t.Fatalf("header: %v", h)
	}
	ctx, _ := inbox.StartConsume(context.Background(), "c", h, e)
	if telemetry.TraceID(ctx) != "trace-abc" {
		t.Fatal("consumer phải giữ trace_id của phong bì")
	}
}

func TestDLQDepth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "mon" || p != "s3cret" || r.URL.Path != "/api/queues" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"pf.audit.consent","vhost":"/","messages":3},{"name":"pf.audit.consent.dlq","vhost":"/","messages":2}]`))
	}))
	defer srv.Close()
	u := strings.Replace(srv.URL, "http://", "http://mon:s3cret@", 1)
	got, err := inbox.DLQDepth(context.Background(), srv.Client(), u)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["pf.audit.consent.dlq"] != 2 {
		t.Fatalf("DLQ: %v", got)
	}
	bad := strings.Replace(srv.URL, "http://", "http://mon:wrong@", 1)
	if _, err := inbox.DLQDepth(context.Background(), srv.Client(), bad); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("lỗi phải có và không lộ mật khẩu: %v", err)
	}
}
