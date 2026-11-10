package kn77_test

// KN-STANDALONE-1-ADMIN-TOOLS (#264): BRD 2.0 không có FN cho 10 công cụ nền tảng (/quan-tri/{tham-so,to-chuc,mau-bao-cao,
// xung-dot,giam-sat-dong-bo,khach-hang-api,su-dung-api,bang-ke,nhat-ky-xuat}, /dong-bo). Trên gốc lắp ráp thật:
//   - Admin KN (vai "admin", cả khi token mang thêm vai realm quản trị nền tảng) gọi API của các màn đó trong ngữ cảnh
//     phiên KN (X-AG-Context) → 403 accounts.kn_surface_removed, ở cả Application API và Admin API;
//   - phần hạ tầng KN còn dùng không bị chặn: /v1/orgs/tree (Cơ cấu tổ chức 3 cấp FN-AD-04.02), đọc tham số
//     /v1/masterdata/params/value, đăng ký thiết bị POST /v1/devices (đồng bộ ngoại tuyến);
//   - phiên nền tảng (không ngữ cảnh KN) giữ nguyên quyền theo vai realm — không xóa màn/API khỏi nền tảng.

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

const knSurfaceRemoved = "accounts.kn_surface_removed"

func TestKNStandaloneAdminToolsClosedForKNSession(t *testing.T) {
	clk := clock.NewFixed(t36)
	st := harness.NewAt(t, clk)
	s := &agScenario{t: t, s: st, clk: clk, tid: harness.NewTenant(), mail: &agMailer{}, seen: map[uuid.UUID]bool{}, forms: map[string]string{}}
	s.rootA = s.admin(authz.RolePlatformAdmin)
	s.want(s.rootA.adm(http.MethodPost, "/v1/masterdata/seed", map[string]any{}), http.StatusOK, "seed masterdata")
	s.nat = str(obj(s.want(s.rootA.adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "TTKNQG-264", "name": "TTKN Quốc gia"}),
		http.StatusCreated, "Nút TW").body, "org"), "ref")

	// Vai "admin" chỉ gán cho tài khoản đã có platform_admin (accounts.kn_admin_grant) — Admin KN luôn mang vai realm
	// quản trị nền tảng; vai realm đó KHÔNG mở lại công cụ trong phiên KN.
	admID := st.Directory.Seed(accport.NewUser{Username: "admin.kn264", Email: "admin.kn264@ttkn.vi-du.vn", FirstName: "Admin", LastName: "KN",
		TenantID: s.tid}, true, authz.RolePlatformAdmin)
	g := s.want(s.rootA.adm(http.MethodPost, "/v1/accounts/users/"+admID.String()+"/assignments",
		map[string]any{"role": authz.KNAdmin, "department_codes": []string{}, "commune_codes": []string{}}), http.StatusCreated, "gán vai Admin KN")
	aid := str(obj(g.body, "assignment"), "assignment_id")
	roles := append(s.realm(admID), "param_setter", "param_approver", "registry_admin", "reports_admin",
		"sync_conflict_resolver", "device_admin", "geo_admin", "api_client_admin", "metering_viewer", "billing_officer")
	u := &agUser{s: s, id: admID, tid: s.tid, token: s.s.Issuer.Sign(t, authztest.Token{Subject: admID, TenantID: s.tid, Roles: roles, MFA: true, TTL: 3 * time.Hour})}
	r := s.want(u.post("/v1/me/context", map[string]any{"assignment_id": aid, "department_code": nil}), http.StatusCreated, "Admin KN chọn ngữ cảnh")
	kn := *u
	kn.ctx = str(r.body, "context_id")
	if kn.ctx == "" {
		t.Fatalf("không có context_id: %s", r.raw)
	}
	platform := *u // cùng tài khoản, không ngữ cảnh KN: phiên nền tảng

	id := ids.New().String()
	type call struct {
		admin        bool
		method, path string
	}
	removed := []call{
		// /quan-tri/tham-so
		{true, http.MethodGet, "/v1/masterdata/params"},
		{true, http.MethodGet, "/v1/masterdata/params/versions"},
		{true, http.MethodPost, "/v1/masterdata/params/versions"},
		{true, http.MethodPost, "/v1/masterdata/params/versions/" + id + "/approve"},
		{true, http.MethodPost, "/v1/masterdata/params/restore-default"},
		// /quan-tri/to-chuc
		{true, http.MethodGet, "/v1/orgs"},
		{true, http.MethodPost, "/v1/orgs"},
		{true, http.MethodGet, "/v1/orgs/export.xlsx"},
		{true, http.MethodPost, "/v1/orgs/import"},
		{true, http.MethodPost, "/v1/orgs/" + s.nat + "/rename"},
		{true, http.MethodGet, "/v1/orgs/" + s.nat + "/events"},
		{false, http.MethodGet, "/v1/orgs"},
		// /quan-tri/mau-bao-cao
		{true, http.MethodGet, "/v1/reports/templates"},
		{true, http.MethodGet, "/v1/reports/templates/kn.x/versions"},
		{true, http.MethodGet, "/v1/reports/schedules"},
		{true, http.MethodGet, "/v1/reports/schedules/" + id + "/runs"},
		{false, http.MethodGet, "/v1/reports/templates"},
		// /quan-tri/xung-dot, /quan-tri/giam-sat-dong-bo, /dong-bo
		{true, http.MethodGet, "/v1/sync/conflicts"},
		{true, http.MethodGet, "/v1/sync/conflicts/" + id},
		{true, http.MethodPost, "/v1/sync/conflicts/" + id + "/resolve"},
		{false, http.MethodGet, "/v1/geo/conflicts"},
		{false, http.MethodGet, "/v1/geo/conflicts/" + id},
		{true, http.MethodGet, "/v1/sync/integrations"},
		{true, http.MethodGet, "/v1/sync/integrations/log"},
		{true, http.MethodGet, "/v1/devices"},
		{true, http.MethodPost, "/v1/devices/" + id + "/revoke"},
		{false, http.MethodPost, "/v1/sync/nudge"},
		// /quan-tri/khach-hang-api, /quan-tri/su-dung-api, /quan-tri/bang-ke, /quan-tri/nhat-ky-xuat
		{true, http.MethodGet, "/v1/api-clients"},
		{true, http.MethodGet, "/v1/api-clients/" + id},
		{true, http.MethodGet, "/v1/metering/usage"},
		{true, http.MethodGet, "/v1/metering/usage-events"},
		{true, http.MethodGet, "/v1/metering/statements"},
		{true, http.MethodGet, "/v1/metering/egress"},
		{true, http.MethodGet, "/v1/metering/egress/anomalies"},
	}
	do := func(u *agUser, c call) agResp {
		var body any
		if c.method == http.MethodPost {
			body = map[string]any{}
		}
		if c.admin {
			return u.adm(c.method, c.path, body)
		}
		return u.app(c.method, c.path, body)
	}
	for _, c := range removed {
		s.wantCode(do(&kn, c), http.StatusForbidden, knSurfaceRemoved, "phiên KN "+c.method+" "+c.path)
		if got := do(&platform, c); got.code() == knSurfaceRemoved {
			t.Fatalf("phiên nền tảng %s %s phải giữ route (không bị chặn KN): %d %s", c.method, c.path, got.status, got.raw)
		}
	}

	// Hạ tầng KN còn dùng: không bị chặn trong phiên KN.
	s.want(kn.adm(http.MethodGet, "/v1/orgs/tree", nil), http.StatusOK, "Cơ cấu tổ chức 3 cấp (FN-AD-04.02)")
	for _, c := range []call{
		{false, http.MethodGet, "/v1/masterdata/params/value?key=cnts.diary.edit_window_days&at=2026-08-31"},
		{false, http.MethodPost, "/v1/devices"},
		{false, http.MethodGet, "/v1/me/context"},
	} {
		if got := do(&kn, c); got.code() == knSurfaceRemoved {
			t.Fatalf("chặn nhầm hạ tầng KN %s %s: %s", c.method, c.path, got.raw)
		}
	}
	// Phiên nền tảng vẫn đọc được danh sách tham số (màn /quan-tri/tham-so giữ cho nền tảng).
	s.want(platform.adm(http.MethodGet, "/v1/masterdata/params", nil), http.StatusOK, "phiên nền tảng đọc tham số")
}
