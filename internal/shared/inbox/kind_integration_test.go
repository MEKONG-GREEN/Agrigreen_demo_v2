package inbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0018 (Q-MKG-01): outbox cụm mkg có hai loại; mỗi publisher chỉ quét loại
// của nó và phát qua broker (vhost) của loại đó — sự kiện nội bộ MKG không bao
// giờ lên vhost nền tảng, sự kiện van không lên vhost nội bộ.
func TestOutboxKindRoutesToOwnBroker(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.MKG, "worker_internal")
	pub := env.Pool(t, migrations.MKG, dbadmin.OutboxPublisher)
	tid := ids.New()
	mk := func(typ string, kind outbox.Kind) outbox.Event {
		return outbox.Event{ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: time.Now(),
			Producer: "mkg/test", Payload: map[string]string{"k": "v"}, Kind: kind}
	}
	platformEvt := mk("mkg.hub.capacity.published", "")
	internalEvt := mk("mkg.measurement.weigh_ticket.recorded", outbox.KindInternal)
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		if err := outbox.Enqueue(ctx, tx, platformEvt); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, internalEvt)
	}); err != nil {
		t.Fatal(err)
	}
	sentIDs := func(b *fakeBroker) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, e := range b.sent {
			out[e.EventID] = true
		}
		return out
	}
	platform, internal := &fakeBroker{}, &fakeBroker{}
	if _, err := (&outbox.Publisher{Pool: pub, Broker: platform}).PublishBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := (&outbox.Publisher{Pool: pub, Broker: internal, Kind: outbox.KindInternal}).PublishBatch(ctx); err != nil {
		t.Fatal(err)
	}
	p, i := sentIDs(platform), sentIDs(internal)
	if !p[platformEvt.ID] || p[internalEvt.ID] {
		t.Fatalf("vhost nền tảng chỉ được nhận sự kiện platform: %v", p)
	}
	if !i[internalEvt.ID] || i[platformEvt.ID] {
		t.Fatalf("vhost mkg.internal chỉ được nhận sự kiện internal: %v", i)
	}
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Sự kiện nội bộ chỉ có tên mkg.* — kiểm ở Enqueue và ở CHECK DB (outbox_internal_is_mkg).
func TestOutboxInternalKindMustBeMKG(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	tid := ids.New()
	err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded",
			SchemaVersion: 1, OccurredAt: time.Now(), Producer: "pf/party", Payload: map[string]string{}, Kind: outbox.KindInternal})
	})
	if err == nil {
		t.Fatal("Enqueue phải từ chối sự kiện nội bộ không thuộc mkg")
	}
	err = tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO shared.outbox (id, tenant_id, event_type, schema_version, occurred_at, producer, payload, kind)
		  VALUES ($1, $2, 'pf.party.consent.recorded', 1, now(), 'pf/party', '{}', 'internal')`, ids.New(), tid)
		return err
	})
	if pgCode(err) != "23514" {
		t.Fatalf("CHECK outbox_internal_is_mkg phải chặn: %v", err)
	}
}
