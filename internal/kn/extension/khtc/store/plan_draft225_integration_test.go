package store_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func TestDraft225TypedHeaderSerializesConcurrentLateAppend(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	tid, actor := uuid.New(), uuid.New()
	ctx, cancel := context.WithTimeout(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, TenantID: tid}), 20*time.Second)
	defer cancel()
	owned := db.ForModule(pool, db.Module("pf/extension"))
	tx, err := owned.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	program := uuid.New()
	qty := decimal.RequireFromString("7.2500")
	entries := []domain.Entry{{Ref: uuid.New(), Tab: domain.TabPrograms, Kind: domain.EntryProgram, ProgramRef: &program, Name: "Cây đã chốt", SortOrder: 1, PlannedQty: &qty, Tracking: "tracking", Communes: []string{"X1"}}}
	event := store.DraftEvent225{Meta: store.Meta{ID: uuid.New(), ActorRef: actor, EventTime: time.Now().UTC(), IdemKey: "draft225-header-race"}, Year: 2032, DraftID: uuid.New(), Action: "create", EntriesCount: 1, EntriesHash: domain.EntriesHash(entries), RequestHash: bytes.Repeat([]byte{1}, 32), AuthorityHash: bytes.Repeat([]byte{2}, 32)}
	if err = store.InsertDraft225(ctx, tx, tid, &event, entries); err != nil {
		t.Fatal(err)
	}
	// Capture only our own second connection identity, and observe a real waiter.
	pidCh := make(chan int32, 1)
	done := make(chan error, 1)
	go func() {
		done <- tenant.WithTenantTx(ctx, owned, tid, func(other pgx.Tx) error {
			var pid int32
			if e := other.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			pidCh <- pid
			_, e := other.Exec(ctx, `INSERT INTO pf_extension.khtc_plan_draft225_entry
   (tenant_id,draft_event_id,draft_recorded_at,plan_year,entry_ref,position,tab,kind,program_ref,sort_order,name,communes,tracking)
   VALUES($1,$2,$3,2032,$4,1,'programs','program',$5,2,'late append',ARRAY[]::text[],'tracking')`, tid, event.ID, event.RecordedAt, uuid.New(), program)
			return e
		})
	}()
	var pid int32
	select {
	case pid = <-pidCh:
	case <-ctx.Done():
		t.Fatal("second TX did not start")
	}
	admin := env.AdminPool(t, migrations.PF)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waiting := false
	for !waiting {
		select {
		case e := <-done:
			t.Fatalf("late append did not wait for uncommitted header: %T", e)
		case <-ctx.Done():
			t.Fatal("advisory waiter not observed")
		case <-ticker.C:
			if e := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, pid).Scan(&waiting); e != nil {
				t.Fatal(e)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23514" || pg.Message != "khtc_draft225_entries_frozen" {
			t.Fatalf("late append after ACK error=%T", e)
		}
	case <-ctx.Done():
		t.Fatal("late append did not terminate after header commit")
	}
	if err = tenant.WithTenantTx(ctx, owned, tid, func(read pgx.Tx) error {
		got, e := store.DraftEntries225(ctx, read, event)
		if e != nil {
			return e
		}
		if len(got) != 1 || !bytes.Equal(domain.EntriesHash(got), event.EntriesHash) {
			t.Fatal("stored typed payload/hash changed after ACK")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// An entry cannot claim another year to evade the resource lock: FK binds year.
	err = tenant.WithTenantTx(ctx, owned, tid, func(other pgx.Tx) error {
		_, e := other.Exec(ctx, `INSERT INTO pf_extension.khtc_plan_draft225_entry
 (tenant_id,draft_event_id,draft_recorded_at,plan_year,entry_ref,position,tab,kind,program_ref,sort_order,name,communes,tracking)
 VALUES($1,$2,$3,2033,$4,1,'programs','program',$5,2,'wrong year',ARRAY[]::text[],'tracking')`, tid, event.ID, event.RecordedAt, uuid.New(), program)
		return e
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatalf("cross-year late append must fail immutable-header guard: %T", err)
	}
	// A genuinely orphan staged row is rejected at COMMIT by its deferred FK.
	err = tenant.WithTenantTx(ctx, owned, tid, func(other pgx.Tx) error {
		_, e := other.Exec(ctx, `INSERT INTO pf_extension.khtc_plan_draft225_entry
 (tenant_id,draft_event_id,draft_recorded_at,plan_year,entry_ref,position,tab,kind,program_ref,sort_order,name,communes,tracking)
 VALUES($1,$2,$3,2034,$4,0,'programs','program',$5,0,'orphan',ARRAY[]::text[],'tracking')`, tid, uuid.New(), event.RecordedAt, uuid.New(), program)
		return e
	})
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Fatalf("orphan typed payload committed without header: %T", err)
	}
	if err = tenant.WithTenantTx(ctx, owned, uuid.New(), func(read pgx.Tx) error {
		got, e := store.DraftEntries225(ctx, read, event)
		if e != nil {
			return e
		}
		if len(got) != 0 {
			t.Fatal("foreign tenant read immutable payload")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
