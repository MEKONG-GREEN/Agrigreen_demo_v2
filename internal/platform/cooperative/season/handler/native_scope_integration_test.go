package handler_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/app"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type nativeSeasonScopeFunc func(context.Context) (authz.Scope, error)

func (f nativeSeasonScopeFunc) Scope(ctx context.Context) (authz.Scope, error) { return f(ctx) }

// Native season consumers use fromCtx without app-level filtering. The DB must
// receive the server's commune scope, retain its ready-node requirement, and
// leave independently resolved KN/selected projections authoritative.
func TestNativeSeasonScopeReachesRLSWithoutReplacingSessionScope(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP native scope", "OM5451")
	f.managerA().mustOpen(f.p1)
	f.managerB().mustOpen(f.p4)
	svc := f.mod.svc
	original := svc.Scope
	defer func() { svc.Scope = original }()
	seasons := app.SeasonsPort{Svc: svc}
	p := authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, OrgRef: f.knA, Roles: []string{partyport.RoleKNOfficer}}
	ctxFor := func(p authz.Principal) context.Context {
		return tenant.WithID(authz.WithPrincipal(context.Background(), p), p.TenantID)
	}
	read := func(t *testing.T, ctx context.Context, want uuid.UUID) {
		t.Helper()
		before := shareddb.ContextVarsFrom(ctx).Settings()
		got, err := seasons.HarvestDates(ctx, []uuid.UUID{f.p1, f.p4})
		if err != nil {
			t.Fatal(err)
		}
		if want == uuid.Nil {
			if len(got) != 0 {
				t.Fatalf("closed scope exposed %+v", got)
			}
		} else if len(got) != 1 || got[0].PlotRef != want {
			t.Fatalf("plots=%+v want only %s", got, want)
		}
		if !reflect.DeepEqual(before, shareddb.ContextVarsFrom(ctx).Settings()) {
			t.Fatal("transaction changed the caller projection")
		}
	}
	t.Run("native commune positive", func(t *testing.T) { read(t, ctxFor(p), f.p1) })
	t.Run("empty native scope denies a read port without app filtering", func(t *testing.T) {
		svc.Scope = nativeSeasonScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{}, nil })
		defer func() { svc.Scope = original }()
		if _, err := seasons.HarvestDates(ctxFor(p), []uuid.UUID{f.p1, f.p4}); !errors.Is(err, app.ErrOutOfScope) {
			t.Fatalf("empty scope read port: %v", err)
		}
	})
	t.Run("unprojected actor stays closed despite unit scope", func(t *testing.T) {
		missing := p
		missing.OrgRef = ids.New()
		svc.Scope = nativeSeasonScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{Units: []string{"31117"}}, nil })
		defer func() { svc.Scope = original }()
		read(t, ctxFor(missing), uuid.Nil)
	})
	t.Run("other tenant cannot reuse actor or unit scope", func(t *testing.T) {
		other := p
		other.TenantID = ids.New()
		svc.Scope = nativeSeasonScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{Units: []string{"31117"}}, nil })
		defer func() { svc.Scope = original }()
		read(t, ctxFor(other), uuid.Nil)
	})
	t.Run("foreign target cannot acquire native units", func(t *testing.T) {
		svc.Scope = nil
		defer func() { svc.Scope = original }()
		read(t, tenant.WithID(ctxFor(p), ids.New()), uuid.Nil)
	})
	t.Run("all scope does not remove native node", func(t *testing.T) {
		svc.Scope = nativeSeasonScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{All: true}, nil })
		defer func() { svc.Scope = original }()
		read(t, ctxFor(p), uuid.Nil)
	})
	nodeB := ids.New()
	if _, err := pgtest.Get(t).AdminPool(t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_party.org
      (tenant_id,ref,kind,code,name,admin_unit_code,active,last_event_id,last_seq)
      VALUES ($1,$2,'extension_team','TKN-B','Tổ KN xã 31120','31120',true,$3,1)`, f.tenant, nodeB, ids.New()); err != nil {
		t.Fatal(err)
	}
	t.Run("KN projection wins without extra scope port", func(t *testing.T) {
		kn := p
		kn.KN = &authz.KNContext{ID: ids.New(), Role: authz.KNXa, OrgRef: nodeB, Department: "trong_trot", Communes: []string{"31120"}}
		svc.Scope = nil
		defer func() { svc.Scope = original }()
		read(t, ctxFor(kn), f.p4)
	})
	t.Run("selected projection wins without extra scope port", func(t *testing.T) {
		selected, err := authz.NewSession("fixture", ids.New(), authz.SessionScope{NodeID: nodeB, Department: "trong_trot", UnitCodes: []string{"31120"}, RequiresNode: true})
		if err != nil {
			t.Fatal(err)
		}
		session := p
		session.SelectedSession = &selected
		svc.Scope = nil
		defer func() { svc.Scope = original }()
		read(t, ctxFor(session), f.p4)
	})
	for _, tc := range []struct {
		name   string
		change func(*authz.Principal)
	}{
		{"missing KN node does not fall back", func(p *authz.Principal) {
			p.KN = &authz.KNContext{ID: ids.New(), Role: authz.KNXa, Communes: []string{"31117"}}
		}},
		{"invalid selected session does not fall back", func(p *authz.Principal) { p.SelectedSession = &authz.Session{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := p
			tc.change(&input)
			svc.Scope = nil
			defer func() { svc.Scope = original }()
			if _, err := seasons.HarvestDates(ctxFor(input), []uuid.UUID{f.p1, f.p4}); !errors.Is(err, shareddb.ErrNodeScopeRequired) {
				t.Fatalf("want node guard, got %v", err)
			}
		})
	}
	t.Run("native scope after selected request", func(t *testing.T) { read(t, ctxFor(p), f.p1) })
}
