package kn77_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
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

// These actual capability roots must apply KN owner policy before any transaction,
// including when raw realm roles would otherwise grant broader native access.
func TestCapabilitiesRequireOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	counted := &countedSessionDB{Beginner: env.Pool(t, migrations.PF, dbadmin.AppPlatform)}
	fm, err := forms.New(counted, clock.System{}, forms.Deps{Profiles: extension.FormProfiles()})
	if err != nil {
		t.Fatal(err)
	}
	rm := reports.New(counted, clock.System{}, reports.Deps{Policies: []reportsport.Policy{extension.ReportPolicy()}, ResolveSession: extension.ReportSessionResolver(nil)})
	actor, tid := ids.New(), ids.New()
	valid, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleAdmin, Department: partyport.DeptChanNuoiThuySan, Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero := authz.Session{}
	missing, malformed := valid.WithPayload(nil), valid.WithPayload([]byte("{"))
	principal := func(s *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, MFA: true, Roles: []string{authz.RolePlatformAdmin, reportsport.RoleReportsAdmin, "kn_officer"}, KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}, SelectedSession: s}
	}
	validLegacy := principal(&valid)
	validLegacy.KN.Role = authz.KNXa
	withoutLegacy := principal(&valid)
	withoutLegacy.KN = nil
	native := principal(nil)
	native.KN = nil
	cases := []struct {
		name   string
		p      *authz.Principal
		status int
		native bool
	}{
		{name: "no identity", status: 401},
		{name: "legacy admin only", p: principal(nil), status: 428},
		{name: "zero session", p: principal(&zero), status: 403},
		{name: "different owner", p: principal(&wrong), status: 403},
		{name: "missing owner payload", p: principal(&missing), status: 403},
		{name: "malformed owner payload", p: principal(&malformed), status: 403},
		{name: "owner overrides legacy commune", p: validLegacy, status: 200},
		{name: "owner without legacy", p: withoutLegacy, status: 200},
		{name: "native unselected administrator", p: native, status: 200, native: true},
	}
	routes := []struct {
		name, path   string
		nativeStatus int
	}{
		{"base forms", "/v1/forms/templates?profile=kn", 200},
		{"CNTS forms", "/v1/forms/templates?profile=kn_cnts", 200},
		{"KHTC forms", "/v1/forms/templates?profile=kn_khtc", 200},
		{"report forms", "/v1/reports/forms/available?department=chan_nuoi_thuy_san&level=TINH", 200},
		{"report dispatch", "/v1/reports/dispatch-recipients", 403},
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
					fm.RegisterApp(group)
					rm.RegisterApp(group)
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
					want := tc.status
					if tc.native {
						want = route.nativeStatus
					}
					if response.Code != want {
						t.Fatalf("status=%d want%d: %s", response.Code, want, response.Body.String())
					}
					if want != 200 && counted.calls != 0 {
						t.Fatalf("denied request opened%d transactions", counted.calls)
					}
					if want == 200 && counted.calls == 0 {
						t.Fatal("authorized request never reached PostgreSQL")
					}
				})
			}
		})
	}
}
