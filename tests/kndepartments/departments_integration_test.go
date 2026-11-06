// Package kndepartments_test kiểm HTTP thật của danh mục phòng ban sau KNSPLIT-2 #118 trên gốc lắp ráp api-platform đầy
// đủ (PostgreSQL, chuỗi xác thực → phiên hiện hành của pf/accounts → Idempotency-Key): binding /v1/departments* do chủ
// KN (internal/kn/extension/departments) đăng ký, dữ liệu và luật ở pf/party qua port.DepartmentAdmin. Đường dẫn, thân
// JSON, mã HTTP và mã lỗi giữ như trước khi chuyển; cách ly tenant; Cấp Tỉnh KN không quản trị phòng ban.
package kndepartments_test

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func want(t *testing.T, r harness.Resp, status int, what string) harness.Resp {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: muốn %d, nhận %d %s", what, status, r.Status, r.Raw)
	}
	return r
}

func wantCode(t *testing.T, r harness.Resp, status int, code, what string) {
	t.Helper()
	if r.Status != status || r.Code() != code || !r.IsProblem() {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.Status, r.Raw)
	}
}

func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

func TestKNSPLIT2DepartmentCatalogHTTPOnOwner(t *testing.T) {
	st := harness.New(t)
	tid := harness.NewTenant()
	root := st.User(harness.UserSpec{Tenant: tid, Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), http.StatusOK, "seed masterdata")
	nat := obj(want(t, root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "ttkn_national", "code": "DEP-NAT",
		"name": "TTKN Quốc gia"}), http.StatusCreated, "Nút TW").Body, "org")["ref"].(string)
	prov := obj(want(t, root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "ttkn_province", "code": "DEP-AG",
		"name": "TTKN An Giang", "parent_ref": nat, "admin_unit_code": "91"}), http.StatusCreated, "Nút tỉnh").Body, "org")["ref"].(string)

	// Tạo: 201 + Location; gọi lặp cùng Idempotency-Key → cùng phòng ban (phát lại của chuỗi idempotency).
	body := map[string]any{"org_ref": prov, "code": "trong_trot", "name": "Phòng Trồng trọt", "description": "Phòng ban thử", "sort_order": 1,
		"status": port.DeptActive, "menu_profile": port.MenuProfileTrongTrot}
	idem := harness.NewIdem()
	r1 := want(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments", idem, body), http.StatusCreated, "tạo phòng ban")
	id := r1.Str("id")
	if r1.Header.Get("Location") != "/v1/departments/"+id || r1.Str("status_label") != "Đang áp dụng" || r1.Body["selectable"] != true ||
		r1.Body["account_count"] != nil || r1.Str("org_ref") != prov {
		t.Fatalf("phòng ban vừa tạo: %v %s", r1.Header, r1.Raw)
	}
	if r2 := root.Do(harness.Admin, http.MethodPost, "/v1/departments", idem, body); r2.Str("id") != id || r2.Header.Get("Idempotent-Replayed") == "" {
		t.Fatalf("gọi lặp: %d %v %s", r2.Status, r2.Header, r2.Raw)
	}
	wantCode(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments", "", body), http.StatusConflict, "party.department_code_taken", "trùng mã")
	bad := map[string]any{"org_ref": prov, "code": "Trong Trot", "name": "Sai mã"}
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments", "", bad), http.StatusUnprocessableEntity, "mã sai dạng")

	// Deny-by-default: Cấp Tỉnh KN (vai realm) không quản trị phòng ban; Application API không có thao tác ghi.
	tinh := st.User(harness.UserSpec{Tenant: tid, Org: uuid.MustParse(prov), Roles: []string{port.RoleKNProvinceAdmin}, MFA: true})
	if r := tinh.Do(harness.Admin, http.MethodPost, "/v1/departments", "", map[string]any{"org_ref": prov, "code": "pb_tinh", "name": "Phòng tỉnh"}); r.Status != http.StatusForbidden {
		t.Fatalf("Cấp Tỉnh tạo phòng ban: %d %s", r.Status, r.Raw)
	}
	want(t, root.Do(harness.App, http.MethodPost, "/v1/departments", "", body), http.StatusMethodNotAllowed, "Application API không ghi phòng ban")

	// Đọc Admin kèm số tài khoản/bản ghi (nguồn đếm thật của gốc lắp ráp), đọc Application không kèm.
	g := want(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments/"+id, "", nil), http.StatusOK, "đọc phòng ban")
	if _, ok := g.Body["account_count"].(float64); !ok {
		t.Fatalf("Admin phải có account_count: %s", g.Raw)
	}
	al := want(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil), http.StatusOK, "danh sách Admin")
	ap := want(t, root.Do(harness.App, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil), http.StatusOK, "danh sách Application")
	if items := ap.Items(); len(items) != 1 || items[0]["account_count"] != nil || ap.Str("org_ref") != prov || len(al.Items()) != 1 {
		t.Fatalf("danh sách: admin %s / app %s", al.Raw, ap.Raw)
	}
	want(t, root.Do(harness.App, http.MethodGet, "/v1/departments?org_ref=khong-phai-uuid", "", nil), http.StatusUnprocessableEntity, "org_ref sai")

	// Sửa, ngưng (lý do bắt buộc, cảnh báo), mở lại, xóa; Log đủ chuỗi sự kiện.
	want(t, root.Do(harness.Admin, http.MethodPut, "/v1/departments/"+id, "", map[string]any{"name": "Phòng Trồng trọt AG", "sort_order": 2}),
		http.StatusOK, "sửa")
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments/"+id+"/status", "", map[string]any{"status": "suspended"}),
		http.StatusUnprocessableEntity, "ngưng thiếu lý do")
	s := want(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments/"+id+"/status", "", map[string]any{"status": "suspended",
		"reason": "Tạm ngưng chờ sắp xếp tổ chức"}), http.StatusCreated, "ngưng")
	if _, ok := s.Body["warnings"].([]any); !ok || obj(s.Body, "department")["status"] != port.DeptSuspended {
		t.Fatalf("ngưng: %s", s.Raw)
	}
	wantCode(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments/"+id+"/status", "", map[string]any{"status": "suspended",
		"reason": "Ngưng lần hai cho chắc"}), http.StatusConflict, "party.department_state", "đã ở trạng thái")
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/departments/"+id+"/retire", "", map[string]any{"reason": "Khai báo nhầm phòng ban"}),
		http.StatusCreated, "xóa (không tài khoản/dữ liệu)")
	if r := root.Do(harness.Admin, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil); len(r.Items()) != 0 {
		t.Fatalf("phòng ban đã xóa còn trong danh sách: %s", r.Raw)
	}
	if r := root.Do(harness.Admin, http.MethodGet, "/v1/departments?include_retired=true&org_ref="+prov, "", nil); len(r.Items()) != 1 || r.Items()[0]["retired"] != true {
		t.Fatalf("include_retired: %s", r.Raw)
	}
	lg := want(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments/"+id+"/log", "", nil), http.StatusOK, "log")
	var events []string
	for _, e := range lg.Items() {
		events = append(events, e["event"].(string))
		if e["actor_ref"] != root.Actor.String() {
			t.Fatalf("dòng log thiếu người: %v", e)
		}
	}
	if !slices.Equal(events, []string{"created", "updated", "status_changed", "retired"}) || lg.Body["next_cursor"] != nil {
		t.Fatalf("log: %v %s", events, lg.Raw)
	}
	want(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments/"+id+"/log?cursor=-1", "", nil), http.StatusUnprocessableEntity, "cursor âm")
	wantCode(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments/khong-phai-uuid", "", nil), http.StatusNotFound, "party.department_not_found", "id sai")

	// Cách ly tenant: quản trị tenant khác không đọc, không sửa được.
	other := st.User(harness.UserSpec{Tenant: harness.NewTenant(), Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	wantCode(t, other.Do(harness.Admin, http.MethodGet, "/v1/departments/"+id, "", nil), http.StatusNotFound, "party.department_not_found", "tenant khác đọc")
	want(t, other.Do(harness.Admin, http.MethodPost, "/v1/departments/"+id+"/status", "", map[string]any{"status": "active"}), http.StatusNotFound, "tenant khác sửa")
	if r := other.Do(harness.Admin, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil); len(r.Items()) != 0 {
		t.Fatalf("tenant khác thấy phòng ban: %s", r.Raw)
	}

	// Không đăng ký trùng: cây module của phòng ban (chủ KN từ #222) không còn phòng ban đã xóa.
	want(t, root.Do(harness.App, http.MethodGet, "/v1/departments/trong_trot/modules?audience=admin&org_ref="+prov, "", nil), http.StatusNotFound,
		"phòng ban đã xóa không có cây module")

	// Chủ KN: route đi qua cổng bật module pf/extension — tắt pf/extension cho tenant thì danh mục phòng ban đóng 403.
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/modules/pf/extension/enablement", "", map[string]any{"enabled": false,
		"reason": "Kiểm cổng module của chủ KN"}), http.StatusCreated, "tắt pf/extension")
	wantCode(t, root.Do(harness.Admin, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil), http.StatusForbidden, "module_not_enabled", "module tắt")
	wantCode(t, root.Do(harness.App, http.MethodGet, "/v1/departments?org_ref="+prov, "", nil), http.StatusForbidden, "module_not_enabled", "module tắt (app)")
}

// KNSPLIT-2 #222: GET /v1/menu, GET /v1/departments/{code}/modules và POST /v1/orgs/{ref}/departments/seed do chủ KN đăng
// ký (internal/kn/extension/departments) trên gốc lắp ráp đầy đủ; bảng menu, luật đối tượng/đơn vị và dữ liệu nhúng ở
// pf/party qua port.DepartmentAdmin. Đường dẫn, tham số (kể cả ?role= cũ), thân JSON, Cache-Control và mã lỗi giữ nguyên.
func TestKNSPLIT2MenuAndSeedHTTPOnOwner(t *testing.T) {
	st := harness.New(t)
	tid := harness.NewTenant()
	root := st.User(harness.UserSpec{Tenant: tid, Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), http.StatusOK, "seed masterdata")
	nat := obj(want(t, root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "ttkn_national", "code": "MENU-NAT",
		"name": "TTKN Quốc gia"}), http.StatusCreated, "Nút TW").Body, "org")["ref"].(string)
	prov := obj(want(t, root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "ttkn_province", "code": "MENU-AG",
		"name": "TTKN An Giang", "parent_ref": nat, "admin_unit_code": "91"}), http.StatusCreated, "Nút tỉnh").Body, "org")["ref"].(string)
	seed := "/v1/orgs/" + prov + "/departments/seed"

	// Nạp 5 phòng ban mặc định: lần đầu tạo đủ, gọi lặp khóa mới không tạo thêm; cùng khóa → phát lại phản hồi gốc.
	idem := harness.NewIdem()
	s1 := want(t, root.Do(harness.Admin, http.MethodPost, seed, idem, nil), http.StatusOK, "nạp phòng ban")
	created, _ := s1.Body["created"].([]any)
	if len(created) != 5 || s1.Body["existing"] != float64(0) || created[0].(map[string]any)["org_ref"] != prov {
		t.Fatalf("nạp phòng ban: %s", s1.Raw)
	}
	if r := root.Do(harness.Admin, http.MethodPost, seed, idem, nil); string(r.Raw) != string(s1.Raw) || r.Header.Get("Idempotent-Replayed") == "" {
		t.Fatalf("gọi lặp cùng khóa: %d %v %s", r.Status, r.Header, r.Raw)
	}
	if r := want(t, root.Do(harness.Admin, http.MethodPost, seed, "", nil), http.StatusOK, "nạp lặp"); r.Body["existing"] != float64(5) ||
		len(r.Body["created"].([]any)) != 0 {
		t.Fatalf("nạp lặp: %s", r.Raw)
	}
	wantCode(t, root.Do(harness.Admin, http.MethodPost, "/v1/orgs/khong-phai-uuid/departments/seed", "", nil), http.StatusNotFound,
		"party.org_not_found", "ref sai")
	tinh := st.User(harness.UserSpec{Tenant: tid, Org: uuid.MustParse(prov), Roles: []string{port.RoleKNProvinceAdmin}, MFA: true})
	want(t, tinh.Do(harness.Admin, http.MethodPost, seed, "", nil), http.StatusForbidden, "Cấp Tỉnh không nạp phòng ban")
	want(t, root.Do(harness.App, http.MethodPost, seed, "", nil), http.StatusNotFound, "Application API không có nạp phòng ban")

	// Menu Admin của đơn vị: nhóm Phòng ban, không lưu đệm.
	m := want(t, root.Do(harness.App, http.MethodGet, "/v1/menu?org_ref="+prov, "", nil), http.StatusOK, "menu Admin")
	if m.Header.Get("Cache-Control") != "no-store" || m.Str("audience") != port.AudienceAdmin || m.Str("org_ref") != prov ||
		!strings.Contains(string(m.Raw), `"key":"phong_ban"`) || m.Str("menu_version") == "" {
		t.Fatalf("menu Admin: %v %s", m.Header, m.Raw)
	}
	// Cấp Tỉnh: menu của vai mình, phòng ban Đang áp dụng duy nhất; xin menu vai khác (kể cả qua ?role= cũ) → 403.
	mt := want(t, tinh.Do(harness.App, http.MethodGet, "/v1/menu", "", nil), http.StatusOK, "menu Cấp Tỉnh")
	if mt.Str("audience") != port.AudienceTinh || obj(mt.Body, "department")["code"] != "trong_trot" {
		t.Fatalf("menu Cấp Tỉnh: %s", mt.Raw)
	}
	wantCode(t, tinh.Do(harness.App, http.MethodGet, "/v1/menu?audience=admin", "", nil), http.StatusForbidden, "party.menu_audience_forbidden", "xin menu Admin")
	wantCode(t, tinh.Do(harness.App, http.MethodGet, "/v1/menu?role=admin", "", nil), http.StatusForbidden, "party.menu_audience_forbidden", "?role= cũ")
	wantCode(t, tinh.Do(harness.App, http.MethodGet, "/v1/menu?department=chan_nuoi_thuy_san", "", nil), http.StatusConflict,
		"party.department_not_open", "phòng ban chưa mở")
	want(t, tinh.Do(harness.App, http.MethodGet, "/v1/menu?org_ref=khong-phai-uuid", "", nil), http.StatusUnprocessableEntity, "org_ref sai")

	// Cây module của một phòng ban.
	md := want(t, tinh.Do(harness.App, http.MethodGet, "/v1/departments/trong_trot/modules", "", nil), http.StatusOK, "modules Cấp Tỉnh")
	if md.Header.Get("Cache-Control") != "no-store" || md.Str("audience") != port.AudienceTinh {
		t.Fatalf("modules Cấp Tỉnh: %v %s", md.Header, md.Raw)
	}
	if r := want(t, root.Do(harness.App, http.MethodGet, "/v1/departments/trong_trot/modules?role=admin&org_ref="+prov, "", nil), http.StatusOK,
		"modules Admin (?role= cũ)"); r.Str("audience") != port.AudienceAdmin {
		t.Fatalf("modules Admin: %s", r.Raw)
	}
	wantCode(t, root.Do(harness.App, http.MethodGet, "/v1/departments/khong_co/modules?audience=admin&org_ref="+prov, "", nil), http.StatusNotFound,
		"party.department_not_found", "phòng ban không có")

	// Cách ly tenant: tenant khác không thấy phòng ban của đơn vị này.
	other := st.User(harness.UserSpec{Tenant: harness.NewTenant(), Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	if r := other.Do(harness.App, http.MethodGet, "/v1/departments/trong_trot/modules?audience=admin&org_ref="+prov, "", nil); r.Status < 400 {
		t.Fatalf("tenant khác đọc cây module: %d %s", r.Status, r.Raw)
	}

	// Chủ KN: route đi qua cổng bật module pf/extension (như /v1/departments*, #118).
	want(t, root.Do(harness.Admin, http.MethodPost, "/v1/modules/pf/extension/enablement", "", map[string]any{"enabled": false,
		"reason": "Kiểm cổng module của chủ KN"}), http.StatusCreated, "tắt pf/extension")
	wantCode(t, root.Do(harness.App, http.MethodGet, "/v1/menu?org_ref="+prov, "", nil), http.StatusForbidden, "module_not_enabled", "module tắt (menu)")
	wantCode(t, root.Do(harness.Admin, http.MethodPost, seed, "", nil), http.StatusForbidden, "module_not_enabled", "module tắt (nạp)")
}
