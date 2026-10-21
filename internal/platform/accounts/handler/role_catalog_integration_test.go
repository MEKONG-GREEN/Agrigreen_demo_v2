package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const catalogFixtureRole = "fixture_auditor"

func newRoleCatalogFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	catalog, err := port.ExtendRoleCatalog(nil, port.RoleRegistration{
		Role: catalogFixtureRole, MFARole: true, LoginTOTP: true,
		OrgKinds: []string{string(partyport.OrgCooperative)}, Grantors: []string{port.RoleAccountAdmin},
	})
	if err != nil {
		t.Fatal(err)
	}
	realm := append(slices.Clone(port.ManagedRoles), authz.RolePlatformAdmin, port.RoleFarmer, catalogFixtureRole)
	f.dir = keycloak.NewMemory(realm...)
	mod := accounts.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{
		Directory: f.dir, Scope: f.party, Orgs: f.party, Roles: catalog,
	})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	// A server-owned application session can describe an application admin, but
	// cannot turn that into a realm account-admin permission or change MFA.
	project := func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		if !ok {
			c.Next()
			return
		}
		session, err := authz.NewSession("fixture", ids.New(), authz.SessionScope{NodeID: p.OrgRef})
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		session = session.WithPayload([]byte(`{"role":"admin"}`))
		p.SelectedSession = &session
		p.KN = &authz.KNContext{Role: authz.KNAdmin, OrgRef: p.OrgRef}
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), project,
		idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	f.admin, f.mod = srv, mod
	return f
}

func catalogStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected catalog list, got %T", value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("expected catalog string, got %T", item)
		}
		out = append(out, s)
	}
	return out
}

func TestRoleCatalogHTTPMatchesCreateAssignmentAndMFA(t *testing.T) {
	f := newRoleCatalogFixture(t)
	nat, prov := f.nationalAdmin(), f.provinceAdmin()
	ctx := context.Background()
	grants := nat.do(http.MethodGet, "/v1/accounts/grants", nil)
	if grants.status != http.StatusOK || !slices.Contains(catalogStrings(t, grants.body["grantable"]), catalogFixtureRole) ||
		!slices.Contains(catalogStrings(t, grants.body["mfa_roles"]), catalogFixtureRole) {
		t.Fatalf("registered catalog absent: %d %v", grants.status, grants.body)
	}
	kinds, ok := grants.body["role_org_kinds"].(map[string]any)
	if !ok || !slices.Equal(catalogStrings(t, kinds[catalogFixtureRole]), []string{string(partyport.OrgCooperative)}) {
		t.Fatalf("registered organization kinds absent: %v", grants.body)
	}
	provGrants := prov.do(http.MethodGet, "/v1/accounts/grants", nil)
	if provGrants.status != http.StatusOK || slices.Contains(catalogStrings(t, provGrants.body["grantable"]), catalogFixtureRole) {
		t.Fatalf("province received unregistered grant: %d %v", provGrants.status, provGrants.body)
	}
	if r := prov.do(http.MethodPost, "/v1/accounts/users", person(f.coop30292, catalogFixtureRole)); r.status != http.StatusForbidden || r.body["code"] != "accounts.role_not_grantable" {
		t.Fatalf("province created registered role: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodPost, "/v1/accounts/users", person(f.team30292, catalogFixtureRole)); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("registered organization restriction bypassed: %d %v", r.status, r.body)
	}
	id := nat.create(person(f.coop30292, catalogFixtureRole))
	u, err := f.dir.GetUser(ctx, id)
	if err != nil || !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) {
		t.Fatalf("registered role missing TOTP enrollment: %v %v", u.RequiredActions, err)
	}
	path := "/v1/accounts/users/" + id.String()
	r := nat.do(http.MethodGet, path, nil)
	if r.status != http.StatusOK || mfaOf(t, r)["required"] != true {
		t.Fatalf("registered role missing MFA status: %d %v", r.status, r.body)
	}
	if r := prov.do(http.MethodGet, path, nil); r.status != http.StatusForbidden || r.body["code"] != "accounts.role_not_grantable" {
		t.Fatalf("province operated registered target: %d %v", r.status, r.body)
	}

	// Adding a registered role follows the same catalog as initial creation.
	coop := nat.create(person(f.coop30292, "coop_manager"))
	coopPath := "/v1/accounts/users/" + coop.String()
	if mfaOf(t, nat.do(http.MethodGet, coopPath, nil))["required"] != false {
		t.Fatal("native cooperative role gained a TOTP requirement")
	}
	r = nat.do(http.MethodPost, coopPath+"/assignment", map[string]any{"roles": []string{"coop_manager", catalogFixtureRole}})
	if r.status != http.StatusOK {
		t.Fatalf("registered assignment failed: %d %v", r.status, r.body)
	}
	assigned, err := f.dir.RealmRoles(ctx, coop)
	u, userErr := f.dir.GetUser(ctx, coop)
	if err != nil || userErr != nil || !slices.Contains(assigned, catalogFixtureRole) || !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) {
		t.Fatalf("registered assignment/enrollment not applied: %v %v %v %v", assigned, u.RequiredActions, err, userErr)
	}
	if mfaOf(t, nat.do(http.MethodGet, coopPath, nil))["required"] != true {
		t.Fatal("registered assignment missing MFA requirement")
	}
	if r := nat.do(http.MethodPost, coopPath+"/assignment", map[string]any{"org_ref": f.team30292.String()}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("registered assignment escaped organization kind: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodPost, coopPath+"/assignment", map[string]any{"roles": []string{"coop_manager"}}); r.status != http.StatusOK {
		t.Fatalf("registered role removal failed: %d %v", r.status, r.body)
	}
	assigned, err = f.dir.RealmRoles(ctx, coop)
	if err != nil || slices.Contains(assigned, catalogFixtureRole) || mfaOf(t, nat.do(http.MethodGet, coopPath, nil))["required"] != false {
		t.Fatalf("registered managed role was not removed: %v %v", assigned, err)
	}
	// Native registry-admin protection remains intact under the extended catalog.
	registry := nat.create(person(f.national, "registry_admin"))
	if mfaOf(t, nat.do(http.MethodGet, "/v1/accounts/users/"+registry.String(), nil))["required"] != true {
		t.Fatal("native registry-admin lost its TOTP requirement")
	}
	if r := nat.do(http.MethodPost, "/v1/accounts/users", person(f.team30292, "coop_manager")); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("native cooperative organization restriction changed: %d %v", r.status, r.body)
	}
}

func TestRoleCatalogHTTPPreservesTenantAdminAndInstanceBoundaries(t *testing.T) {
	f := newRoleCatalogFixture(t)
	nat := f.nationalAdmin()
	id := nat.create(person(f.coop30292, catalogFixtureRole))
	path := "/v1/accounts/users/" + id.String()

	foreignTenant := ids.New()
	foreignNational := f.party.add(foreignTenant, partyport.OrgTTKNNational, "")
	foreignCoop := f.party.add(foreignTenant, partyport.OrgCooperative, "30292")
	foreign := f.as(foreignTenant, foreignNational, port.RoleAccountAdmin)
	foreignID := foreign.create(person(foreignCoop, catalogFixtureRole))
	if r := foreign.do(http.MethodGet, path, nil); r.status != http.StatusNotFound || r.body["code"] != "accounts.user_not_found" {
		t.Fatalf("registered role leaked tenant target: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodGet, "/v1/accounts/users/"+foreignID.String(), nil); r.status != http.StatusNotFound {
		t.Fatalf("registered role leaked foreign target: %d %v", r.status, r.body)
	}
	if r := nat.do(http.MethodPost, "/v1/accounts/users", person(foreignCoop, catalogFixtureRole)); r.status != http.StatusUnprocessableEntity || r.body["code"] != "accounts.org_invalid" {
		t.Fatalf("registered role accepted foreign-tenant organization: %d %v", r.status, r.body)
	}
	if r := f.provinceAdmin().do(http.MethodPost, "/v1/accounts/users", person(f.coop31120, "coop_manager")); r.status != http.StatusForbidden || r.body["code"] != "accounts.out_of_scope" {
		t.Fatalf("native province node boundary changed: %d %v", r.status, r.body)
	}
	for _, role := range []string{catalogFixtureRole, "kn_user", "kn_province_admin", "kn_national_admin"} {
		// The fixture binds an application-admin session for every request; it
		// must not add account-admin privileges to any of these realm roles.
		if r := f.as(f.tenant, f.national, role).do(http.MethodGet, "/v1/accounts/grants", nil); r.status != http.StatusForbidden {
			t.Fatalf("application session or realm role became account admin (%s): %d %v", role, r.status, r.body)
		}
	}
	noMFA := caller{f: f, token: f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant,
		OrgRef: f.national, Roles: []string{port.RoleAccountAdmin}})}
	if r := noMFA.do(http.MethodGet, path, nil); r.status != http.StatusForbidden || r.body["code"] != "mfa_required" {
		t.Fatalf("extended catalog bypassed Admin MFA: %d %v", r.status, r.body)
	}

	// A second module without configuration must retain its own legacy snapshot.
	plain := newFixture(t)
	plainAdmin := plain.nationalAdmin()
	g := plainAdmin.do(http.MethodGet, "/v1/accounts/grants", nil)
	if g.status != http.StatusOK || slices.Contains(catalogStrings(t, g.body["grantable"]), catalogFixtureRole) ||
		slices.Contains(catalogStrings(t, g.body["mfa_roles"]), catalogFixtureRole) {
		t.Fatalf("registered role leaked into another instance: %d %v", g.status, g.body)
	}
	if r := plainAdmin.do(http.MethodPost, "/v1/accounts/users", person(plain.coop30292, catalogFixtureRole)); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("unconfigured instance accepted registered role: %d %v", r.status, r.body)
	}
}
