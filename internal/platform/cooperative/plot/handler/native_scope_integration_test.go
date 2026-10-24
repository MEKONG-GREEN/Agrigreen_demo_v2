package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type nativeScopeProbe struct {
	pool  *pgxpool.Pool
	scope authz.Scope
	err   error
	calls atomic.Int64
}

func (s *nativeScopeProbe) Scope(ctx context.Context) (authz.Scope, error) {
	s.calls.Add(1)
	if s.err != nil {
		return authz.Scope{}, s.err
	}
	if s.pool != nil {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var n int
		if err := s.pool.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
			return authz.Scope{}, err
		}
	}
	return s.scope, nil
}

type nativeBeginProbe struct {
	tenant.Beginner
	before func()
	calls  atomic.Int64
}

type directNativeTx struct{ pgx.Tx }

func (tx directNativeTx) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return tx.Begin(ctx)
}

func (b *nativeBeginProbe) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	b.calls.Add(1)
	if b.before != nil {
		b.before()
	}
	return b.Beginner.BeginTx(ctx, opts)
}

// The resolver uses the exact same one-connection pool as plot. Resolving it
// after BeginTx would time out. Mutating the returned slice at BeginTx also
// verifies that the SQL scope was serialized before opening the transaction.
func TestNativeGISScopeResolvesBeforeSingleConnectionTx(t *testing.T) {
	g := newG2(t)
	plot, _ := g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
	cfg := g.pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	units := []string{"31117"}
	scope := &nativeScopeProbe{pool: pool, scope: authz.Scope{Units: units}}
	db := &nativeBeginProbe{Beginner: shareddb.ForModule(pool, coopDB), before: func() { units[0] = "31120" }}
	g.mod.svc.DB, g.mod.svc.Scope = db, scope
	ctx := syncCtx(g.fixture, g.orgA, syncport.RoleConflictResolver)
	changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{Units: []string{"31117"}}, "plot", 0, 100)
	if err != nil || len(changes) != 1 || changes[0].ID != plot {
		t.Fatalf("native scope before tx: %v %+v", err, changes)
	}
	if scope.calls.Load() != 1 || db.calls.Load() != 1 {
		t.Fatalf("resolver/transaction calls: %d/%d", scope.calls.Load(), db.calls.Load())
	}
}

func TestNativeGISMissingScopeFailsBeforeTransaction(t *testing.T) {
	for _, mode := range []string{"missing", "error"} {
		t.Run(mode, func(t *testing.T) {
			g := newG2(t)
			db := &nativeBeginProbe{Beginner: g.db}
			g.mod.svc.DB = db
			failure := errors.New("scope resolver unavailable")
			if mode == "missing" {
				g.mod.svc.Scope = nil
			} else {
				g.mod.svc.Scope = &nativeScopeProbe{err: failure}
			}
			ctx := syncCtx(g.fixture, g.orgA, coopport.RolePlotViewer)
			_, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{Units: []string{"31117"}}, "plot", 0, 100)
			if mode == "missing" {
				var p *problem.Problem
				if !errors.As(err, &p) || p.Code != "geo.dependency_unavailable" {
					t.Fatalf("missing scope port: %v", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("scope failure lost: %v", err)
			}
			if db.calls.Load() != 0 {
				t.Fatalf("opened %d transactions before resolving scope", db.calls.Load())
			}
		})
	}
}

func TestNativeGISRejectsScopeLookupInsideExistingTransaction(t *testing.T) {
	for _, mode := range []string{"joined", "direct"} {
		t.Run(mode, func(t *testing.T) {
			g := newG2(t)
			ctx := syncCtx(g.fixture, g.orgA, coopport.RolePlotViewer)
			tx, err := g.db.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			poison := &nativeScopeProbe{err: errors.New("scope resolver must not run inside another transaction")}
			g.mod.svc.Scope = poison
			if mode == "joined" {
				ctx = shareddb.JoinTx(ctx, tx)
			} else {
				g.mod.svc.DB = directNativeTx{Tx: tx}
			}
			_, _, err = g.mod.SyncSource().Changes(ctx, authz.Scope{Units: []string{"31117"}}, "plot", 0, 100)
			var p *problem.Problem
			if !errors.As(err, &p) || p.Code != "geo.dependency_unavailable" {
				t.Fatalf("native scope in existing tx: %v", err)
			}
			if poison.calls.Load() != 0 {
				t.Fatalf("resolver called inside existing tx %d times", poison.calls.Load())
			}
		})
	}
}

// These synthetic projections are negative tests only. The positive native
// regression uses the original realm context and real source/projection tree.
func TestNativeAliasScopePreservesKNAndSelectedProjection(t *testing.T) {
	for _, mode := range []string{"selected_session", "kn_context"} {
		t.Run(mode, func(t *testing.T) {
			g := newG2(t)
			g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
			poison := &nativeScopeProbe{err: errors.New("must not resolve a native scope for this session")}
			g.mod.svc.Scope = poison
			p := authz.Principal{ActorRef: ids.New(), TenantID: g.tenant, OrgRef: g.orgA, Roles: []string{coopport.RolePlotViewer}}
			if mode == "selected_session" {
				selected, err := authz.NewSession("native_test", ids.New(), authz.SessionScope{
					NodeID: g.orgB, UnitCodes: []string{"31120"}, RequiresNode: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				p.SelectedSession = &selected
			} else {
				p.KN = &authz.KNContext{ID: ids.New(), Role: authz.KNXa, OrgRef: g.orgB,
					Communes: []string{"31120"}, Scope: authz.Scope{Units: []string{"31120"}}}
			}
			ctx := authz.WithPrincipal(hubCtx(g.fixture), p)
			changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
			if err != nil || len(changes) != 0 {
				t.Fatalf("selected projection was broadened: %v %+v", err, changes)
			}
			if poison.calls.Load() != 0 {
				t.Fatalf("selected projection invoked native resolver %d times", poison.calls.Load())
			}
		})
	}
}

func TestNativeGISUnprojectedActorRemainsDenied(t *testing.T) {
	g := newG2(t)
	g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
	unknown := ids.New()
	g.scope.set(unknown, authz.Scope{Units: []string{"31117"}})
	ctx := syncCtx(g.fixture, unknown, coopport.RolePlotEditor)
	changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{Units: []string{"31117"}}, "plot", 0, 100)
	if err != nil || len(changes) != 0 {
		t.Fatalf("unprojected actor read plots: %v %+v", err, changes)
	}
	r := g.as(g.tenant, unknown, coopport.RolePlotEditor).appDo(http.MethodPost, "/v1/geo/cooperative-boundaries", map[string]any{
		"cooperative_ref": g.coopB.String(), "geometry": rect(2000, 0, 500, 500),
	})
	if r.status < http.StatusBadRequest {
		t.Fatalf("unprojected actor wrote boundary: %d %s", r.status, r.raw)
	}
	if n := outboxCount(t, g.fixture, coopport.EventCooperativeBoundaryRecorded, "cooperative_ref", g.coopB.String()); n != 0 {
		t.Fatalf("unprojected actor emitted boundary events: %d", n)
	}
}

func TestNativeGISAllScopeKeepsProjectedNode(t *testing.T) {
	g := newG2(t)
	g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
	g.scope.set(g.orgA, authz.Scope{All: true, Units: []string{"31117"}})
	ctx := syncCtx(g.fixture, g.orgA, coopport.RolePlotViewer)
	changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
	if err != nil || len(changes) != 0 {
		t.Fatalf("All scope bypassed independent HTX/node restriction: %v %+v", err, changes)
	}
}

func TestNativeGISForeignContextKeepsExistingProjection(t *testing.T) {
	g := newG2(t)
	g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
	scope := &nativeScopeProbe{scope: authz.Scope{Units: []string{"31117"}}}
	g.mod.svc.Scope = scope
	ctx := tenant.WithID(syncCtx(g.fixture, g.orgA, syncport.RoleConflictResolver), ids.New())
	before := shareddb.ContextVarsFrom(ctx)
	// Changes targets the principal tenant, while PlotStats targets the explicit
	// context tenant. Neither may add native communes across this mismatch.
	changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
	if err != nil || len(changes) != 0 {
		t.Fatalf("foreign context gained principal commune scope: %v %+v", err, changes)
	}
	stats, err := g.mod.PlotStats().DigitizedByCommune(ctx, []string{"31117"})
	if err != nil || len(stats) != 0 {
		t.Fatalf("foreign context commune stats: %v %+v", err, stats)
	}
	byCoop, err := g.mod.PlotStats().DigitizedByCooperative(ctx, []uuid.UUID{g.coopA})
	if err != nil || len(byCoop) != 0 {
		t.Fatalf("foreign context cooperative stats: %v %+v", err, byCoop)
	}
	if scope.calls.Load() != 0 || shareddb.ContextVarsFrom(ctx).NodeID != before.NodeID {
		t.Fatalf("foreign context replaced projection: calls=%d", scope.calls.Load())
	}
}

func TestNativeGISIncompletePrincipalDoesNotResolveScope(t *testing.T) {
	for _, mode := range []string{"missing_actor", "missing_tenant"} {
		t.Run(mode, func(t *testing.T) {
			g := newG2(t)
			g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
			scope := &nativeScopeProbe{scope: authz.Scope{Units: []string{"31117"}}}
			g.mod.svc.Scope = scope
			p := authz.Principal{ActorRef: ids.New(), TenantID: g.tenant, OrgRef: g.orgA, Roles: []string{syncport.RoleConflictResolver}}
			if mode == "missing_actor" {
				p.ActorRef = uuid.Nil
			} else {
				p.TenantID = uuid.Nil
			}
			ctx := authz.WithPrincipal(hubCtx(g.fixture), p)
			changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
			if mode == "missing_tenant" {
				if !errors.Is(err, tenant.ErrMissing) {
					t.Fatalf("missing principal tenant: %v", err)
				}
			} else if err != nil || len(changes) != 0 {
				t.Fatalf("missing actor gained native scope: %v %+v", err, changes)
			}
			if scope.calls.Load() != 0 {
				t.Fatalf("incomplete principal invoked scope port %d times", scope.calls.Load())
			}
		})
	}
}

func TestNativeGISKNRealmWithoutSessionKeepsExistingProjection(t *testing.T) {
	g := newG2(t)
	g.national().createPlot("31117", g.coopA, rect(0, 0, 100, 100))
	scope := &nativeScopeProbe{scope: authz.Scope{Units: []string{"31117"}}}
	g.mod.svc.Scope = scope
	ctx := syncCtx(g.fixture, g.orgA, "kn_user", coopport.RolePlotEditor)
	before := shareddb.ContextVarsFrom(ctx)
	changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
	if err != nil || len(changes) != 0 {
		t.Fatalf("KN realm without selected context gained native scope: %v %+v", err, changes)
	}
	if scope.calls.Load() != 0 || shareddb.ContextVarsFrom(ctx).NodeID != before.NodeID {
		t.Fatalf("KN realm replaced its existing projection: calls=%d", scope.calls.Load())
	}
}

func TestNativeGISEmptyScopeCannotReadDraft(t *testing.T) {
	for _, mode := range []string{"nil_units", "empty_units"} {
		t.Run(mode, func(t *testing.T) {
			g := newG2(t)
			_, created := g.national().createPlot("31117", uuid.Nil, rect(0, 0, 100, 100))
			if got := str(created.body, "plot", "properties", "status"); got != coopport.PlotDraft {
				t.Fatalf("fixture is not an unassigned draft: %q", got)
			}
			scope := &nativeScopeProbe{}
			if mode == "empty_units" {
				scope.scope.Units = []string{}
			}
			db := &nativeBeginProbe{Beginner: g.mod.svc.DB}
			g.mod.svc.DB, g.mod.svc.Scope = db, scope
			ctx := syncCtx(g.fixture, g.orgA, syncport.RoleConflictResolver)
			denied := func(err error) {
				t.Helper()
				var p *problem.Problem
				if !errors.As(err, &p) || p.Code != "geo.out_of_scope" || p.Status != http.StatusForbidden {
					t.Fatalf("empty server scope did not fail closed: %v", err)
				}
			}
			// A caller-supplied All scope cannot override the empty server scope.
			changes, _, err := g.mod.SyncSource().Changes(ctx, authz.Scope{All: true}, "plot", 0, 100)
			denied(err)
			if len(changes) != 0 {
				t.Fatalf("empty scope emitted draft changes: %+v", changes)
			}
			stats, err := g.mod.PlotStats().DigitizedByCommune(ctx, nil)
			denied(err)
			if len(stats) != 0 {
				t.Fatalf("empty scope counted draft plots: %+v", stats)
			}
			byCoop, err := g.mod.PlotStats().DigitizedByCooperative(ctx, []uuid.UUID{g.coopA})
			denied(err)
			if len(byCoop) != 0 {
				t.Fatalf("empty scope counted cooperative plots: %+v", byCoop)
			}
			if db.calls.Load() != 0 || scope.calls.Load() != 3 {
				t.Fatalf("empty scope did not stop before transaction: begins=%d resolver=%d", db.calls.Load(), scope.calls.Load())
			}
		})
	}
}
