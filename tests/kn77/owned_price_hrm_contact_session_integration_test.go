package kn77_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	contactport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
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

// Exercise public module roots with production KN policy adapters. Negative
// requests must be denied before beginning any transaction; owner-only requests
// must actually reach PostgreSQL. Populated source suites separately protect the
// full price, HRM/vault/documents and contact workflows.
func TestPriceHRMContactRequireOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	counted := &countedSessionDB{Beginner: env.Pool(t, migrations.PF, dbadmin.AppPlatform)}
	pm, err := price.New(counted, clock.System{}, platformasm.PriceProfiles())
	if err != nil {
		t.Fatal(err)
	}
	hm, err := hrm.New(counted, clock.System{}, platformasm.KNHRMAccess)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := contact.New(counted, clock.System{}, platformasm.KNContactAccess, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor, tid := ids.New(), ids.New()
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	makeSession := func(role, dept string, viewOnly bool, duties []string) authz.Session {
		p := authz.Principal{ActorRef: actor, TenantID: tid,
			KN: &authz.KNContext{ID: ids.New(), Role: role, Department: dept, OrgRef: ids.New(), ViewOnly: viewOnly, Duties: duties}}
		s, err := registry.ResolveSelected(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	admin := makeSession(extport.SessionRoleAdmin, "", false, nil)
	province := makeSession(extport.SessionRoleProvince, "hanh_chinh_tong_hop", false, []string{extport.SessionDutyPriceAdmin})
	other, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	zero := authz.Session{}
	missing, malformed := admin.WithPayload(nil), admin.WithPayload([]byte("{"))
	principal := func(s *authz.Session) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{"platform_admin", "kn_officer"}, MFA: true,
			KN: &authz.KNContext{Role: authz.KNAdmin, Department: "hanh_chinh_tong_hop"}, SelectedSession: s}
	}
	noLegacy := principal(&admin)
	noLegacy.KN = nil
	narrowerLegacy := principal(&province)
	narrowerLegacy.KN.Role = authz.KNXa
	cases := []struct {
		name   string
		p      *authz.Principal
		status int
	}{
		{"no identity", nil, 401}, {"legacy admin only", principal(nil), 428}, {"zero session", principal(&zero), 403},
		{"different owner", principal(&other), 403}, {"missing owner payload", principal(&missing), 403}, {"malformed owner payload", principal(&malformed), 403},
		{"owner admin without legacy", noLegacy, 200}, {"owner province overrides legacy commune", narrowerLegacy, 200},
	}
	routes := []struct{ name, path string }{
		{"price", "/v1/price/catalog"}, {"hrm", "/v1/hrm/status"}, {"contact", "/v1/contacts/units"},
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
					pm.RegisterApp(group)
					hm.RegisterApp(group)
					cm.RegisterApp(group)
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
					if response.Code != tc.status {
						t.Fatalf("status=%d want %d: %s", response.Code, tc.status, response.Body.String())
					}
					if tc.status != 200 && counted.calls != 0 {
						t.Fatalf("denied after %d transactions", counted.calls)
					}
					if tc.status == 200 && counted.calls == 0 {
						t.Fatal("valid owner did not reach PostgreSQL")
					}
				})
			}
		})
	}
	// A broader legacy admin and raw platform_admin must never override the owner
	// department or read-only policy. Read access remains available as before.
	wrongDept := makeSession(extport.SessionRoleProvince, "trong_trot", false, nil)
	viewOnly := makeSession(extport.SessionRoleProvince, "hanh_chinh_tong_hop", true, []string{extport.SessionDutyPriceAdmin})
	for _, tc := range []struct {
		name, path string
		s          *authz.Session
		status     int
		flag       string
		value      any
	}{
		{"owner department wins", "/v1/hrm/status", &wrongDept, 403, "", nil},
		{"owner contact field-set", "/v1/contacts/access", &viewOnly, 200, "can_manage", false},
		{"owner readonly price duty", "/v1/price/status", &viewOnly, 200, "can_steward", false},
		{"owner HRM readonly", "/v1/hrm/status", &viewOnly, 200, "can_manage", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counted.calls = 0
			engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
			group := engine.Group("/v1", func(c *gin.Context) {
				c.Request = c.Request.WithContext(authz.WithPrincipal(tenant.WithID(c.Request.Context(), tid), *principal(tc.s)))
			})
			pm.RegisterApp(group)
			hm.RegisterApp(group)
			cm.RegisterApp(group)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.flag != "" {
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body[tc.flag] != tc.value {
					t.Fatalf("%s=%v want %v", tc.flag, body[tc.flag], tc.value)
				}
			}
			if tc.status == 403 && counted.calls != 0 {
				t.Fatal("wrong owner department reached database")
			}
		})
	}
}

// Explicitly native configurations still authorize their own realm roles. No KN
// profile, legacy context or owner session is manufactured for these requests.
func TestPriceHRMContactNativeRealmConfigurationThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	counted := &countedSessionDB{Beginner: env.Pool(t, migrations.PF, dbadmin.AppPlatform)}
	pm, err := price.New(counted, clock.System{}, []priceport.Profile{{Name: "native-price", Roles: map[string]string{"native_reader": "TINH"}, ManageBy: []string{"native_admin"}}})
	if err != nil {
		t.Fatal(err)
	}
	hm, err := hrm.New(counted, clock.System{}, hrmport.Access{ManageBy: []string{"native_admin"}})
	if err != nil {
		t.Fatal(err)
	}
	cm, err := contact.New(counted, clock.System{}, contactport.Access{RealmFull: []string{"native_admin"}, RealmBasic: []string{"native_reader"}, RealmManage: []string{"native_admin"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	actor, tid := ids.New(), ids.New()
	for _, route := range []struct {
		name, path, role string
		status           int
	}{
		{"price reader", "/v1/price/catalog", "native_reader", 200},
		{"HRM administrator", "/v1/hrm/status", "native_admin", 200},
		{"contact reader", "/v1/contacts/units", "native_reader", 200},
		{"price wrong realm", "/v1/price/catalog", "unrelated", 403},
		{"HRM wrong realm", "/v1/hrm/status", "native_reader", 403},
		{"contact wrong realm", "/v1/contacts/units", "unrelated", 403},
	} {
		t.Run(route.name, func(t *testing.T) {
			counted.calls = 0
			engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
			group := engine.Group("/v1", func(c *gin.Context) {
				c.Request = c.Request.WithContext(authz.WithPrincipal(tenant.WithID(c.Request.Context(), tid), authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{route.role}}))
			})
			pm.RegisterApp(group)
			hm.RegisterApp(group)
			cm.RegisterApp(group)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
			if response.Code != route.status {
				t.Fatalf("status=%d want %d: %s", response.Code, route.status, response.Body.String())
			}
			if route.status == 200 && counted.calls == 0 {
				t.Fatal("native request did not reach PostgreSQL")
			}
			if route.status == 403 && counted.calls != 0 {
				t.Fatal("wrong native role reached PostgreSQL")
			}
		})
	}
}
