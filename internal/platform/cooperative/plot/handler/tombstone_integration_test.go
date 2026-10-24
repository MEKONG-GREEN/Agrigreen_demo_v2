package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func transferPlot(t *testing.T, f *fixture, ref, owner uuid.UUID) {
	t.Helper()
	body := map[string]any{}
	if owner != uuid.Nil {
		body["cooperative_ref"] = owner.String()
	}
	expect(t, f.national().appDo(http.MethodPost, "/v1/plots/"+ref.String()+"/assign", body), http.StatusOK, "transfer")
}

func TestPlotScopeTombstonePaginationAndPrivacy(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	lost, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	kept, _ := nat.createPlot("31117", f.coopA, rect(400, 0, 100, 100))
	source := f.mod.SyncSource()
	who := syncCtx(f, f.coopA, coopport.RoleCoopManager)
	scope := authz.Scope{Units: []string{"31117"}}
	_, cursor, err := source.Changes(who, scope, "plot", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	transferPlot(t, f, lost, f.coopB)
	// Later updates by the new owner must neither disclose metadata nor advance
	// the old owner's version/cursor beyond the ownership-loss event.
	var lossSeq, lossRev int64
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	if err := admin.QueryRow(context.Background(), `SELECT sync_seq, revision FROM pf_coop.plot_scope_tombstone
   WHERE tenant_id=$1 AND plot_ref=$2 AND previous_cooperative_ref=$3`, f.tenant, lost, f.coopA).Scan(&lossSeq, &lossRev); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []uuid.UUID{lost, kept} {
		expect(t, nat.appDo(http.MethodPost, "/v1/plots/"+ref.String()+"/retire", map[string]any{"reason": "Thửa tạm ngừng để kiểm tra trạng thái"}), http.StatusOK, "same-owner retire")
		expect(t, nat.appDo(http.MethodPost, "/v1/plots/"+ref.String()+"/restore", map[string]any{"reason": "Khôi phục thửa sau kiểm tra trạng thái"}), http.StatusOK, "same-owner restore")
	}
	ch, next, err := source.Changes(who, scope, "plot", cursor, 1)
	if err != nil || len(ch) != 1 || ch[0].ID != lost || ch[0].Op != "tombstone" || ch[0].Data != nil || ch[0].Version != lossRev || next != lossSeq {
		t.Fatalf("old owner ID-only page: err=%v next=%d loss=%d changes=%+v", err, next, lossSeq, ch)
	}
	ch, final, err := source.Changes(who, scope, "plot", next, 1)
	if err != nil || len(ch) != 1 || ch[0].ID != kept || ch[0].Op != "upsert" || final <= next {
		t.Fatalf("shared page limit: %v %+v cursor=%d", err, ch, final)
	}
	ch, again, err := source.Changes(who, scope, "plot", final, 1)
	if err != nil || len(ch) != 0 || again != final {
		t.Fatalf("empty final page: %v %+v %d", err, ch, again)
	}
	ch, _, err = source.Changes(who, authz.Scope{Units: []string{"31120"}}, "plot", cursor, 100)
	if err != nil || len(ch) != 0 {
		t.Fatalf("outside commune: %v %+v", err, ch)
	}
	// The new owner receives the current row normally.
	ch, _, err = source.Changes(syncCtx(f, f.coopB, coopport.RoleCoopManager), scope, "plot", 0, 100)
	if err != nil || len(ch) != 1 || ch[0].ID != lost || ch[0].Op != "upsert" {
		t.Fatalf("new owner: %v %+v", err, ch)
	}
	var count int
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.plot_scope_tombstone WHERE tenant_id=$1 AND plot_ref=$2`, f.tenant, lost).Scan(&count); err != nil || count != 1 {
		t.Fatalf("same-owner update captured: %d %v", count, err)
	}
}

func TestPlotScopeTombstoneReturnAndRollback(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.national().createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	source := f.mod.SyncSource()
	sc := authz.Scope{Units: []string{"31117"}}
	a := syncCtx(f, f.coopA, coopport.RoleCoopManager)
	b := syncCtx(f, f.coopB, coopport.RoleCoopManager)
	_, cursor, err := source.Changes(a, sc, "plot", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	transferPlot(t, f, ref, f.coopB)
	transferPlot(t, f, ref, f.coopA)
	ch, next, err := source.Changes(a, sc, "plot", cursor, 1)
	if err != nil || len(ch) != 1 || ch[0].ID != ref || ch[0].Op != "upsert" {
		t.Fatalf("A→B→A must suppress older loss: %v %+v", err, ch)
	}
	ch, _, err = source.Changes(b, sc, "plot", 0, 100)
	if err != nil || len(ch) != 1 || ch[0].Op != "tombstone" || ch[0].Data != nil {
		t.Fatalf("B after return: %v %+v", err, ch)
	}
	tx, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(context.Background(), `UPDATE pf_coop.plot SET cooperative_ref=$1,revision=revision+1,sync_seq=nextval('pf_coop.plot_sync_seq') WHERE tenant_id=$2 AND ref=$3`, f.coopB, f.tenant, ref); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.plot_scope_tombstone WHERE tenant_id=$1 AND plot_ref=$2`, f.tenant, ref).Scan(&count); err != nil || count != 3 {
		t.Fatalf("same-TX trigger: %d %v", count, err)
	}
	if err = tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	ch, again, err := source.Changes(a, sc, "plot", next, 100)
	if err != nil || len(ch) != 0 || again != next {
		t.Fatalf("rolled-back transfer leaked: %v %+v", err, ch)
	}
}

func TestPlotScopeTombstoneRLSAndPrivileges(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.national().createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	transferPlot(t, f, ref, f.coopB)
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		tenant, node uuid.UUID
		xa           string
		want         int
	}{
		{"previous owner", f.tenant, f.coopA, "", 1},
		{"new owner", f.tenant, f.coopB, "", 0},
		{"missing projection", f.tenant, ids.New(), "", 0},
		{"no node", f.tenant, uuid.Nil, "", 0},
		{"commune alone", f.tenant, f.orgA, `{"31117"}`, 0},
		{"foreign tenant", ids.New(), f.coopA, "", 0},
		{"missing tenant", uuid.Nil, f.coopA, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := admin.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			tid, node := "", ""
			if tc.tenant != uuid.Nil {
				tid = tc.tenant.String()
			}
			if tc.node != uuid.Nil {
				node = tc.node.String()
			}
			if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.node_id',$2,true),set_config('app.xa_ids',$3,true)`, tid, node, tc.xa); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `SET LOCAL ROLE r_cooperative`); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot_scope_tombstone WHERE plot_ref=$1`, ref).Scan(&n); err != nil || n != tc.want {
				t.Fatalf("RLS got=%d want=%d err=%v", n, tc.want, err)
			}
		})
	}
	t.Run("old owner without self projection", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `DELETE FROM k_ref.node_closure WHERE tenant_id=$1 AND ancestor=$2 AND descendant=$2 AND depth=0`, f.tenant, f.coopA); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.node_id',$2,true)`, f.tenant.String(), f.coopA.String()); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `SET LOCAL ROLE r_cooperative`); err != nil {
			t.Fatal(err)
		}
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot_scope_tombstone WHERE plot_ref=$1`, ref).Scan(&n); err != nil || n != 0 {
			t.Fatalf("old owner without self: %d %v", n, err)
		}
	})
	t.Run("trigger rejects foreign tenant context", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, ids.New().String()); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `UPDATE pf_coop.plot SET cooperative_ref=$1,revision=revision+1,sync_seq=nextval('pf_coop.plot_sync_seq') WHERE tenant_id=$2 AND ref=$3`, f.coopA, f.tenant, ref)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("foreign context trigger: %v", err)
		}
	})
	t.Run("trigger rejects spoofed source", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `CREATE TEMP TABLE scope_spoof(v int); INSERT INTO scope_spoof VALUES (1);
          CREATE TRIGGER spoof AFTER UPDATE ON scope_spoof FOR EACH ROW EXECUTE FUNCTION pf_coop.capture_plot_scope_loss()`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `UPDATE scope_spoof SET v=2`)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("spoofed source trigger: %v", err)
		}
	})
	t.Run("transfer requires a new source cursor", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `UPDATE pf_coop.plot SET cooperative_ref=$1 WHERE tenant_id=$2 AND ref=$3`, f.coopA, f.tenant, ref)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "23514" {
			t.Fatalf("missing transfer revision/cursor: %v", err)
		}
	})
	for _, sql := range []string{
		`INSERT INTO pf_coop.plot_scope_tombstone SELECT * FROM pf_coop.plot_scope_tombstone`,
		`UPDATE pf_coop.plot_scope_tombstone SET revision=revision`,
		`DELETE FROM pf_coop.plot_scope_tombstone`,
		`TRUNCATE pf_coop.plot_scope_tombstone`,
		`SELECT pf_coop.capture_plot_scope_loss()`,
	} {
		t.Run(sql, func(t *testing.T) {
			tx, err := admin.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err = tx.Exec(ctx, `SET LOCAL ROLE r_cooperative`); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, sql)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "42501" {
				t.Fatalf("application DML/function must deny42501: %v", err)
			}
		})
	}
	for _, sql := range []string{`UPDATE pf_coop.plot_scope_tombstone SET revision=revision WHERE tenant_id=$1`, `DELETE FROM pf_coop.plot_scope_tombstone WHERE tenant_id=$1`, `TRUNCATE pf_coop.plot_scope_tombstone`} {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var args []any
		if sql[:8] != "TRUNCATE" {
			args = []any{f.tenant}
		}
		_, err = tx.Exec(ctx, sql, args...)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("append-only even for admin: %s: %v", sql, err)
		}
		_ = tx.Rollback(ctx)
	}
	// The trigger owner is NOLOGIN/NOBYPASSRLS and cannot read the projection.
	var safe, forced bool
	var cols int
	if err := admin.QueryRow(ctx, `SELECT NOT rolcanlogin AND NOT rolbypassrls AND NOT has_table_privilege('own_cooperative','pf_coop.plot_scope_tombstone','SELECT') FROM pg_roles WHERE rolname='own_cooperative'`).Scan(&safe); err != nil || !safe {
		t.Fatalf("trigger owner: %v %v", safe, err)
	}
	if err := admin.QueryRow(ctx, `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid='pf_coop.plot_scope_tombstone'::regclass`).Scan(&forced); err != nil || !forced {
		t.Fatalf("FORCE RLS: %v %v", forced, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='pf_coop' AND table_name='plot_scope_tombstone'`).Scan(&cols); err != nil || cols != 6 {
		t.Fatalf("ID-only columns: %d %v", cols, err)
	}
}

type tombstoneSavepoint struct{ tx pgx.Tx }

func (b tombstoneSavepoint) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return b.tx.Begin(ctx)
}

func TestPlotScopeTombstoneMigrationBackfill(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.national().createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	transferPlot(t, f, ref, f.coopB)
	// Duplicate historical ownership must still backfill one (owner,plot).
	transferPlot(t, f, ref, f.coopA)
	transferPlot(t, f, ref, f.coopB)
	draft, _ := f.national().createPlot("31117", f.coopA, rect(500, 0, 100, 100))
	transferPlot(t, f, draft, uuid.Nil)
	tx, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot := func() string {
		var got string
		err := tx.QueryRow(ctx, `SELECT md5((SELECT row_to_json(p)::text FROM pf_coop.plot p WHERE tenant_id=$1 AND ref=$2)
     ||(SELECT string_agg(row_to_json(e)::text,',' ORDER BY e.seq) FROM pf_coop.plot_event e WHERE tenant_id=$1 AND plot_ref=$2)
     ||(SELECT string_agg(row_to_json(o)::text,',' ORDER BY o.ref) FROM k_ref.org o WHERE tenant_id=$1))`, f.tenant, ref).Scan(&got)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := snapshot()
	var oldCursor int64
	if err = tx.QueryRow(ctx, `SELECT last_value FROM pf_coop.plot_sync_seq`).Scan(&oldCursor); err != nil {
		t.Fatal(err)
	}
	// Reconstruct only this new projection's pre-DDL state inside a private test
	// transaction. Rollback restores the installed projection; no source table,
	// source ledger, FORCE RLS policy or immutable source trigger is changed.
	if _, err = tx.Exec(ctx, `DROP FUNCTION pf_coop.capture_plot_scope_loss() CASCADE; DROP TABLE pf_coop.plot_scope_tombstone`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.node_id',$2,true),set_config('app.xa_ids','{sentinel}',true)`, f.tenant.String(), f.coopA.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE ag_migrator`); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.FS.ReadFile("pf/20261024050100_pf_coop_plot_scope_tombstone.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	var tid, node, xa string
	if err = tx.QueryRow(ctx, `SELECT current_setting('app.tenant_id'),current_setting('app.node_id'),current_setting('app.xa_ids')`).Scan(&tid, &node, &xa); err != nil || tid != f.tenant.String() || node != f.coopA.String() || xa != "{sentinel}" {
		t.Fatalf("context restored: %q %q %q %v", tid, node, xa, err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("backfill changed authoritative source: %s != %s", before, after)
	}
	var count int
	var newCursor int64
	if err = tx.QueryRow(ctx, `SELECT count(*),min(sync_seq) FROM pf_coop.plot_scope_tombstone WHERE tenant_id=$1 AND plot_ref=$2 AND previous_cooperative_ref=$3`, f.tenant, ref, f.coopA).Scan(&count, &newCursor); err != nil || count != 1 || newCursor <= oldCursor {
		t.Fatalf("fresh distinct old-owner cursor: count=%d new=%d old=%d err=%v", count, newCursor, oldCursor, err)
	}
	var drafts int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot_scope_tombstone WHERE tenant_id=$1 AND plot_ref=$2`, f.tenant, draft).Scan(&drafts); err != nil || drafts != 0 {
		t.Fatalf("visible draft must not backfill loss: %d %v", drafts, err)
	}
	// A queued source writer cannot allocate its new cursor while the atomic
	// projection upgrade holds the plot table lock.
	var sequenceBefore, sequenceAfter int64
	if err = tx.QueryRow(ctx, `SELECT last_value FROM pf_coop.plot_sync_seq`).Scan(&sequenceBefore); err != nil {
		t.Fatal(err)
	}
	writer, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.Exec(ctx, `UPDATE pf_coop.plot SET revision=revision+1,sync_seq=nextval('pf_coop.plot_sync_seq') WHERE tenant_id=$1 AND ref=$2`, f.tenant, ref)
	_ = writer.Rollback(ctx)
	var lockErr *pgconn.PgError
	if !errors.As(writeErr, &lockErr) || lockErr.Code != "55P03" {
		t.Fatalf("migration must block source writer: %v", writeErr)
	}
	if err = tx.QueryRow(ctx, `SELECT last_value FROM pf_coop.plot_sync_seq`).Scan(&sequenceAfter); err != nil || sequenceAfter != sequenceBefore {
		t.Fatalf("blocked writer allocated cursor: %d→%d %v", sequenceBefore, sequenceAfter, err)
	}
	svc := *f.mod.svc
	svc.DB = shareddb.ForModule(tombstoneSavepoint{tx}, shareddb.Module("pf/cooperative"))
	changes, last, err := (app.SyncSource{Svc: &svc}).Changes(syncCtx(f, f.coopA, coopport.RoleCoopManager), authz.Scope{Units: []string{"31117"}}, "plot", oldCursor, 100)
	if err != nil || len(changes) != 1 || changes[0].ID != ref || changes[0].Op != "tombstone" || changes[0].Data != nil || last != newCursor {
		t.Fatalf("advanced client healed: last=%d changes=%+v err=%v", last, changes, err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	var afterWrite int64
	if err = tx.QueryRow(ctx, `UPDATE pf_coop.plot SET revision=revision+1,sync_seq=nextval('pf_coop.plot_sync_seq') WHERE tenant_id=$1 AND ref=$2 RETURNING sync_seq`, f.tenant, ref).Scan(&afterWrite); err != nil || afterWrite <= newCursor {
		t.Fatalf("post-deploy writer ordering: %d <= %d: %v", afterWrite, newCursor, err)
	}
}
