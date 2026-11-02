package assembly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBuildAccountRoleOrderThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mods.Accounts.RegisterAdmin(engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mods.Accounts.Middleware(), httpx.RequireMFA()))
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	for _, roles := range [][]string{{"platform_admin"}, {accport.RoleAccountAdmin}, {accport.RoleAccountAdminProvince}, {accport.RoleAccountAdminProvince, accport.RoleAccountAdmin}} {
		token := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: roles, MFA: true})
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/grants", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Grantable []string            `json:"grantable"`
			MFA       []string            `json:"mfa_roles"`
			Kinds     map[string][]string `json:"role_org_kinds"`
		}
		err = json.NewDecoder(res.Body).Decode(&body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("%v: %d %v", roles, res.StatusCode, err)
		}
		want := accport.LegacyRoleCatalog()
		if !slices.Equal(body.Grantable, want.Grantable(roles)) || !slices.Equal(body.MFA, want.MFARoles()) {
			t.Fatalf("HTTP role order changed: %v: %v", roles, body.Grantable)
		}
		for _, r := range []string{"kn_national_admin", "kn_province_admin", "kn_officer", "coop_manager", accport.RoleAccountAdminProvince} {
			if !slices.Equal(body.Kinds[r], want.OrgKinds(r)) {
				t.Fatalf("organization restriction changed: %s", r)
			}
		}
	}
}
