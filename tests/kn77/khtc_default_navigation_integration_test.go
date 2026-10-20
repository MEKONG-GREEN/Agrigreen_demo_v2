package kn77_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// KH-FN-CH-00.01/KH-UR-CH-01: use the actual root, signed JWTs, account
// selection, current-context middleware, live department profiles and menu.
func TestKH_FN_CH_00_01_DefaultRouteKHTC(t *testing.T) {
	clk := clock.NewFixed(t36)
	st := harness.NewAt(t, clk)
	s := &agScenario{t: t, s: st, clk: clk, tid: harness.NewTenant()}
	s.rootA = s.admin(authz.RolePlatformAdmin)
	s.want(s.rootA.adm(http.MethodPost, "/v1/masterdata/seed", map[string]any{}), 200, "seed administrative units")
	s.nat = str(obj(s.want(s.rootA.adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "NAV-NAT", "name": "TTKN Quốc gia"}), 201, "national org").body, "org"), "ref")
	s.p91 = str(obj(s.want(s.rootA.adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_province", "code": "NAV-AG", "name": "TTKN tỉnh", "parent_ref": s.nat, "admin_unit_code": agProv}), 201, "province org").body, "org"), "ref")
	const planning = "ke_toan_tinh_tuy_chinh"
	const technical = "ky_thuat_tinh_tuy_chinh"
	for _, org := range []string{s.nat, s.p91} {
		for _, dept := range []struct{ code, profile string }{{planning, "ke_hoach_tai_chinh"}, {technical, "trong_trot"}} {
			s.want(s.rootA.adm(http.MethodPost, "/v1/departments", map[string]any{"org_ref": org, "code": dept.code, "name": "Phòng " + dept.code, "menu_profile": dept.profile, "status": "active", "sort_order": 1}), 201, "custom department profile")
		}
	}
	user, assignments := s.createKN("Cán bộ điều hướng", "nav-canbo@vi-du.vn", "0912000701",
		asgKN(authz.KNTinh, s.p91, []string{planning, technical}, "chuyen_vien"),
		asgKN(authz.KNTW, s.nat, []string{planning}, "chuyen_vien"),
		asgKN(authz.KNXa, s.p91, []string{technical}, "chuyen_vien", agC1))
	// A stale old context must not affect selection of the next assignment/profile.
	var previous *agUser
	assertRoute := func(assignment, department, want string) *agUser {
		t.Helper()
		caller := user
		if previous != nil {
			caller = previous
		}
		selected := s.want(caller.post("/v1/me/context", map[string]any{"assignment_id": assignment, "department_code": department}), 201, "select role and profile")
		next := *user
		next.ctx = str(selected.body, "context_id")
		current := s.want(next.get("/v1/me/context"), 200, "current context")
		menu := s.want(next.get("/v1/menu"), 200, "current menu")
		if selected.body["default_route"] != want || current.body["default_route"] != want || menu.body["default_route"] != want || !navigationMenuHasRoute(menu.body, want) {
			t.Fatalf("route %s/%s: selected=%s current=%s menu=%s", assignment, department, selected.raw, current.raw, menu.raw)
		}
		previous = &next
		return &next
	}
	planningContext := assertRoute(assignments[authz.KNTinh], planning, "/kn/khtc/ky-bao-cao")
	assertRoute(assignments[authz.KNTinh], technical, "/kn/bao-cao-khtc")
	assertRoute(assignments[authz.KNTW], planning, "/kn/dashboard")
	assertRoute(assignments[authz.KNXa], technical, "/kn/nhiem-vu/cong-viec")
	// Leadership is still read-only after navigation, including a department outside the assignment.
	leader, leaderAssignments := s.createKN("Lãnh đạo chỉ xem", "nav-lanhdao@vi-du.vn", "0912000702", asgKN(authz.KNTinh, s.p91, []string{technical}, "pho_giam_doc"))
	s.want(s.rootA.adm(http.MethodPut, "/v1/accounts/users/"+leader.id.String()+"/assignments/"+leaderAssignments[authz.KNTinh]+"/attributes", map[string]any{"province_group": "ban_giam_doc"}), 200, "view-only group")
	readOnly := leader.as(leaderAssignments[authz.KNTinh], planning)
	view := s.want(readOnly.get("/v1/me/context"), 200, "read-only context")
	if view.body["read_only"] != true || view.body["default_route"] != "/kn/khtc/ky-bao-cao" {
		t.Fatalf("read-only routing:%s", view.raw)
	}
	s.wantCode(readOnly.post("/v1/extension/khtc/periods", map[string]any{}), 403, "accounts.context_read_only", "landing does not grant writes")
	support := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"target_assignment_id": assignments[authz.KNTinh], "department_code": planning, "reason": "Hỗ trợ chọn trang báo cáo"}), 201, "start support")
	supporter := *s.rootA
	supporter.ctx = str(support.body, "context_id")
	if support.body["default_route"] != "/kn/khtc/ky-bao-cao" {
		t.Fatalf("support route:%s", support.raw)
	}
	supportMenu := s.want(supporter.get("/v1/menu"), 200, "support menu")
	if supportMenu.body["default_route"] != support.body["default_route"] {
		t.Fatalf("support menu disagrees:%s", supportMenu.raw)
	}
	s.wantCode(supporter.post("/v1/extension/khtc/periods", map[string]any{}), 422, "accounts.support_reason_required", "support write still requires reason")
	// An administrator remains on the existing overview page.
	adminID := st.Directory.Seed(accport.NewUser{Username: "nav-admin", Email: "nav-admin@vi-du.vn", FirstName: "Admin", LastName: "Điều hướng", TenantID: s.tid}, true, authz.RolePlatformAdmin)
	adminAssignment := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/users/"+adminID.String()+"/assignments", map[string]any{"role": authz.KNAdmin, "department_codes": []string{}, "commune_codes": []string{}}), 201, "grant existing platform admin")
	adminUser := &agUser{s: s, id: adminID, tid: s.tid, token: st.Issuer.Sign(t, authztest.Token{Subject: adminID, TenantID: s.tid, Roles: s.realm(adminID), MFA: true, TTL: 3 * time.Hour})}
	adminContext := adminUser.as(str(obj(adminAssignment.body, "assignment"), "assignment_id"), "")
	adminCurrent := s.want(adminContext.get("/v1/me/context"), 200, "admin current")
	if adminCurrent.body["default_route"] != "/kn/tong-quan" {
		t.Fatalf("admin default changed:%s", adminCurrent.raw)
	}
	// Turning off the preferred module must choose a remaining enabled menu leaf,
	// consistently for existing sessions without issuing a new context.
	s.want(s.rootA.adm(http.MethodPost, "/v1/modules/pf/extension/enablement", map[string]any{"enabled": false, "reason": "Kiểm tra trang đích khi module ngưng"}), 201, "disable preferred module")
	current := s.want(planningContext.get("/v1/me/context"), 200, "context after module disable")
	menu := s.want(planningContext.get("/v1/menu"), 200, "menu after module disable")
	route := str(current.body, "default_route")
	if route == "/kn/khtc/ky-bao-cao" || route == "" || menu.body["default_route"] != route || !navigationMenuHasRoute(menu.body, route) {
		t.Fatalf("disabled target used:%s /%s", current.raw, menu.raw)
	}
	// Foreign actors and tenants still cannot use the selected context.
	foreign := *planningContext
	foreign.token = st.Issuer.Sign(t, authztest.Token{Subject: uuid.New(), TenantID: s.tid, Roles: []string{accport.RoleKNUser}})
	s.wantCode(foreign.get("/v1/me/context"), 403, "accounts.context_invalid", "foreign actor")
}

func navigationMenuHasRoute(menu map[string]any, route string) bool {
	var contains func([]map[string]any) bool
	contains = func(items []map[string]any) bool {
		for _, item := range items {
			if item["enabled"] != true {
				continue
			}
			children := arr(item, "children")
			if len(children) > 0 {
				if contains(children) {
					return true
				}
				continue
			}
			if item["route"] == route {
				return true
			}
		}
		return false
	}
	for _, group := range arr(menu, "groups") {
		if contains(arr(group, "items")) {
			return true
		}
	}
	return false
}
