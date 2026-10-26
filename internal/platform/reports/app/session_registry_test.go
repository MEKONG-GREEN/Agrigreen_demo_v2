package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"testing"
)

func TestRegisteredReportSessionIdentityAndFilters(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New()}
	ctx := authz.WithPrincipal(context.Background(), p)
	behalf := uuid.New()
	svc := Service{ResolveSession: func(_ context.Context, got authz.Principal, op string) (port.SessionActor, bool, error) {
		if got.ActorRef != p.ActorRef || got.TenantID != p.TenantID {
			t.Fatal("principal identity changed")
		}
		if op != port.SessionReadDispatch && op != port.SessionReadForms {
			t.Fatalf("unexpected operation %s", op)
		}
		return port.SessionActor{Allowed: true, Department: "department", Province: "89", AllowsDepartment: func(d string) bool { return d == "department" }, OnBehalfOf: &behalf}, true, nil
	}}
	a, err := svc.resolveSessionActor(ctx, false)
	if err != nil || a.p.ActorRef != p.ActorRef || a.p.TenantID != p.TenantID {
		t.Fatalf("registered actor: %+v %v", a, err)
	}
	if !a.allows("89", "department") || a.allows("91", "department") || a.allows("89", "other") || a.scope.UnmaskRecipients {
		t.Fatal("scope or PII policy lost")
	}
	copy := a.onBehalf()
	*copy = uuid.Nil
	if behalf == uuid.Nil {
		t.Fatal("on-behalf reference escaped by pointer")
	}
	got, scope, err := svc.formReader(ctx)
	if err != nil || got.TenantID != p.TenantID || !formDeptAllows(scope, "department") || formDeptAllows(scope, "other") {
		t.Fatalf("form projection: %+v %v", scope, err)
	}
}

func TestRegisteredReportSessionDenialCannotFallBack(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &authz.KNContext{Role: authz.KNTinh, Department: "legacy"}}
	ctx := authz.WithPrincipal(context.Background(), p)
	for _, actor := range []port.SessionActor{{}, {Allowed: true}, {Allowed: true, AllowsDepartment: func(string) bool { return true }}} {
		svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
			return actor, true, nil
		}}
		if _, err := svc.resolveSessionActor(ctx, true); !errors.Is(err, problem.Forbidden) {
			t.Fatalf("zero/incomplete projection fell back: %v", err)
		}
	}
	sentinel := errors.New("resolver failed")
	svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
		return port.SessionActor{}, false, sentinel
	}}
	if _, err := svc.resolveSessionActor(ctx, true); !errors.Is(err, sentinel) {
		t.Fatalf("dispatch resolver error fell back: %v", err)
	}
	if _, _, err := svc.formReader(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("form resolver error fell back: %v", err)
	}
}

func TestRegisteredReportSessionNativeRealmCompatibility(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: []string{port.RoleReportsAdmin}}
	svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
		return port.SessionActor{}, false, nil
	}}
	if _, scope, err := svc.formReader(authz.WithPrincipal(context.Background(), p)); err != nil || scope != nil {
		t.Fatalf("native realm authorization: %+v %v", scope, err)
	}
}

func TestReportSessionAnonymousDenied(t *testing.T) {
	svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
		t.Fatal("anonymous request reached session resolver")
		return port.SessionActor{}, false, nil
	}}
	if _, err := svc.resolveSessionActor(context.Background(), false); !errors.Is(err, problem.Unauthenticated) {
		t.Fatalf("anonymous dispatch: %v", err)
	}
	if _, _, err := svc.formReader(context.Background()); !errors.Is(err, problem.Unauthenticated) {
		t.Fatalf("anonymous forms: %v", err)
	}
}

func TestReportSessionRequiresRegistration(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &authz.KNContext{Role: authz.KNTinh, Department: "legacy"}}
	ctx := authz.WithPrincipal(context.Background(), p)
	for _, tc := range []struct {
		name    string
		resolve port.SessionResolver
	}{
		{name: "missing"},
		{name: "unhandled", resolve: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
			return port.SessionActor{}, false, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{ResolveSession: tc.resolve}
			for _, write := range []bool{false, true} {
				if _, err := svc.resolveSessionActor(ctx, write); !errors.Is(err, problem.Forbidden) {
					t.Errorf("unregistered dispatch write=%v inferred KN access: %v", write, err)
				}
			}
			if _, _, err := svc.formReader(ctx); !errors.Is(err, problem.Forbidden) {
				t.Errorf("unregistered forms inferred KN access: %v", err)
			}
		})
	}
}

// Even a native administrator role cannot override a selected application denial.
func TestRegisteredFormSessionPrecedesRealmPrivileges(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: []string{authz.RolePlatformAdmin, port.RoleReportsAdmin}}
	ctx := authz.WithPrincipal(context.Background(), p)
	sentinel := errors.New("owner payload invalid")
	for _, tc := range []struct {
		name  string
		actor port.SessionActor
		err   error
		want  error
	}{
		{name: "owner error", err: sentinel, want: sentinel},
		{name: "explicit denial", want: problem.Forbidden},
		{name: "missing scope", actor: port.SessionActor{Allowed: true}, want: problem.Forbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
				return tc.actor, true, tc.err
			}}
			if _, _, err := svc.formReader(ctx); !errors.Is(err, tc.want) {
				t.Fatalf("realm bypassed selected owner: %v", err)
			}
		})
	}
	calls := 0
	svc := Service{ResolveSession: func(_ context.Context, got authz.Principal, op string) (port.SessionActor, bool, error) {
		calls++
		if got.ActorRef != p.ActorRef || got.TenantID != p.TenantID || !got.Has(authz.RolePlatformAdmin) || op != port.SessionReadForms {
			t.Fatal("identity or operation changed")
		}
		return port.SessionActor{Allowed: true, Department: "selected", AllowsDepartment: func(d string) bool { return d == "selected" }}, true, nil
	}}
	got, scope, err := svc.formReader(ctx)
	if err != nil || calls != 1 || got.ActorRef != p.ActorRef || scope == nil || !formDeptAllows(scope, "selected") || formDeptAllows(scope, "other") {
		t.Fatalf("selected scope lost: %+v %v", scope, err)
	}
}
