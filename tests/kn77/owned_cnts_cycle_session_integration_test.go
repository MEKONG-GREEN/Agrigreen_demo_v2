package kn77_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
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

// Owner permissions are enforced by real module routes before any transaction.
// Valid owner state remains authoritative when legacy state is absent or broader.
func TestCNTSAndReportCycleRequireOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	counted := &countedSessionDB{Beginner: pool}
	module := extension.New(counted, clock.System{}, extension.Deps{})
	valid, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleAdmin, Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero, empty, bad := authz.Session{}, valid.WithPayload(nil), valid.WithPayload([]byte("{"))
	actor, tid := ids.New(), ids.New()
	principal := func(selected *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{"platform_admin", "kn_officer"}, MFA: true,
			KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}, SelectedSession: selected}
	}
	noLegacy, narrowLegacy := principal(&valid), principal(&valid)
	noLegacy.KN = nil
	narrowLegacy.KN = &authz.KNContext{Role: authz.KNXa, Department: "trong_trot"}
	commune, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: extport.SessionRoleCommune, OrgRef: ids.New(), Department: "trong_trot", Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		p      *authz.Principal
		status int
		wantTX bool
	}{
		{"no identity", nil, 401, false}, {"legacy admin only", principal(nil), 428, false}, {"zero owner", principal(&zero), 403, false},
		{"different owner", principal(&wrong), 403, false}, {"missing payload", principal(&empty), 403, false}, {"malformed payload", principal(&bad), 403, false},
		{"owner commune cannot borrow legacy admin", principal(&commune), 403, false},
		{"owner admin overrides legacy commune", narrowLegacy, 404, true}, {"owner admin without legacy", noLegacy, 404, true},
	}
	for _, path := range []string{"/v1/extension/cnts/alerts/" + ids.New().String(), "/v1/extension/cnts/vaccination-batches/" + ids.New().String(), "/v1/extension/report-batches/" + ids.New().String()} {
		for _, tc := range cases {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
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
				engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != tc.status || (counted.calls > 0) != tc.wantTX {
					t.Fatalf("status=%d want%d, transactions=%d: %s", response.Code, tc.status, counted.calls, response.Body.String())
				}
			})
		}
	}
}
