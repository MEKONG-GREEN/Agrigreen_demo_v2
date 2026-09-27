package tenant_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func insertConsent(ctx context.Context, tx pgx.Tx, tid, subject uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.consent
	  (id, tenant_id, stream_key, subject_ref, purpose, text_version, status, channel, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, 'mrv.evidence', 'v1', 'granted', 'field_app', now(), $5, $6, 'consent/v1')`,
		ids.New(), tid, "subject:"+subject.String(), subject, ids.New(), ids.New().String())
	return err
}

// R-32: N goroutine, N tenant, qua PgBouncer transaction mode nếu có. Mỗi
// goroutine ghi rồi đọc; không goroutine nào được thấy bản ghi của tenant khác.
func TestTenantLeakParallel(t *testing.T) {
	env := pgtest.Get(t)
	if env.ViaBouncer {
		t.Log("chạy qua PgBouncer transaction mode")
	} else {
		t.Log("CẢNH BÁO: chạy thẳng PostgreSQL — CI phải đặt AG_TEST_PGBOUNCER_URL")
	}
	pool := env.Pool(t, migrations.PF, "party_app")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const tenants, rounds = 24, 15
	var wg sync.WaitGroup
	errs := make(chan error, tenants)
	for i := 0; i < tenants; i++ {
		tid := ids.New()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
					if err := insertConsent(ctx, tx, tid, ids.New()); err != nil {
						return err
					}
					var foreign int
					if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_party.consent WHERE tenant_id <> $1", tid).Scan(&foreign); err != nil {
						return err
					}
					if foreign != 0 {
						return fmt.Errorf("RÒ RỈ: tenant %s thấy %d bản ghi của tenant khác", tid, foreign)
					}
					var own int
					if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_party.consent").Scan(&own); err != nil {
						return err
					}
					if own != r+1 {
						return fmt.Errorf("tenant %s: thấy %d bản ghi, mong %d", tid, own, r+1)
					}
					return nil
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// R-31: thiếu ngữ cảnh tenant → rỗng, không phải "thấy tất cả".
func TestNoTenantContextSeesNothing(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, "party_app")
	ctx := context.Background()
	tid := ids.New()
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return insertConsent(ctx, tx, tid, ids.New()) }); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pf_party.consent").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("không có ngữ cảnh mà thấy %d bản ghi", n)
	}
	// Owner cũng bị FORCE RLS.
	owner := env.Pool(t, migrations.PF, dbadmin.Migrator)
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM pf_party.consent").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("owner không có ngữ cảnh mà thấy %d bản ghi", n)
	}
}

// WITH CHECK: không ghi được bản ghi mang tenant khác ngữ cảnh.
func TestCannotWriteForeignTenant(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, "party_app")
	ctx := context.Background()
	mine, theirs := ids.New(), ids.New()
	err := tenant.WithTenantTx(ctx, pool, mine, func(tx pgx.Tx) error { return insertConsent(ctx, tx, theirs, ids.New()) })
	if err == nil {
		t.Fatal("ghi bản ghi của tenant khác phải bị RLS chặn")
	}
}

// R-30: SET LOCAL hết hiệu lực sau giao dịch — kết nối trả về pool sạch.
func TestContextDoesNotOutliveTransaction(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	dsn, err := dbadmin.RoleURL(env.DirectURL, migrations.PF, env.Suffix, "party_app")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := tenant.WithTenantTx(ctx, conn, ids.New(), func(tx pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var v *string
	if err := conn.QueryRow(ctx, "SELECT nullif(current_setting('app.tenant_id', true), '')").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("app.tenant_id còn sót sau giao dịch: %s", *v)
	}
}

func TestRejectsNilTenant(t *testing.T) {
	err := tenant.WithTenantTx(context.Background(), nil, uuid.Nil, func(pgx.Tx) error { return nil })
	if !errors.Is(err, tenant.ErrMissing) {
		t.Fatalf("muốn ErrMissing, nhận %v", err)
	}
}
