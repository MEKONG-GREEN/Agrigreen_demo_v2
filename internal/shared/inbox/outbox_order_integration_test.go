package inbox_test

import (
	"context"
	"errors"
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

// hangBroker chặn tới khi ctx hết hạn (broker treo, connection.blocked).
type hangBroker struct{}

func (hangBroker) Publish(ctx context.Context, _ outbox.Envelope) error {
	<-ctx.Done()
	return ctx.Err()
}

func enqueueOne(t *testing.T, ctx context.Context, env *pgtest.Env, tid uuid.UUID) uuid.UUID {
	t.Helper()
	app := env.Pool(t, migrations.PF, "worker_platform")
	e := outbox.Event{ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded", SchemaVersion: 1, OccurredAt: time.Now(),
		Producer: "pf/party", Payload: map[string]string{"purpose": "mrv.evidence"}}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error { return outbox.Enqueue(ctx, tx, e) }); err != nil {
		t.Fatal(err)
	}
	return e.ID
}

// Review H3: broker treo không giữ giao dịch outbox vô hạn — PublishBatch trả lỗi trong khoảng PublishTimeout,
// sự kiện còn nguyên (chưa phát, chưa gác, attempts không tăng vì không phải broker từ chối).
func TestOutboxPublishTimeout(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	id := enqueueOne(t, ctx, env, ids.New())

	p := &outbox.Publisher{Pool: pub, Broker: hangBroker{}, PublishTimeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := p.PublishBatch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mong DeadlineExceeded, nhận %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("PublishBatch phải trả về theo PublishTimeout, mất %s", d)
	}
	var attempts int
	var published, parked *time.Time
	if err := pub.QueryRow(ctx, "SELECT attempts, published_at, parked_at FROM shared.outbox WHERE id = $1", id).
		Scan(&attempts, &published, &parked); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || published != nil || parked != nil {
		t.Fatalf("hết hạn chờ không được đổi trạng thái sự kiện: attempts=%d published=%v parked=%v", attempts, published, parked)
	}
}

// Review LOW (attempts): lỗi kết nối không đếm vào ngưỡng gác — sau nhiều lần đứt, MỘT lần broker từ chối chưa gác.
func TestOutboxConnectionErrorsDoNotCountTowardsPark(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	id := enqueueOne(t, ctx, env, ids.New())
	b := &pickyBroker{reject: map[uuid.UUID]bool{}, down: map[uuid.UUID]bool{id: true}, sent: map[uuid.UUID]int{}}
	p := &outbox.Publisher{Pool: pub, Broker: b}
	for i := 0; i < outbox.ParkAfterRejections; i++ {
		if _, err := p.PublishBatch(ctx); err == nil {
			t.Fatal("broker đứt phải trả lỗi")
		}
	}
	b.down[id], b.reject[id] = false, true
	if _, err := p.PublishBatch(ctx); !errors.Is(err, outbox.ErrRejected) {
		t.Fatalf("mong ErrRejected, nhận %v", err)
	}
	var parked *time.Time
	if err := pub.QueryRow(ctx, "SELECT parked_at FROM shared.outbox WHERE id = $1", id).Scan(&parked); err != nil {
		t.Fatal(err)
	}
	if parked != nil {
		t.Fatal("một lần từ chối sau các lần đứt kết nối không được gác sự kiện")
	}
}

// Review M2: hai bản publisher không phát cùng một loại song song (giữ thứ tự) — khi một bản đang giữ lượt,
// bản kia không phát gì.
func TestOutboxSinglePublisherPerKind(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	id := enqueueOne(t, ctx, env, ids.New())

	holder, err := pub.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx) //nolint:errcheck
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('ag.outbox.publish:platform'))"); err != nil {
		t.Fatal(err)
	}
	b := &pickyBroker{reject: map[uuid.UUID]bool{}, down: map[uuid.UUID]bool{}, sent: map[uuid.UUID]int{}}
	p := &outbox.Publisher{Pool: pub, Broker: b}
	if n, err := p.PublishBatch(ctx); err != nil || n != 0 || len(b.sent) != 0 {
		t.Fatalf("bản khác đang giữ lượt: không được phát (n=%d err=%v sent=%v)", n, err, b.sent)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50 && b.sent[id] == 0; i++ { // outbox dùng chung giữa các test: lặp tới khi tới lượt id
		if _, err := p.PublishBatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if b.sent[id] != 1 {
		t.Fatalf("nhả lượt → phát: sent=%d", b.sent[id])
	}
}
