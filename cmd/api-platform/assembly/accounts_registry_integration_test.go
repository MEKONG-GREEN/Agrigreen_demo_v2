package assembly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
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

// Exercise the real API assembly and public DTO: an application role must not
// gain account-admin access, and D24 MFA remains independent of the KN catalog.
func TestBuildAccountOwnerCatalogThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := extension.AttributeCatalog()
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mods.Accounts.RegisterAdmin(engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mods.Accounts.Middleware(), httpx.RequireMFA()))
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	tid, actor := ids.New(), ids.New()
	for _, tc := range []struct {
		name, role string
		mfa        bool
		status     int
	}{
		{"native admin", "platform_admin", true, http.StatusOK},
		{"missing MFA", "platform_admin", false, http.StatusForbidden},
		{"KN province role", "kn_province_admin", true, http.StatusForbidden},
		{"KN user role", "kn_user", true, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{tc.role}, MFA: tc.mfa})
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/mgmt-levels", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", res.StatusCode, tc.status)
			}
			if tc.status != http.StatusOK {
				return
			}
			var got map[string]any
			if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["version"] != owner.Version() || got["provisional"] != owner.Provisional() {
				t.Fatalf("owner metadata: %v", got)
			}
			levels := make([]any, 0, len(owner.Levels()))
			for _, l := range owner.Levels() {
				levels = append(levels, map[string]any{"code": l.Code, "label": l.Label, "manager": l.Manager, "reads_all_departments": l.ReadsAllDepartments})
			}
			if !reflect.DeepEqual(got["items"], levels) {
				t.Fatalf("owner labels/capability flags changed: %v", got["items"])
			}
			if len(got["province_groups"].([]any)) != len(owner.Groups()) || len(got["duties"].([]any)) != len(owner.Duties()) {
				t.Fatalf("owner catalog lost groups/duties: %v", got)
			}
		})
	}
}
