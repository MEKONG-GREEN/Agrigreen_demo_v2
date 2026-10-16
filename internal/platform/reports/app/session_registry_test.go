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
	a, err := svc.kn(ctx, false)
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
		if _, err := svc.kn(ctx, true); !errors.Is(err, problem.Forbidden) {
			t.Fatalf("zero/incomplete projection fell back: %v", err)
		}
	}
	sentinel := errors.New("resolver failed")
	svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
		return port.SessionActor{}, false, sentinel
	}}
	if _, err := svc.kn(ctx, true); !errors.Is(err, sentinel) {
		t.Fatalf("dispatch resolver error fell back: %v", err)
	}
	if _, _, err := svc.formReader(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("form resolver error fell back: %v", err)
	}
}

func TestRegisteredReportSessionLegacyAndRealmCompatibility(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &authz.KNContext{Role: authz.KNTinh, Department: "legacy"}}
	svc := Service{ResolveSession: func(context.Context, authz.Principal, string) (port.SessionActor, bool, error) {
		return port.SessionActor{}, false, nil
	}}
	if a, err := svc.kn(authz.WithPrincipal(context.Background(), p), true); err != nil || a.dept() != "legacy" {
		t.Fatalf("legacy adapter: %+v %v", a, err)
	}
	p.KN = nil
	p.Roles = []string{port.RoleReportsAdmin}
	if _, scope, err := svc.formReader(authz.WithPrincipal(context.Background(), p)); err != nil || scope != nil {
		t.Fatalf("native realm authorization: %+v %v", scope, err)
	}
}
