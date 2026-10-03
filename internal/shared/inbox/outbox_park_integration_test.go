package inbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pickyBroker từ chối (nack) các id trong reject, lỗi kết nối với các id trong down, nhận phần còn lại.
type pickyBroker struct {
	mu     sync.Mutex
	reject map[uuid.UUID]bool
	down   map[uuid.UUID]bool
	sent   map[uuid.UUID]int
}

func (b *pickyBroker) Publish(_ context.Context, e outbox.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.reject[e.EventID]:
		return outbox.ErrRejected
	case b.down[e.EventID]:
		return errors.New("amqp: connection closed")
	}
	b.sent[e.EventID]++
	return nil
}

// Review M6: sự kiện bị broker từ chối lặp lại chặn sự kiện sau tới khi bị gác (ParkAfterRejections lần), rồi
// sự kiện sau được phát; lỗi kết nối không bao giờ gác. Backlog đếm riêng sự kiện bị gác.
func TestOutboxParksRepeatedlyRejectedEvent(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	tid := ids.New()
	enqueue := func() uuid.UUID {
		t.Helper()
		e := outbox.Event{ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded", SchemaVersion: 1, OccurredAt: time.Now(),
			Producer: "pf/party", Payload: map[string]string{"purpose": "mrv.evidence"}}
		if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error { return outbox.Enqueue(ctx, tx, e) }); err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	poison, next := enqueue(), enqueue()
	b := &pickyBroker{reject: map[uuid.UUID]bool{poison: true}, down: map[uuid.UUID]bool{}, sent: map[uuid.UUID]int{}}
	p := &outbox.Publisher{Pool: pub, Broker: b}
	parkedAt := func(id uuid.UUID) *time.Time {
		t.Helper()
		var at *time.Time
		if err := pub.QueryRow(ctx, "SELECT parked_at FROM shared.outbox WHERE id = $1", id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	for i := 1; i < outbox.ParkAfterRejections; i++ {
		if _, err := p.PublishBatch(ctx); !errors.Is(err, outbox.ErrRejected) {
			t.Fatalf("lần %d: mong ErrRejected, nhận %v", i, err)
		}
		if b.sent[next] != 0 || parkedAt(poison) != nil {
			t.Fatalf("lần %d: chưa đủ ngưỡng — giữ thứ tự (sự kiện sau chưa phát), chưa gác", i)
		}
	}
	if _, err := p.PublishBatch(ctx); !errors.Is(err, outbox.ErrRejected) {
		t.Fatalf("lần gác: mong lỗi bọc ErrRejected, nhận %v", err)
	}
	if parkedAt(poison) == nil || b.sent[next] != 1 {
		t.Fatalf("đủ ngưỡng: gác sự kiện độc và phát sự kiện sau (sent=%d)", b.sent[next])
	}
	if _, err := p.PublishBatch(ctx); err != nil || b.sent[poison] != 0 {
		t.Fatalf("sự kiện đã gác không được quét lại: err=%v", err)
	}
	bl, err := outbox.ReadBacklog(ctx, pub)
	if err != nil || bl.Parked < 1 {
		t.Fatalf("backlog phải đếm sự kiện bị gác: %+v %v", bl, err)
	}

	// Lỗi kết nối: thử lại mãi, không gác (giữ thứ tự).
	stuck := enqueue()
	b.down[stuck] = true
	for i := 0; i < outbox.ParkAfterRejections+2; i++ {
		if _, err := p.PublishBatch(ctx); err == nil {
			t.Fatal("broker đứt phải trả lỗi")
		}
	}
	if parkedAt(stuck) != nil {
		t.Fatal("lỗi kết nối không được gác sự kiện")
	}
	b.down[stuck] = false
	if _, err := p.PublishBatch(ctx); err != nil || b.sent[stuck] != 1 {
		t.Fatalf("broker hồi phục → phát: err=%v sent=%d", err, b.sent[stuck])
	}
}
