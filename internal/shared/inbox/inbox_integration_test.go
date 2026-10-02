package inbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fakeBroker struct {
	mu   sync.Mutex
	sent []outbox.Envelope
	fail bool
}

func (b *fakeBroker) Publish(_ context.Context, e outbox.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail {
		return errors.New("broker down")
	}
	b.sent = append(b.sent, e)
	return nil
}

// Toàn tuyến mục 12: ghi outbox trong tx nghiệp vụ → publisher phát sau confirm →
// consumer xử lý; giao lặp cùng sự kiện hai lần → kết quả như giao một lần (R-61).
func TestOutboxToInboxExactlyOnceEffect(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	tid := ids.New()

	evt := outbox.Event{
		ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded", SchemaVersion: 1,
		OccurredAt: time.Now(), TraceID: "trace-abc", Producer: "pf/party",
		Payload: map[string]string{"purpose": "mrv.evidence"},
	}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error { return outbox.Enqueue(ctx, tx, evt) }); err != nil {
		t.Fatal(err)
	}

	// Broker hỏng: không được đánh dấu đã phát.
	broker := &fakeBroker{fail: true}
	p := &outbox.Publisher{Pool: pub, Broker: broker}
	if n, err := p.PublishBatch(ctx); err == nil || n != 0 {
		t.Fatalf("broker hỏng phải trả lỗi và 0 sự kiện, nhận n=%d err=%v", n, err)
	}
	broker.fail = false
	if _, err := p.PublishBatch(ctx); err != nil {
		t.Fatal(err)
	}
	var mine []outbox.Envelope
	for _, e := range broker.sent {
		if e.EventID == evt.ID {
			mine = append(mine, e)
		}
	}
	if len(mine) != 1 || mine[0].TraceID != "trace-abc" {
		t.Fatalf("phải phát đúng một lần kèm trace_id: %+v", mine)
	}
	if _, err := p.PublishBatch(ctx); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range broker.sent {
		if e.EventID == evt.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("sự kiện đã phát không được phát lại, đếm %d", count)
	}

	// Giao hai lần.
	calls := 0
	handler := func(ctx context.Context, tx pgx.Tx, e outbox.Envelope) error {
		calls++
		var payload map[string]string
		return json.Unmarshal(e.Payload, &payload)
	}
	for i := 0; i < 2; i++ {
		processed, err := inbox.Process(ctx, app, "test.consumer", mine[0], handler)
		if err != nil {
			t.Fatal(err)
		}
		if processed != (i == 0) {
			t.Fatalf("lần giao %d: processed=%v", i+1, processed)
		}
	}
	if calls != 1 {
		t.Fatalf("handler chạy %d lần, mong 1", calls)
	}
}

// Handler lỗi → giao dịch rollback, inbox không ghi → lần giao sau xử lý lại được.
func TestHandlerFailureAllowsRetry(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	e := outbox.Envelope{EventID: ids.New(), TenantID: ids.New(), EventType: "pf.party.consent.recorded", Payload: json.RawMessage(`{}`)}

	_, err := inbox.Process(ctx, app, "retry.consumer", e, func(context.Context, pgx.Tx, outbox.Envelope) error {
		return errors.New("lỗi tạm")
	})
	if err == nil {
		t.Fatal("phải trả lỗi")
	}
	processed, err := inbox.Process(ctx, app, "retry.consumer", e, func(context.Context, pgx.Tx, outbox.Envelope) error { return nil })
	if err != nil || !processed {
		t.Fatalf("lần giao lại phải được xử lý: processed=%v err=%v", processed, err)
	}
}

// KN P8: handler nhận ngữ cảnh mang tenant của sự kiện (cổng module khác đọc tenant từ ngữ cảnh — consumer gửi Chi cục
// đọc tham số masterdata từng lỗi "tenant: thiếu tenant_id" trong worker).
func TestHandlerContextCarriesEventTenant(t *testing.T) {
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	e := outbox.Envelope{EventID: ids.New(), TenantID: ids.New(), EventType: "pf.party.consent.recorded", Payload: json.RawMessage(`{}`)}
	processed, err := inbox.Process(context.Background(), app, "tenant.ctx.consumer", e, func(ctx context.Context, _ pgx.Tx, ev outbox.Envelope) error {
		got, err := tenant.FromContext(ctx)
		if err != nil || got != ev.TenantID {
			t.Fatalf("ngữ cảnh handler phải mang tenant %s, nhận %s %v", ev.TenantID, got, err)
		}
		return nil
	})
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
}

func TestEventTypeConvention(t *testing.T) {
	for _, bad := range []string{"consent.recorded", "pf.party.consent", "PF.party.consent.recorded", "x.party.consent.recorded"} {
		if outbox.ValidateType(bad) == nil {
			t.Errorf("%q phải bị từ chối", bad)
		}
	}
	if err := outbox.ValidateType("mkg.measurement.weigh_ticket.recorded"); err != nil {
		t.Error(err)
	}
}
