package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
)

type registeredMenuSession struct{ unit uuid.UUID }

func (s registeredMenuSession) Current(context.Context) (string, string, uuid.UUID, bool) {
	return port.AudienceTinh, "trong_trot", s.unit, true
}
func TestRegisteredMenusHTTPAndContextShareCurrentTenantCatalog(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	for _, unit := range []uuid.UUID{tr.p89, tr.p92} {
		body := deptBody(unit, "trong_trot")
		body["status"] = port.DeptActive
		wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments", body), http.StatusCreated, "seed active department")
	}
	calls := 0
	failOwner := false
	invalidOwner := false
	modes := []port.MenuMode{}
	registry, err := port.NewMenus(func(ctx context.Context, in port.MenuBuildRequest) (port.MenuDefinition, error) {
		calls++
		modes = append(modes, in.Mode)
		p, ok := authz.FromContext(ctx)
		if !ok || p.TenantID != f.tenant || p.ActorRef == uuid.Nil {
			t.Fatal("builder identity changed")
		}
		if failOwner {
			return port.MenuDefinition{}, problem.Forbidden
		}
		if in.Mode == port.MenuDepartment {
			if in.Department.ID == uuid.Nil || in.Department.OrgRef != in.UnitRef || in.Department.MenuProfile != "generic" {
				t.Fatal("builder did not receive current tenant metadata")
			}
		} else if len(in.Departments) == 0 || in.Departments[0].OrgRef != tr.p89 {
			t.Fatal("admin metadata missing")
		}
		// A disabled first presentation must not mask an enabled shortcut to the
		// same registered route when navigation selects its landing page.
		def := port.MenuDefinition{Version: "registered/v1", DefaultRoute: "/registered/reports", Groups: []port.MenuGroup{
			{Key: "work", Label: "Work", Items: []port.MenuItem{
				{Key: "report", Label: "Reports", Route: "/registered/reports", Module: "pf/reports", Enabled: false},
				{Key: "unavailable", Label: "Unavailable", Route: "/registered/unavailable", Module: "pf/party", Enabled: false},
				{Key: "overview", Label: "Overview", Route: "/registered/overview", Module: "pf/party", Enabled: true},
			}},
			{Key: "shared", Label: "Shared", Items: []port.MenuItem{{Key: "shortcuts", Label: "Shortcuts", Enabled: true, Children: []port.MenuItem{
				{Key: "report.shortcut", Label: "Quick reports", Route: "/registered/reports", Module: "pf/reports", Enabled: true},
			}}}},
		}}
		if invalidOwner {
			def.DefaultRoute = "/not-registered"
		}
		return def, nil
	}, port.MenuRoute{Pattern: "/registered/reports", Module: "pf/reports"}, port.MenuRoute{Pattern: "/registered/unavailable", Module: "pf/party"}, port.MenuRoute{Pattern: "/registered/overview", Module: "pf/party"})
	if err != nil {
		t.Fatal(err)
	}
	policy := func(port.NavigationRequest) ([]string, error) {
		return []string{"/registered/reports", "/registered/unavailable"}, nil
	}
	mod := party.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, party.Deps{AdminUnits: f.units, Vault: f.vault, Menus: registry, NavigationPolicy: policy})
	mod.UseSessionContext(registeredMenuSession{unit: tr.p89})
	log := telemetry.NewLogger("menu-registry-test", io.Discard)
	app := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(app.Group("/v1", httpx.Authenticate(f.iss.Verifier())))
	server := httptest.NewServer(app)
	defer server.Close()
	user := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	get := func(c caller, path string) resp {
		return c.call(server.URL, http.MethodGet, path, "", "application/json", nil)
	}
	r := get(user, "/v1/menu")
	wantStatus(t, r, http.StatusOK, "registered menu")
	if str(r.body, "default_route") != "/registered/reports" || str(r.body, "menu_version") != "registered/v1" {
		t.Fatal("legacy catalog used", r.body)
	}
	if keys := menuKeys(r); !slices.Contains(keys, "report") || !slices.Contains(keys, "report.shortcut") {
		t.Fatal("registered route alias was removed", r.body)
	}
	principal := authz.Principal{ActorRef: user.actor, TenantID: f.tenant, OrgRef: tr.p89, Roles: []string{port.RoleKNProvinceAdmin}, MFA: true}
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tenant), principal)
	request := port.NavigationRequest{Audience: port.AudienceTinh, UnitRef: tr.p89, Department: port.Department{OrgRef: tr.p89, Code: "trong_trot", MenuProfile: "stale", Status: port.DeptNotOpen}}
	landing, err := mod.ContextNavigation()(ctx, request)
	if err != nil || landing != str(r.body, "default_route") {
		t.Fatal("context and HTTP differ", landing, err)
	}
	// The same instance's real enablement ledger removes a registered leaf before landing.
	admin := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(admin.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	adminServer := httptest.NewServer(admin)
	defer adminServer.Close()
	rr := root.call(adminServer.URL, http.MethodPost, "/v1/modules/pf/reports/enablement", key(), "application/json", []byte(`{"enabled":false,"reason":"Tạm ngưng module báo cáo của tenant"}`))
	wantStatus(t, rr, http.StatusCreated, "disable registered module")
	r = get(user, "/v1/menu")
	wantStatus(t, r, http.StatusOK, "filtered menu")
	if str(r.body, "default_route") != "/registered/overview" || slices.Contains(menuKeys(r), "report") || slices.Contains(menuKeys(r), "report.shortcut") {
		t.Fatal("disabled route became landing", r.body)
	}
	landing, err = mod.ContextNavigation()(ctx, request)
	if err != nil || landing != "/registered/overview" {
		t.Fatal("context bypassed disabled module", landing, err)
	}
	// Selecting a new validated assignment can still carry the old context header.
	// The trusted producer request owns the new unit; the public query stays bound.
	newSelection := request
	newSelection.UnitRef, newSelection.Department.OrgRef = tr.p92, tr.p92
	landing, err = mod.ContextNavigation()(ctx, newSelection)
	if err != nil || landing != "/registered/overview" {
		t.Fatal("old selected node blocked valid new selection", landing, err)
	}
	before := calls
	wantStatus(t, get(user, "/v1/menu?org_ref="+tr.p92.String()), http.StatusForbidden, "selected node cannot escape")
	foreign := f.as(uuid.New(), tr.p89, port.RoleKNProvinceAdmin)
	wantStatus(t, get(foreign, "/v1/menu"), http.StatusNotFound, "foreign tenant cannot reuse department")
	foreignPrincipal := principal
	foreignPrincipal.TenantID = uuid.New()
	if _, err := mod.ContextNavigation()(authz.WithPrincipal(tenant.WithID(context.Background(), foreignPrincipal.TenantID), foreignPrincipal), request); err == nil {
		t.Fatal("foreign context reused metadata")
	}
	if calls != before {
		t.Fatal("denied node/tenant reached builder")
	}
	wantStatus(t, get(root, "/v1/menu?audience=admin&org_ref="+tr.p89.String()), http.StatusOK, "registered admin")
	wantStatus(t, get(root, "/v1/departments/trong_trot/modules?audience=admin&org_ref="+tr.p89.String()), http.StatusOK, "registered department modules")
	if !slices.Contains(modes, port.MenuAdmin) || !slices.Contains(modes, port.MenuDepartmentModules) {
		t.Fatal("consumer still used legacy tree")
	}
	failOwner = true
	wantStatus(t, get(user, "/v1/menu"), http.StatusForbidden, "owner denial final")
	if _, err := mod.ContextNavigation()(ctx, request); err == nil {
		t.Fatal("owner denial fell back in context")
	}
	failOwner = false
	invalidOwner = true
	wantStatus(t, get(user, "/v1/menu"), http.StatusServiceUnavailable, "invalid registration final")
	// The original unconfigured server retains its native menu in the same process.
	native := user.appDo(http.MethodGet, "/v1/menu?audience=kn_tinh&org_ref="+tr.p89.String()+"&department=trong_trot", nil)
	wantStatus(t, native, http.StatusOK, "nil compatibility")
	if str(native.body, "menu_version") == "registered/v1" {
		t.Fatal("registration leaked to another instance")
	}
}
