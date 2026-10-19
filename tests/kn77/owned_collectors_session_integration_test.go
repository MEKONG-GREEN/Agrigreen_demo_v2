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

// The actual positive collection/lifecycle workflows are exercised by each
// module's existing signed-JWT/PG tests. Here missing/corrupt owner state must
// deny before transactions, despite broader legacy or realm permissions.
func TestCollectorsRequireOwnedSessionThroughHTTP(t *testing.T) {
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
	cases := []struct {
		name   string
		p      *authz.Principal
		status int
	}{
		{"no identity", nil, 401}, {"legacy admin only", principal(nil), 428}, {"zero owner", principal(&zero), 403},
		{"different owner", principal(&wrong), 403}, {"missing payload", principal(&empty), 403}, {"malformed payload", principal(&bad), 403},
	}
	for _, path := range []string{"/v1/kn/materials/" + ids.New().String() + "/log", "/v1/extension/cnts/outbreaks/" + ids.New().String()} {
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
				if response.Code != tc.status || counted.calls != 0 {
					t.Fatalf("status=%d want%d, transactions=%d: %s", response.Code, tc.status, counted.calls, response.Body.String())
				}
			})
		}
	}
}
