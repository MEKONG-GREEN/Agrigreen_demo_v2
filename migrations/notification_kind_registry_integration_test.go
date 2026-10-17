package migrations_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Synthetic old-state snapshot, not a claim about production dataset size.
func TestNotificationKindConstraintUpgrade(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, 20261017022114)
	tid, recipient := ids.New(), ids.New()
	raw, err := os.ReadFile("../tests/golden/kn-notify-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	var old struct{ Kinds []string }
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Kinds) != 43 {
		t.Fatal("missing captured old vocabulary")
	}
	insert := func(tx pgx.Tx, kind string) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_notify.notification
          (tenant_id,id,recipient_ref,kind,title,body,deep_link,source_event_id,source_event_type,created_at)
          VALUES ($1,$2,$3,$4,'Fixture','','/fixture',$5,'fixture.kind.upgrade',$6)`,
			tid, ids.New(), recipient, kind, ids.New(), time.Now().UTC())
		return err
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		for i := 0; i < 1000; i++ {
			if err := insert(tx, old.Kinds[i%len(old.Kinds)]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() (int64, string) {
		t.Helper()
		var count int64
		var checksum string
		if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*),md5(coalesce(string_agg(to_jsonb(n)::text,'' ORDER BY id),''))
              FROM pf_notify.notification n WHERE tenant_id=$1`, tid).Scan(&count, &checksum)
		}); err != nil {
			t.Fatal(err)
		}
		return count, checksum
	}
	count, checksum := snapshot()
	if count != 1000 {
		t.Fatalf("old rows: %d", count)
	}
	reject := func(kind string) {
		t.Helper()
		err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insert(tx, kind) })
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "23514" {
			t.Fatalf("kind %q must fail CHECK: %v", kind, err)
		}
	}
	reject("fixture_kind")
	for i, version := range []uint{20261017220437, 20261017220439, 20261017220441} {
		migrateDir(t, dsn, "pf", version)
		got, sum := snapshot()
		if got != count || sum != checksum {
			t.Fatalf("phase %d changed existing rows: %d %s", i, got, sum)
		}
		var valid, legacy bool
		if err := pool.QueryRow(ctx, `SELECT
          EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='pf_notify.notification'::regclass AND conname='notification_kind_key_shape' AND convalidated),
          EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='pf_notify.notification'::regclass AND conname='notification_kind_check')`).Scan(&valid, &legacy); err != nil {
			t.Fatal(err)
		}
		if valid != (i > 0) || legacy != (i < 2) {
			t.Fatalf("phase %d valid=%v legacy=%v", i, valid, legacy)
		}
		if i < 2 {
			reject("fixture_kind")
		}
		if i == 0 {
			// A direct premature contract attempt must fail and leave old CHECK intact.
			statement, err := migrations.FS.ReadFile("pf/20261017220441_pf_notify_kind_key_contract.up.sql")
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, guardErr := tx.Exec(ctx, string(statement))
			_ = tx.Rollback(ctx)
			var pe *pgconn.PgError
			if !errors.As(guardErr, &pe) || pe.Code != "P0001" || !strings.Contains(pe.Message, "must be validated") {
				t.Fatalf("premature contract not rejected: %v", guardErr)
			}
		}
	}
	for _, kind := range []string{"a", "fixture_kind", strings.Repeat("a", 40)} {
		if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insert(tx, kind) }); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"", "_leading", "UPPER", "kind_v1", "a.b", "has space", strings.Repeat("a", 41)} {
		reject(kind)
	}
	if got, _ := snapshot(); got != 1003 {
		t.Fatalf("post-upgrade rows: %d", got)
	}
}
