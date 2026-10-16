package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestRegisteredActorScopeAndIdentity(t *testing.T) {
	allowed, other, outside := ids.New(), ids.New(), ids.New()
	principal := authz.Principal{ActorRef: ids.New(), TenantID: ids.New()}
	profile := fxProfile
	profile.ResolveActor = func(_ context.Context, _ authz.Principal, _ port.Profile) (port.ResolvedActor, bool, error) {
		return port.ResolvedActor{Role: "xa", Department: "operations", Scope: authz.Scope{Units: []string{"A"}}, Node: allowed,
			AllowsTarget: func(_ string, ref uuid.UUID) bool { return ref == allowed || ref == outside }}, true, nil
	}
	svc, err := NewService(nil, nil, nil, []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	_, actor, err := svc.resolve(authz.WithPrincipal(context.Background(), principal), profile.Name)
	if err != nil {
		t.Fatal(err)
	}
	if actor.ref != principal.ActorRef || actor.tenant != principal.TenantID || actor.node != allowed || !actor.leaf {
		t.Fatalf("incorrect projection: %+v", actor)
	}
	spec := domain.Spec{UnitKind: port.UnitFacility, UnitParents: []string{"A", "A", "B"}}
	units := []string{allowed.String(), other.String(), outside.String()}
	if !actor.allowsUnit(spec, units, units[0]) || actor.allowsUnit(spec, units, units[1]) || actor.allowsUnit(spec, units, units[2]) {
		t.Fatal("registered target/scope restriction bypassed")
	}
	if _, err := json.Marshal(profile); err != nil {
		t.Fatalf("server callback leaked into API: %v", err)
	}
}

func TestRegisteredActorErrorCannotFallBackToRealm(t *testing.T) {
	profile := fxProfile
	profile.ResolveActor = func(context.Context, authz.Principal, port.Profile) (port.ResolvedActor, bool, error) {
		return port.ResolvedActor{}, false, problem.Forbidden
	}
	svc, err := NewService(nil, nil, nil, []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	principal := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{"admin"}}
	if _, _, err := svc.resolve(authz.WithPrincipal(context.Background(), principal), profile.Name); !isProblem(err, problem.Forbidden.Code) {
		t.Fatalf("fell back after denial: %v", err)
	}
	if _, _, err := svc.resolve(context.Background(), profile.Name); !isProblem(err, problem.Unauthenticated.Code) {
		t.Fatalf("anonymous resolver access: %v", err)
	}
}

func TestRegisteredActorRealmCompatibility(t *testing.T) {
	profile := fxProfile
	profile.ResolveActor = func(context.Context, authz.Principal, port.Profile) (port.ResolvedActor, bool, error) {
		return port.ResolvedActor{}, false, nil
	}
	svc, err := NewService(nil, nil, nil, []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	_, actor, err := svc.resolve(authz.WithPrincipal(context.Background(), authz.Principal{Roles: []string{"tw"}}), profile.Name)
	if err != nil || actor.level != "TW" || !actor.scope.All {
		t.Fatalf("realm compatibility: %+v %v", actor, err)
	}
}

// A real DB capability call must use the registry even without any legacy session or realm roles.
func TestRegisteredActorDatabaseTenantIsolation(t *testing.T) {
	f := newFx(t)
	profile := fxProfile
	profile.ResolveActor = func(context.Context, authz.Principal, port.Profile) (port.ResolvedActor, bool, error) {
		return port.ResolvedActor{Role: "admin", Scope: authz.Scope{All: true}}, true, nil
	}
	svc, err := NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "registered service")
	actor := ids.New()
	makeContext := func(tenant uuid.UUID) context.Context {
		return authz.WithPrincipal(f.ctx, authz.Principal{ActorRef: actor, TenantID: tenant})
	}
	templates, err := svc.InstallFixtures(makeContext(f.tid), profile.Name, "")
	f.must(err, "registered template writes")
	if len(templates) == 0 {
		t.Fatal("no template persisted")
	}
	own, err := svc.Templates(makeContext(f.tid), profile.Name)
	f.must(err, "own tenant read")
	foreign, err := svc.Templates(makeContext(ids.New()), profile.Name)
	f.must(err, "other tenant read")
	if len(own) != len(templates) || len(foreign) != 0 {
		t.Fatalf("tenant isolation: own=%d other=%d", len(own), len(foreign))
	}
}
