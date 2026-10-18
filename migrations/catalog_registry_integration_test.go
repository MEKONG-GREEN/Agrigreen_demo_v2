package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// The fixture exercises all three transactions on existing append-only rows.
// It is deliberately a synthetic 1,000-row dataset, not a production-size claim.
func TestCatalogRegistryConstraintUpgrade(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, 20261015223036)
	tid := ids.New()
	insert := func(tx pgx.Tx, catalog, code string) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.catalog_effective
   (id,tenant_id,stream_key,catalog,code,change_id,valid_from,after,event_time,actor_ref,idem_key,rule_version)
   VALUES ($1,$2,$3,$4,$5,$6,'2026-01-01',
   '{"name":"fixture","attrs":{"value":"x"},"active":true}'::jsonb,$7,$8,$9,'pf.masterdata.catalog_effective/v1')`,
			ids.New(), tid, "catalog:"+catalog+":"+code, catalog, code, ids.New(), time.Now().UTC(), ids.New(), ids.New().String())
		return err
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		for i := 0; i < 1000; i++ {
			if err := insert(tx, "cnts_fixture", fmt.Sprintf("R%04d", i)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() (int64, string) {
		var n int64
		var sum string
		if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*),md5(coalesce(string_agg(to_jsonb(e)::text,'' ORDER BY id),''))
   FROM pf_masterdata.catalog_effective e WHERE tenant_id=$1`, tid).Scan(&n, &sum)
		}); err != nil {
			t.Fatal(err)
		}
		return n, sum
	}
	n, checksum := snapshot()
	if n != 1000 {
		t.Fatalf("fixture rows: %d", n)
	}
	expectReject := func(catalog string) {
		err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insert(tx, catalog, "BLOCKED") })
		var pe *pgconn.PgError
		if err == nil || !errors.As(err, &pe) || pe.Code != "23514" {
			t.Fatalf("catalog %s should fail CHECK: %v", catalog, err)
		}
	}
	expectReject("fixture_catalog")
	for i, version := range []uint{20261017022108, 20261017022111, 20261017022114} {
		migrateDir(t, dsn, "pf", version)
		got, sum := snapshot()
		if got != n || sum != checksum {
			t.Fatalf("phase %d changed row content/hash: %d %s", i, got, sum)
		}
		var valid, old bool
		if err := pool.QueryRow(ctx, `SELECT
   EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='pf_masterdata.catalog_effective'::regclass AND conname='catalog_effective_catalog_registry_check' AND convalidated),
   EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='pf_masterdata.catalog_effective'::regclass AND conname='catalog_effective_catalog_check')`).Scan(&valid, &old); err != nil {
			t.Fatal(err)
		}
		if valid != (i > 0) || old != (i < 2) {
			t.Fatalf("phase %d constraints valid=%v old=%v", i, valid, old)
		}
	}
	if got := verifyAll(t, pool)["pf_masterdata.catalog_effective"]; got != 1000 {
		t.Fatalf("old chain rows: %d", got)
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insert(tx, "fixture_catalog", "NEW") }); err != nil {
		t.Fatal(err)
	}
	expectReject("bad.catalog")
	if got := verifyAll(t, pool)["pf_masterdata.catalog_effective"]; got != 1001 {
		t.Fatalf("new chain rows: %d", got)
	}
}
