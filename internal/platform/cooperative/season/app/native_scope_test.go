package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type nativeScopeFunc func(context.Context) (authz.Scope, error)

func (f nativeScopeFunc) Scope(ctx context.Context) (authz.Scope, error) { return f(ctx) }

type nativeScopeDB struct {
	begins int
	active bool
	xaIDs  []string
}

func (b *nativeScopeDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	b.begins++
	b.active = true
	return &nativeScopeTxFake{b: b}, nil
}

type nativeScopeTxFake struct {
	pgx.Tx
	b *nativeScopeDB
}

func (tx *nativeScopeTxFake) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if sql == "SELECT set_config('app.xa_ids', $1, true)" {
		tx.b.xaIDs = append(tx.b.xaIDs, args[0].(string))
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (tx *nativeScopeTxFake) Commit(context.Context) error   { tx.b.active = false; return nil }
func (tx *nativeScopeTxFake) Rollback(context.Context) error { tx.b.active = false; return nil }

// This explicit reusable wrapper implements both interfaces. Plain pgx.Tx
// only has Begin(ctx), not tenant.Beginner.BeginTx(ctx, opts).
type nativeScopeReusableTx struct{ pgx.Tx }

func (tx nativeScopeReusableTx) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return tx.Tx.Begin(ctx)
}

func nativeScopeContext(p authz.Principal) context.Context {
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), p.TenantID)
}

func TestNativeScopeIsResolvedBeforeBothTransactionBoundaries(t *testing.T) {
	for _, role := range knRoles {
		t.Run(role, func(t *testing.T) {
			for _, entry := range []string{"fromCtx", "inTx"} {
				t.Run(entry, func(t *testing.T) {
					p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: []string{role}}
					ctx := nativeScopeContext(p)
					b := &nativeScopeDB{}
					calls := 0
					s := &Service{DB: b, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) {
						calls++
						if b.active || b.begins != 0 {
							t.Fatal("scope I/O ran after BeginTx")
						}
						return authz.Scope{Units: []string{"31117", `quoted"unit`}}, nil
					})}
					cb := func(pgx.Tx) error {
						if !b.active || len(b.xaIDs) != 1 {
							t.Fatal("scope was not bound before business SQL")
						}
						return nil
					}
					var err error
					if entry == "fromCtx" {
						err = s.fromCtx(ctx, cb)
					} else {
						err = s.inTx(ctx, p.TenantID, cb)
					}
					if err != nil || calls != 1 || b.begins != 1 || b.active || len(b.xaIDs) != 1 || b.xaIDs[0] != `{"31117","quoted\"unit"}` {
						t.Fatalf("err=%v calls=%d begins=%d xa=%q", err, calls, b.begins, b.xaIDs)
					}
				})
			}
		})
	}
}

func TestNativeScopeFailureClosesBeforeBegin(t *testing.T) {
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: []string{partyport.RoleKNOfficer}}
	ctx := nativeScopeContext(p)
	failure := errors.New("scope unavailable")
	for _, tc := range []struct {
		name  string
		scope partyport.ScopeResolver
	}{
		{"missing", nil},
		{"error", nativeScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{}, failure })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &nativeScopeDB{}
			s := &Service{DB: b, Scope: tc.scope}
			if err := s.inTx(ctx, p.TenantID, func(pgx.Tx) error { t.Fatal("business SQL ran without scope"); return nil }); err == nil || b.begins != 0 {
				t.Fatalf("err=%v begins=%d", err, b.begins)
			}
		})
	}
	t.Run("joined foreign context still forbids scope port I/O", func(t *testing.T) {
		b := &nativeScopeDB{}
		calls := 0
		s := &Service{DB: b, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) { calls++; return authz.Scope{}, nil })}
		joined := shareddb.JoinTx(tenant.WithID(ctx, ids.New()), &nativeScopeTxFake{b: b})
		if _, err := s.access(joined, readRoles...); err == nil || calls != 0 || b.begins != 0 {
			t.Fatalf("err=%v calls=%d begins=%d", err, calls, b.begins)
		}
	})
	t.Run("direct reusable transaction", func(t *testing.T) {
		b := &nativeScopeDB{}
		calls := 0
		s := &Service{DB: nativeScopeReusableTx{Tx: &nativeScopeTxFake{b: b}}, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) { calls++; return authz.Scope{}, nil })}
		if err := s.inTx(ctx, p.TenantID, func(pgx.Tx) error { return nil }); err == nil || calls != 0 || b.begins != 0 {
			t.Fatalf("err=%v calls=%d begins=%d", err, calls, b.begins)
		}
	})
	t.Run("joined", func(t *testing.T) {
		b := &nativeScopeDB{}
		calls := 0
		s := &Service{DB: b, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) { calls++; return authz.Scope{}, nil })}
		joined := shareddb.JoinTx(ctx, &nativeScopeTxFake{b: b})
		if err := s.fromCtx(joined, func(pgx.Tx) error { return nil }); err == nil || calls != 0 || b.begins != 0 {
			t.Fatalf("err=%v calls=%d begins=%d", err, calls, b.begins)
		}
		if _, err := s.access(joined, readRoles...); err == nil || calls != 0 {
			t.Fatalf("access resolved a native port inside joined tx: err=%v calls=%d", err, calls)
		}
	})
}

func TestNativeScopeNeverReplacesOtherCallerProjections(t *testing.T) {
	base := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: []string{partyport.RoleKNOfficer}}
	invalid := authz.Session{}
	cases := []struct {
		name    string
		change  func(*authz.Principal)
		foreign bool
	}{
		{"manager", func(p *authz.Principal) { p.Roles = append(p.Roles, port.RoleCoopManager) }, false},
		{"farmer", func(p *authz.Principal) { p.Roles = append(p.Roles, port.RoleFarmer) }, false},
		{"partner", func(p *authz.Principal) { p.Roles = append(p.Roles, partyport.RolePartnerSync) }, false},
		{"api", func(p *authz.Principal) { p.Roles = append(p.Roles, "api_client") }, false},
		{"admin", func(p *authz.Principal) { p.Roles = append(p.Roles, authz.RolePlatformAdmin) }, false},
		{"job", func(p *authz.Principal) { p.Roles = nil }, false},
		{"no actor", func(p *authz.Principal) { p.ActorRef = uuid.Nil }, false},
		{"KN realm without context", func(p *authz.Principal) { p.Roles = append(p.Roles, "kn_user") }, false},
		{"no node", func(p *authz.Principal) { p.OrgRef = uuid.Nil }, false},
		{"KN", func(p *authz.Principal) {
			p.KN = &authz.KNContext{Role: authz.KNXa, OrgRef: ids.New(), Communes: []string{"31120"}}
		}, false},
		{"selected", func(p *authz.Principal) { p.SelectedSession = &invalid }, false},
		{"foreign target", func(*authz.Principal) {}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.change(&p)
			ctx := nativeScopeContext(p)
			tid := p.TenantID
			if tc.foreign {
				tid = ids.New()
				ctx = tenant.WithID(ctx, tid)
			}
			b := &nativeScopeDB{}
			// The new guard must not reject existing non-native joined callers.
			ctx = shareddb.JoinTx(ctx, &nativeScopeTxFake{b: b})
			s := &Service{DB: b, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) {
				t.Fatal("unexpected scope lookup")
				return authz.Scope{}, nil
			})}
			before := shareddb.ContextVarsFrom(ctx)
			if err := s.inTx(ctx, tid, func(pgx.Tx) error { return nil }); err != nil || len(b.xaIDs) != 0 || b.begins != 1 {
				t.Fatalf("err=%v begins=%d xa=%q", err, b.begins, b.xaIDs)
			}
			after := shareddb.ContextVarsFrom(ctx)
			if before.NodeID != after.NodeID || before.Dept != after.Dept || before.ActorRef != after.ActorRef {
				t.Fatal("caller projection changed")
			}
		})
	}
}

func TestNativeAllScopeDoesNotClearNodeOrBecomeTenantWide(t *testing.T) {
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: []string{partyport.RoleKNNationalAdmin}}
	ctx := nativeScopeContext(p)
	b := &nativeScopeDB{}
	s := &Service{DB: b, Scope: nativeScopeFunc(func(context.Context) (authz.Scope, error) {
		return authz.Scope{All: true, Units: []string{"31117"}}, nil
	})}
	if err := s.inTx(ctx, p.TenantID, func(pgx.Tx) error { return nil }); err != nil || len(b.xaIDs) != 0 || shareddb.ContextVarsFrom(ctx).NodeID != p.OrgRef {
		t.Fatalf("err=%v xa=%v", err, b.xaIDs)
	}
}
