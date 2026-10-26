package contextdata_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func boundCases(t *testing.T) []contextCase {
	t.Helper()
	r, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	cases := nativeCases()
	for i, c := range cases {
		if c.principal == nil || c.principal.KN == nil {
			continue
		}
		original := *c.principal
		s, err := r.ResolveSelected(c.context(), original)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if s.Profile() != "kn" || s.ID() != original.KN.ID {
			t.Fatal("owner/session metadata differs")
		}
		p := original
		p.SelectedSession = &s
		cases[i].principal = &p
		if p.ActorRef != original.ActorRef || p.TenantID != original.TenantID || p.OrgRef != original.OrgRef || !reflect.DeepEqual(p.Roles, original.Roles) || p.MFA != original.MFA {
			t.Fatal("binding changed authentication")
		}
	}
	return cases
}

func TestOwnedSessionProjectionMatchesFrozenNative(t *testing.T) {
	expected := golden(t)
	for i, c := range boundCases(t) {
		got := db.ContextVarsFrom(c.context()).Settings()
		if !reflect.DeepEqual(got, expected[i].Settings) {
			t.Fatalf("%s: got %v, want %v", c.name, got, expected[i].Settings)
		}
		requires := false
		if c.principal != nil {
			requires = db.RequiresNode(*c.principal)
		}
		if requires != expected[i].RequiresNode {
			t.Fatalf("%s: node policy differs", c.name)
		}
	}
}

func TestOwnedSessionProjectionOnPostgres(t *testing.T) { runProjectionOnPostgres(t, boundCases(t)) }

func TestInvalidSelectedSessionCannotFallBack(t *testing.T) {
	p := oneConnection(t, false)
	for _, c := range boundCases(t) {
		if c.principal == nil || c.principal.KN == nil || c.principal.KN.Role != authz.KNAdmin {
			continue
		}
		principal := *c.principal
		principal.SelectedSession = &authz.Session{}
		ctx := tenant.WithID(authz.WithPrincipal(context.Background(), principal), ref(999))
		called := false
		err := db.WithContextTx(ctx, p, db.Module("pf/content"), func(pgx.Tx) error { called = true; return nil })
		if called || !errors.Is(err, db.ErrNodeScopeRequired) {
			t.Fatalf("%s: invalid binding fell back: %v", c.name, err)
		}
	}
}

func TestOwnerSessionProjectionRejectsInvalidResolvedState(t *testing.T) {
	r, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []*authz.KNContext{{ID: ref(4), Role: "unknown"}, {Role: authz.KNXa, OrgRef: ref(7)}} {
		if _, err := r.ResolveSelected(context.Background(), authz.Principal{Roles: []string{"platform_admin"}, KN: k}); !errors.Is(err, authz.ErrInvalidSession) {
			t.Fatalf("invalid owner state accepted: %v", err)
		}
	}
	if _, err := r.ResolveSelected(context.Background(), authz.Principal{Roles: []string{"kn_officer", "platform_admin"}}); !errors.Is(err, authz.ErrSessionNotResolved) {
		t.Fatal("missing selected owner fell back to realm")
	}
}
