package kn77_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

// These empty-tenant reads prove each migrated entry checks its owner before
// transactions. Existing populated KN77/KHTC tests separately protect actual
// forms, snapshots, Excel, support/read-only and final-report workflows.
func TestKHTCReadersRequireOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	counted := &countedSessionDB{Beginner: pool}
	module := extension.New(counted, clock.System{}, extension.Deps{})
	fm, err := forms.New(pool, clock.System{}, forms.Deps{Profiles: []formsport.Profile{platformasm.KNKHTCProfile}})
	if err != nil {
		t.Fatal(err)
	}
	module.UseKHTCPeriods(extension.KHTCDeps{Forms: fm.Publisher(), Control: fm.Control(), Entry: fm.Entry(), MaxSubmissions: platformasm.KNKHTCProfile.MaxSubmissions})
	actor, tid, pid := ids.New(), ids.New(), ids.New().String()
	makeSession := func(role string) authz.Session {
		s, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: role, Department: partyport.DeptKeHoachTaiChinh, OrgRef: ids.New()})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	valid, commune := makeSession(extport.SessionRoleProvince), makeSession(extport.SessionRoleCommune)
	other, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero := authz.Session{}
	missingPayload, malformed := valid.WithPayload(nil), valid.WithPayload([]byte("{"))
	principal := func(s *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{"platform_admin", "kn_officer"}, MFA: true,
			KN: &authz.KNContext{Role: authz.KNAdmin, Department: partyport.DeptKeHoachTaiChinh}, SelectedSession: s}
	}
	narrowerLegacy := principal(&valid)
	narrowerLegacy.KN.Role = authz.KNXa
	noLegacy := principal(&valid)
	noLegacy.KN = nil
	cases := []struct {
		name   string
		p      *authz.Principal
		denied int
	}{
		{"no identity", nil, 401}, {"legacy admin only", principal(nil), 428}, {"zero session", principal(&zero), 403},
		{"different owner", principal(&other), 403}, {"missing owner payload", principal(&missingPayload), 403}, {"malformed owner payload", principal(&malformed), 403},
		{"owner commune overrides legacy admin", principal(&commune), 403}, {"owner province overrides legacy commune", narrowerLegacy, 0}, {"owner province without legacy", noLegacy, 0},
	}
	routes := []struct {
		name, path    string
		allowedStatus int
	}{
		{"catalog", "/v1/kn/khtc/catalogs/unit", 200},
		{"periods", "/v1/extension/khtc/periods?year=2026", 200},
		{"department inbox", "/v1/extension/khtc/department-forms?year=2026", 200},
		{"department form", "/v1/extension/khtc/periods/" + pid + "/forms/trong_trot", 404},
		{"report", "/v1/extension/khtc/periods/" + pid + "/report", 404},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					counted.calls = 0
					engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
					group := engine.Group("/v1", func(c *gin.Context) {
						ctx := tenant.WithID(c.Request.Context(), tid)
						if tc.p != nil {
							ctx = authz.WithPrincipal(ctx, *tc.p)
						}
						c.Request = c.Request.WithContext(ctx)
					})
					module.RegisterApp(group)
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
					want := tc.denied
					if want == 0 {
						want = route.allowedStatus
					}
					if response.Code != want {
						t.Fatalf("status=%d want %d: %s", response.Code, want, response.Body.String())
					}
					if tc.denied != 0 && counted.calls != 0 {
						t.Fatalf("denied before database check: %d transactions", counted.calls)
					}
					if tc.denied == 0 && counted.calls == 0 {
						t.Fatal("valid owner did not reach PostgreSQL")
					}
				})
			}
		})
	}
}
