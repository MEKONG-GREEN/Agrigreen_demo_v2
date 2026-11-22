package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type unitDelegationVerdict struct {
	port.UnitDelegationPolicy
	handled bool
	err     error
	grant   port.DelegationGrantDecision
	people  port.PeopleScopeDecision
}

func (v unitDelegationVerdict) AuthorizeDelegationGrant(context.Context, string) (port.DelegationGrantDecision, bool, error) {
	return v.grant, v.handled, v.err
}
func (v unitDelegationVerdict) AuthorizeDelegationRevoke(context.Context) (bool, error) {
	return v.handled, v.err
}
func (v unitDelegationVerdict) PeopleScope(context.Context) (port.PeopleScopeDecision, bool, error) {
	return v.people, v.handled, v.err
}

// A configured owner cannot silently opt a selected or partially bound request
// into the permissive compatibility branch, even with a legacy manager present.
func TestUnitDelegationConfiguredVerdictsAreFinal(t *testing.T) {
	selected, err := authz.NewSession("synthetic_owner", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("owner denied")
	for _, tc := range []struct {
		name   string
		policy unitDelegationVerdict
		grant  error
		revoke error
		people error
	}{
		{"unhandled", unitDelegationVerdict{}, problem.Forbidden, problem.Forbidden, problem.Forbidden},
		{"owner error", unitDelegationVerdict{handled: true, err: denied}, denied, denied, denied},
		{"error even when unhandled", unitDelegationVerdict{err: denied}, denied, denied, denied},
		{"attributes revoked", unitDelegationVerdict{handled: true, err: port.ErrOwnerAttributesRevoked}, port.ErrOwnerAttributesRevoked, port.ErrOwnerAttributesRevoked, app.ErrContextRevoked},
	} {
		for _, withSelection := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/partial", true: "/selected"}[withSelection], func(t *testing.T) {
				database := &unitPolicyDB{err: errors.New("unexpected database access")}
				svc := app.Service{DB: database, UnitDelegations: tc.policy}
				p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), KN: &authz.KNContext{
					Role: port.KNTinh, Manager: true, Department: "trong_trot", UnitRef: uuid.New(), InternalUnit: "phong_tt"}}
				if withSelection {
					p.SelectedSession = &selected
				}
				ctx := authz.WithPrincipal(context.Background(), p)
				_, grantErr := svc.CreateDelegation(ctx, domain.DelegationInput{}, "owner-verdict")
				_, revokeErr := svc.RevokeDelegation(ctx, uuid.New(), "Thu hồi theo quyết định", "owner-verdict", false)
				_, peopleErr := svc.PeopleInScope(ctx)
				if !matchesUnitDelegationError(grantErr, tc.grant) || !matchesUnitDelegationError(revokeErr, tc.revoke) || !matchesUnitDelegationError(peopleErr, tc.people) || database.calls != 0 {
					t.Fatalf("grant=%v revoke=%v people=%v db=%d", grantErr, revokeErr, peopleErr, database.calls)
				}
			})
		}
	}
}

func matchesUnitDelegationError(got, want error) bool {
	var p *problem.Problem
	return errors.Is(got, want) || errors.As(want, &p) && problem.From(got).Code == p.Code
}

func TestUnitDelegationUnhandledNativeKeepsContextRequired(t *testing.T) {
	svc := app.Service{UnitDelegations: unitDelegationVerdict{}}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: []string{authz.RolePlatformAdmin}})
	_, grantErr := svc.CreateDelegation(ctx, domain.DelegationInput{}, "native-verdict")
	_, revokeErr := svc.RevokeDelegation(ctx, uuid.New(), "Thu hồi theo quyết định", "native-verdict", false)
	_, peopleErr := svc.PeopleInScope(ctx)
	for _, err := range []error{grantErr, revokeErr, peopleErr} {
		if !errors.Is(err, authz.ErrNoKNContext) {
			t.Fatalf("native result changed: %v", err)
		}
	}
}

func TestUnitDelegationIncompleteDecisionsFailBeforeDatabase(t *testing.T) {
	selected, err := authz.NewSession("synthetic_owner", uuid.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []port.DelegationGrantDecision{{}, {Allowed: true}, {Allowed: true, UnitRef: uuid.New()}, {Allowed: true, Department: "trong_trot"}} {
		database := &unitPolicyDB{err: errors.New("unexpected database access")}
		svc := app.Service{DB: database, UnitDelegations: unitDelegationVerdict{handled: true, grant: decision}}
		ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), SelectedSession: &selected})
		_, err := svc.CreateDelegation(ctx, domain.DelegationInput{}, "incomplete-verdict")
		if !errors.Is(err, app.ErrDelegationForbidden) || database.calls != 0 {
			t.Fatalf("incomplete grant reached persistence: %v %+v", err, decision)
		}
	}
	for _, decision := range []port.PeopleScopeDecision{{}, {Mode: "unknown"}, {Mode: port.PeopleScopeInternalUnit}} {
		database := &unitPolicyDB{err: errors.New("unexpected database access")}
		svc := app.Service{DB: database, UnitDelegations: unitDelegationVerdict{handled: true, people: decision}}
		ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), SelectedSession: &selected})
		_, err := svc.PeopleInScope(ctx)
		if !errors.Is(err, app.ErrDependency) || database.calls != 0 {
			t.Fatalf("incomplete people scope reached persistence: %v %+v", err, decision)
		}
	}
}

func TestUnitDelegationNoOpOwnerCannotAuthorizeNativeOrPartialContext(t *testing.T) {
	for _, partial := range []bool{false, true} {
		database := &unitPolicyDB{err: errors.New("unexpected database access")}
		svc := app.Service{DB: database, Clock: clock.System{}, UnitDelegations: unitDelegationVerdict{handled: true,
			grant:  port.DelegationGrantDecision{Allowed: true, UnitRef: uuid.New(), Department: "trong_trot"},
			people: port.PeopleScopeDecision{Mode: port.PeopleScopeAll}}}
		p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New()}
		if partial {
			p.KN = &authz.KNContext{Role: port.KNTinh, Manager: true, UnitRef: uuid.New(), Department: "trong_trot"}
		}
		ctx := authz.WithPrincipal(context.Background(), p)
		_, grantErr := svc.CreateDelegation(ctx, domain.DelegationInput{}, "noop-owner")
		_, revokeErr := svc.RevokeDelegation(ctx, uuid.New(), "Thu hồi theo quyết định", "noop-owner", false)
		_, peopleErr := svc.PeopleInScope(ctx)
		for _, err := range []error{grantErr, revokeErr, peopleErr} {
			if !errors.Is(err, problem.Forbidden) {
				t.Fatalf("no-op owner authorized unbound session (partial=%v): %v", partial, err)
			}
		}
		if database.calls != 0 {
			t.Fatalf("no-op owner reached database (partial=%v)", partial)
		}
	}
}
