package handler_test

// KN v7.7 gói P2 (docs/brd/kn-v7.7-plan.md §3 P2): phòng ban (FN-AD-04.03), khu vực + cây 3 cấp +
// bảng ảnh hưởng (FN-AD-04.02), menu theo cấu hình (UR-KT-02).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// Giả lập cổng của module khác
// ---------------------------------------------------------------------------

type fakeUsage struct {
	mu sync.Mutex
	u  port.Usage
}

func (f *fakeUsage) set(accounts, records int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.u = port.Usage{Accounts: accounts, Records: records}
}

func (f *fakeUsage) DepartmentUsage(context.Context, uuid.UUID, string) (port.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.u, nil
}

type fakeImpact struct {
	kind string
	n    int
}

func (f fakeImpact) ImpactKind() string { return f.kind }
func (f fakeImpact) OrgImpact(context.Context, uuid.UUID, []string) (int, error) {
	return f.n, nil
}

type fakeForms map[string]bool

func (f fakeForms) HasForms(_ context.Context, dept, level, reportType string) (bool, error) {
	return f[dept+"|"+level+"|"+reportType], nil
}

type fakeNames map[uuid.UUID]string

func (f fakeNames) DisplayNames(context.Context, []uuid.UUID) (map[uuid.UUID]string, error) {
	return f, nil
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func deptBody(org uuid.UUID, code string) map[string]any {
	return map[string]any{"org_ref": org.String(), "code": code, "name": "Phòng " + code, "description": "Phòng ban thử", "sort_order": 10}
}

func (c caller) adminWithKey(method, path, idem string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.call(c.f.admin.URL, method, path, idem, "application/json", b)
}

func groupKeys(r resp) []string {
	var out []string
	gs, _ := r.body["groups"].([]any)
	for _, g := range gs {
		out = append(out, g.(map[string]any)["key"].(string))
	}
	return out
}

// menuKeys trả mọi khóa mục (đệ quy) của menu.
func menuKeys(r resp) []string {
	var out []string
	var walk func(items []any)
	walk = func(items []any) {
		for _, it := range items {
			m := it.(map[string]any)
			out = append(out, m["key"].(string))
			kids, _ := m["children"].([]any)
			walk(kids)
		}
	}
	gs, _ := r.body["groups"].([]any)
	for _, g := range gs {
		its, _ := g.(map[string]any)["items"].([]any)
		walk(its)
	}
	return out
}

// adminDeptItem trả mục phòng ban code trong nhóm "Phòng ban" của menu Admin.
func adminDeptItem(t *testing.T, r resp, code string) map[string]any {
	t.Helper()
	for _, g := range r.body["groups"].([]any) {
		gm := g.(map[string]any)
		if gm["key"] != "phong_ban" {
			continue
		}
		for _, it := range gm["items"].([]any) {
			if m := it.(map[string]any); m["key"] == "phong_ban."+code {
				return m
			}
		}
	}
	t.Fatalf("menu Admin không có phòng ban %s: %v", code, r.body)
	return nil
}

func logItems(t *testing.T, r resp) []map[string]any {
	t.Helper()
	wantStatus(t, r, http.StatusOK, "log")
	return items(r)
}

func changeOf(entry map[string]any, field string) (old, nw any, ok bool) {
	cs, _ := entry["changes"].([]any)
	for _, c := range cs {
		m := c.(map[string]any)
		if m["field"] == field {
			return m["old"], m["new"], true
		}
	}
	return nil, nil, false
}

func (f *fixture) deptID(admin caller, unit uuid.UUID, code string) string {
	f.t.Helper()
	r := admin.adminDo(http.MethodGet, "/v1/departments?org_ref="+unit.String(), nil)
	for _, it := range items(r) {
		if it["code"] == code {
			return it["id"].(string)
		}
	}
	f.t.Fatalf("không có phòng ban %s: %v", code, r.body)
	return ""
}

// ---------------------------------------------------------------------------
// FN-AD-04.03 — Danh mục phòng ban
// ---------------------------------------------------------------------------

// FN-AD-04.03 + AC-CHUNG: phòng ban theo đơn vị (Nút cấp 1/2); trùng mã trong đơn vị → chặn; chỉ Admin;
// gọi lặp cùng Idempotency-Key; sửa/đổi trạng thái ghi Log người, thời điểm, cũ → mới, lý do; ngưng có
// lý do và cảnh báo; "xóa" chỉ khi không có tài khoản/dữ liệu (mặc định chặn khi chưa nối nguồn đếm);
// sổ chỉ ghi thêm, chuỗi băm liền; cách ly tenant.
func TestFN_AD_04_03_DepartmentLifecycleAuditLog(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	f.mod.UseActorNames(fakeNames{root.actor: "Quản trị Mekong"})

	wantCode(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.t30001, "trong_trot")), http.StatusUnprocessableEntity,
		"party.department_unit_invalid", "phòng ban của Nút xã")
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p89, "Trong Trot")), http.StatusUnprocessableEntity, "mã sai dạng")
	bad := deptBody(tr.p89, "trong_trot")
	bad["menu_profile"] = "khong_co"
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments", bad), http.StatusUnprocessableEntity, "bộ menu không có")
	wantStatus(t, f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin).adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p89, "trong_trot")),
		http.StatusForbidden, "Cấp Tỉnh không quản trị phòng ban")

	// Gọi lặp cùng Idempotency-Key → cùng phòng ban, một dòng sổ.
	body := deptBody(tr.p89, "trong_trot")
	body["status"], body["menu_profile"] = port.DeptActive, port.MenuProfileTrongTrot
	idem := key()
	r1 := root.adminWithKey(http.MethodPost, "/v1/departments", idem, body)
	r2 := root.adminWithKey(http.MethodPost, "/v1/departments", idem, body)
	wantStatus(t, r1, http.StatusCreated, "tạo phòng ban")
	if str(r2.body, "id") != str(r1.body, "id") || r2.hdr.Get("Idempotent-Replayed") == "" {
		t.Fatalf("gọi lặp: %v / %v %v", r1.body, r2.body, r2.hdr)
	}
	id := str(r1.body, "id")
	if n := f.countIn(f.tenant, "pf_party.department_event"); n != 1 {
		t.Fatalf("sổ phòng ban sau gọi lặp: %d", n)
	}
	if r1.body["selectable"] != true || str(r1.body, "status_label") != "Đang áp dụng" {
		t.Fatalf("phòng ban: %v", r1.body)
	}
	wantCode(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p89, "trong_trot")), http.StatusConflict,
		"party.department_code_taken", "trùng mã trong đơn vị")
	// Cùng mã ở đơn vị khác được phép (theo từng đơn vị).
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p92, "trong_trot")), http.StatusCreated, "đơn vị khác")

	// Sửa (PUT) → một dòng Log cũ → mới; PUT không đổi gì → không ghi sổ.
	upd := map[string]any{"name": "Phòng Trồng trọt", "description": "Báo cáo cây trồng", "sort_order": 5}
	r := root.adminDo(http.MethodPut, "/v1/departments/"+id, upd)
	wantStatus(t, r, http.StatusOK, "sửa")
	wantStatus(t, root.adminDo(http.MethodPut, "/v1/departments/"+id, upd), http.StatusOK, "sửa lặp")

	// Ngưng: lý do bắt buộc; cảnh báo khi chưa nối nguồn đếm.
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments/"+id+"/status", map[string]any{"status": "suspended"}),
		http.StatusUnprocessableEntity, "ngưng thiếu lý do")
	r = root.adminDo(http.MethodPost, "/v1/departments/"+id+"/status", map[string]any{"status": "suspended", "reason": "Tạm ngưng chờ sắp xếp tổ chức"})
	wantStatus(t, r, http.StatusCreated, "ngưng")
	if ws, _ := r.body["warnings"].([]any); len(ws) == 0 || str(r.body, "department", "status") != port.DeptSuspended {
		t.Fatalf("ngưng: %v", r.body)
	}
	wantCode(t, root.adminDo(http.MethodPost, "/v1/departments/"+id+"/status", map[string]any{"status": "suspended", "reason": "Ngưng lần hai cho chắc"}),
		http.StatusConflict, "party.department_state", "đã ở trạng thái")
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments/"+id+"/status", map[string]any{"status": "active"}), http.StatusCreated, "mở lại")

	// "Xóa": chưa nối nguồn đếm → mặc định chặn; có tài khoản → chặn; không còn gì → được, ẩn khỏi danh sách.
	wantCode(t, root.adminDo(http.MethodPost, "/v1/departments/"+id+"/retire", map[string]any{"reason": "Khai báo nhầm phòng ban"}),
		http.StatusServiceUnavailable, "party.dependency_unavailable", "xóa khi chưa nối nguồn đếm")
	usage := &fakeUsage{}
	f.mod.UseDepartmentUsage(usage)
	usage.set(2, 0)
	r = root.adminDo(http.MethodPost, "/v1/departments/"+id+"/retire", map[string]any{"reason": "Khai báo nhầm phòng ban"})
	wantCode(t, r, http.StatusConflict, "party.department_in_use", "xóa khi có tài khoản")
	r = root.adminDo(http.MethodGet, "/v1/departments/"+id, nil)
	if r.body["account_count"] != float64(2) {
		t.Fatalf("số tài khoản: %v", r.body)
	}
	usage.set(0, 0)
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments/"+id+"/retire", map[string]any{"reason": "Khai báo nhầm phòng ban"}), http.StatusCreated, "xóa")
	if list := items(root.adminDo(http.MethodGet, "/v1/departments?org_ref="+tr.p89.String(), nil)); len(list) != 0 {
		t.Fatalf("phòng ban đã xóa còn trong danh sách: %v", list)
	}
	if list := items(root.adminDo(http.MethodGet, "/v1/departments?include_retired=true&org_ref="+tr.p89.String(), nil)); len(list) != 1 || list[0]["retired"] != true {
		t.Fatalf("include_retired: %v", list)
	}
	wantCode(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p89, "trong_trot")), http.StatusConflict,
		"party.department_code_taken", "mã đã xóa không tái sử dụng")

	// Log: tạo, sửa, ngưng (lý do), mở lại, xóa — người, tên hiển thị, cũ → mới.
	es := logItems(t, root.adminDo(http.MethodGet, "/v1/departments/"+id+"/log", nil))
	var events []string
	for _, e := range es {
		events = append(events, e["event"].(string))
		if e["actor_ref"] != root.actor.String() || e["actor_display"] != "Quản trị Mekong" || e["at"] == "" {
			t.Fatalf("dòng log thiếu người/thời điểm: %v", e)
		}
	}
	if !slices.Equal(events, []string{"created", "updated", "status_changed", "status_changed", "retired"}) {
		t.Fatalf("log: %v", events)
	}
	if o, n, ok := changeOf(es[1], "name"); !ok || o != "Phòng trong_trot" || n != "Phòng Trồng trọt" {
		t.Fatalf("log sửa tên cũ → mới: %v", es[1])
	}
	if o, n, ok := changeOf(es[2], "status"); !ok || o != "active" || n != "suspended" || es[2]["reason"] != "Tạm ngưng chờ sắp xếp tổ chức" {
		t.Fatalf("log ngưng: %v", es[2])
	}
	if _, n, ok := changeOf(es[4], "retired"); !ok || n != "true" {
		t.Fatalf("log xóa: %v", es[4])
	}

	// RB-1: sổ chỉ ghi thêm; chuỗi băm liền.
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE pf_party.department_event SET reason = 'sửa lén' WHERE department_id = $1", uuid.MustParse(id))
		return e
	})
	if err == nil {
		t.Fatal("UPDATE sổ phòng ban phải bị chặn (RB-1)")
	}
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		v, e := ledger.Verify(context.Background(), tx, "pf_party.department_event", f.tenant, "dept:"+id)
		if e == nil && (!v.OK || v.RowsChecked != 5) {
			t.Fatalf("chuỗi băm: %+v", v)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}

	// Cách ly tenant: quản trị tenant khác không thấy, không sửa được.
	other := f.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin)
	wantCode(t, other.adminDo(http.MethodGet, "/v1/departments/"+id, nil), http.StatusNotFound, "party.department_not_found", "tenant khác đọc")
	wantStatus(t, other.adminDo(http.MethodPost, "/v1/departments/"+id+"/status", map[string]any{"status": "active"}), http.StatusNotFound, "tenant khác sửa")
	if list := items(other.adminDo(http.MethodGet, "/v1/departments?org_ref="+tr.p92.String(), nil)); len(list) != 0 {
		t.Fatalf("tenant khác thấy phòng ban: %v", list)
	}
}

// ---------------------------------------------------------------------------
// UR-KT-02 — Menu theo cấu hình
// ---------------------------------------------------------------------------

// UR-KT-02, FN-CH-00.01 AC "phòng ban chuyển Đang áp dụng hiện ngay mà không cần cập nhật ứng dụng",
// FN-AD-04.03, BRD §6: backend trả menu theo vai + phòng ban từ bảng tĩnh có phiên bản và trạng thái
// phòng ban hiện hành — bật/ngưng phòng ban đổi menu ở request kế tiếp (không triển khai lại); Admin thấy
// phòng ban chưa mở hiện mờ; "Báo cáo đột xuất" chỉ khi phòng ban có mẫu loại này (FN-AD-04.06); người
// gọi chỉ lấy được menu của vai mình (Admin: mọi cấp, FN-AD-04.07).
func TestUR_KT_02_MenuByConfigDepartmentToggle(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)

	// Dữ liệu nền 5 phòng ban An Giang (W11.06, chờ khách Q-051), chạy lặp an toàn.
	r := root.adminDo(http.MethodPost, "/v1/orgs/"+tr.p89.String()+"/departments/seed", nil)
	if created, _ := r.body["created"].([]any); r.status != http.StatusOK || len(created) != 5 {
		t.Fatalf("seed phòng ban: %d %v", r.status, r.body)
	}
	r = root.adminDo(http.MethodPost, "/v1/orgs/"+tr.p89.String()+"/departments/seed", nil)
	if created, _ := r.body["created"].([]any); len(created) != 0 || r.body["existing"] != float64(5) || len(r.body["legacy_retired"].([]any)) != 0 {
		t.Fatalf("seed lặp: %v", r.body)
	}

	xa := f.as(f.tenant, tr.t30001, port.RoleKNOfficer)
	tinh := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	tw := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)

	// Bước 2 đăng nhập: danh sách phòng ban của đơn vị người gọi (Nút xã → TTKN tỉnh).
	r = xa.appDo(http.MethodGet, "/v1/departments", nil)
	if list := items(r); r.status != http.StatusOK || len(list) != 5 || str(r.body, "org_ref") != tr.p89.String() ||
		list[0]["code"] != "trong_trot" || list[0]["selectable"] != true || list[1]["selectable"] != false || list[1]["account_count"] != nil {
		t.Fatalf("phòng ban (app): %d %v", r.status, r.body)
	}

	// Cấp Xã: menu Trồng trọt mặc định (chỉ một phòng ban Đang áp dụng), trang mặc định Công việc cần làm.
	r = xa.appDo(http.MethodGet, "/v1/menu", nil)
	wantStatus(t, r, http.StatusOK, "menu Cấp Xã")
	if str(r.body, "audience") != port.AudienceXa || str(r.body, "department", "code") != "trong_trot" ||
		str(r.body, "default_route") != "/kn/nhiem-vu/cong-viec" || str(r.body, "menu_version") != "kn.menu/v6" ||
		!slices.Equal(groupKeys(r), []string{"dieu_hanh", "bao_cao", "du_lieu", "dung_chung"}) || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("menu Cấp Xã: %v %v", r.body, r.hdr)
	}
	keys := menuKeys(r)
	for _, k := range []string{"xa.nhiem_vu.cong_viec", "xa.bao_cao.cua_xa", "xa.thu_thap.thua", "xa.thu_thap.vat_tu"} {
		if !slices.Contains(keys, k) {
			t.Fatalf("menu Cấp Xã thiếu %s: %v", k, keys)
		}
	}
	if slices.Contains(keys, "xa.bao_cao.dot_xuat") || slices.Contains(keys, "tw.dashboard") {
		t.Fatalf("menu Cấp Xã thừa mục (đột xuất khi chưa có mẫu / Dashboard — Cấp Xã không có Dashboard): %v", keys)
	}
	raw, _ := json.Marshal(r.body)
	if !strings.Contains(string(raw), `"badge_source":"extension.report_tasks.todo"`) {
		t.Fatalf("thiếu khóa số đếm Công việc cần làm: %s", raw)
	}
	// Không lấy được menu của vai khác.
	wantCode(t, xa.appDo(http.MethodGet, "/v1/menu?audience=kn_tinh", nil), http.StatusForbidden, "party.menu_audience_forbidden", "Cấp Xã xin menu Tỉnh")
	wantCode(t, xa.appDo(http.MethodGet, "/v1/menu?audience=admin", nil), http.StatusForbidden, "party.menu_audience_forbidden", "Cấp Xã xin menu Admin")
	// Tên tham số cũ ?role= (hợp đồng tạm P6) vẫn nhận một đợt.
	wantCode(t, xa.appDo(http.MethodGet, "/v1/menu?role=kn_tinh", nil), http.StatusForbidden, "party.menu_audience_forbidden", "Cấp Xã xin menu Tỉnh (?role=)")
	if r := xa.appDo(http.MethodGet, "/v1/departments/trong_trot/modules?role=kn_xa", nil); r.status != http.StatusOK || str(r.body, "audience") != port.AudienceXa {
		t.Fatalf("?role= (tên cũ): %d %v", r.status, r.body)
	}
	// Hợp đồng đường dẫn KN-fix: trang web-ops, không chứa mã phòng ban (phòng ban ở ngữ cảnh phiên).
	if !strings.Contains(string(raw), `"route":"/kn/nhiem-vu/lich-cong-tac"`) || strings.Contains(string(raw), "/kn/trong_trot/") {
		t.Fatalf("đường dẫn menu Cấp Xã: %s", raw)
	}
	// Phòng ban chưa mở → không vào được.
	wantCode(t, xa.appDo(http.MethodGet, "/v1/menu?department=chan_nuoi_thuy_san", nil), http.StatusConflict, "party.department_not_open", "chưa mở")

	// Cấp Tỉnh, Cấp Quốc gia (chỉ xem: không có Nhiệm vụ).
	r = tinh.appDo(http.MethodGet, "/v1/menu", nil)
	if keys := menuKeys(r); r.status != http.StatusOK || !slices.Contains(keys, "tinh.nhiem_vu.lich_cong_tac") || !slices.Contains(keys, "tinh.bao_cao.chi_cuc") ||
		slices.Contains(keys, "tinh.bao_cao.dot_xuat") || str(r.body, "default_route") != "/kn/dashboard" {
		t.Fatalf("menu Cấp Tỉnh: %v", r.body)
	}
	r = tw.appDo(http.MethodGet, "/v1/menu?org_ref="+tr.p89.String(), nil)
	if keys := menuKeys(r); r.status != http.StatusOK || !slices.Equal(groupKeys(r), []string{"dieu_hanh", "bao_cao", "dung_chung"}) || !slices.Contains(keys, "tw.bao_cao.nam") {
		t.Fatalf("menu Cấp Quốc gia: %v", r.body)
	}

	// Admin: Tổng quan hệ thống; Phòng ban (Trồng trọt mở, 4 phòng khác "Chưa mở" mờ); Quản trị hệ thống; Dùng chung.
	r = root.appDo(http.MethodGet, "/v1/menu?org_ref="+tr.p89.String(), nil)
	if r.status != http.StatusOK || !slices.Equal(groupKeys(r), []string{"dieu_hanh", "phong_ban", "quan_tri", "dung_chung"}) || str(r.body, "default_route") != "/kn/tong-quan" {
		t.Fatalf("menu Admin: %v", r.body)
	}
	if it := adminDeptItem(t, r, "trong_trot"); it["enabled"] != true ||
		!hasMenuChildren(it, []string{"admin.pb.theo_doi_nop", "admin.pb.tong_hop", "admin.pb.du_lieu_bao_cao", "admin.pb.thu_thap", "admin.pb.lich_cong_tac", "admin.pb.danh_muc", "admin.pb.mau_bao_cao", "admin.pb.bao_cao_khtc"}) ||
		!hasMenuChild(it, "admin.pb.mau_bao_cao", "/kn/phong-ban/trong_trot/mau-bao-cao") ||
		!hasMenuChild(it, "admin.pb.bao_cao_khtc", "/kn/phong-ban/trong_trot/bao-cao-khtc") { // W11.11
		t.Fatalf("Admin › Phòng Trồng trọt: %v", it)
	}
	if it := adminDeptItem(t, r, "chan_nuoi_thuy_san"); it["enabled"] != false || it["status_label"] != "Chưa mở" {
		t.Fatalf("Admin › Chăn nuôi chưa mở: %v", it)
	}

	// Bật Phòng Chăn nuôi - Thủy sản: menu đổi NGAY ở request kế tiếp, không triển khai lại.
	cn := f.deptID(root, tr.p89, "chan_nuoi_thuy_san")
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments/"+cn+"/status", map[string]any{"status": "active"}), http.StatusCreated, "bật phòng ban")
	r = xa.appDo(http.MethodGet, "/v1/menu?department=chan_nuoi_thuy_san", nil)
	if r.status != http.StatusOK || str(r.body, "department", "menu_profile") != port.MenuProfileChanNuoiThuySan ||
		// Bộ dựng thử không nối NavigationPolicy → trang mặc định thô của đối tượng; khi chạy thật (assembly) chính sách
		// contextDefaultRoutes đưa CNTS Cấp Xã về /kn/cnts/bao-cao-ky (builder_test, kn-menu.json).
		str(r.body, "default_route") != "/kn/nhiem-vu/cong-viec" ||
		!slices.Equal(groupKeys(r), []string{"dieu_hanh", "bao_cao", "du_lieu", "dung_chung"}) {
		t.Fatalf("menu phòng ban vừa bật (CNTS Cấp Xã theo UP-03, kn.menu/v6): %d %v", r.status, r.body)
	}
	// Mục của module chưa có: "Sắp có", không bấm được.
	if raw, _ := json.Marshal(r.body); !strings.Contains(string(raw), `"status":"coming_soon","status_label":"Sắp có"`) {
		t.Fatalf("mục Sắp có: %s", raw)
	}
	// W11.42 + BF-MENU (#98): "Việc cần làm" (màn Báo cáo kỳ CNTS) của Cấp Xã đã mở.
	if keys := menuKeys(r); !slices.Contains(keys, "xa.cnts.viec_can_lam") {
		t.Fatalf("Cấp Xã CNTS có Việc cần làm: %v", keys)
	}
	wantStatus(t, xa.appDo(http.MethodGet, "/v1/menu", nil), http.StatusUnprocessableEntity, "hai phòng ban đang áp dụng → phải chọn")
	if it := adminDeptItem(t, root.appDo(http.MethodGet, "/v1/menu?org_ref="+tr.p89.String(), nil), "chan_nuoi_thuy_san"); it["enabled"] != true {
		t.Fatalf("Admin thấy phòng ban vừa bật: %v", it)
	}

	// Báo cáo đột xuất: chỉ khi phòng ban có mẫu loại dot_xuat ở cấp tương ứng (P3).
	f.mod.UseFormAvailability(fakeForms{"trong_trot|commune|dot_xuat": true})
	if keys := menuKeys(xa.appDo(http.MethodGet, "/v1/menu?department=trong_trot", nil)); !slices.Contains(keys, "xa.bao_cao.dot_xuat") {
		t.Fatalf("Cấp Xã có mẫu đột xuất: %v", keys)
	}
	if keys := menuKeys(tinh.appDo(http.MethodGet, "/v1/menu?department=trong_trot", nil)); slices.Contains(keys, "tinh.bao_cao.dot_xuat") {
		t.Fatalf("Cấp Tỉnh chưa có mẫu đột xuất cấp tỉnh: %v", keys)
	}

	// /v1/departments/{code}/modules: cây module của một phòng ban.
	r = tinh.appDo(http.MethodGet, "/v1/departments/trong_trot/modules", nil)
	if keys := menuKeys(r); r.status != http.StatusOK || !slices.Contains(keys, "tinh.nhiem_vu.ra_soat") {
		t.Fatalf("modules Cấp Tỉnh: %v", r.body)
	}
	r = root.appDo(http.MethodGet, "/v1/departments/thong_tin_truyen_thong/modules?audience=admin&org_ref="+tr.p89.String(), nil)
	// W11.72: 5 mục chung + "Báo cáo cho KHTC" (W11.11) + "Xã báo giá", "Kỳ báo giá & giờ khóa" (đã mở) + "Mặt hàng" (Sắp có).
	if gs, _ := r.body["groups"].([]any); r.status != http.StatusOK || len(gs) != 1 ||
		!slices.Contains(menuKeys(r), "admin.pb.bao_cao_khtc") ||
		!slices.Contains(menuKeys(r), "admin.pb.bao_gia_xa") || !slices.Contains(menuKeys(r), "admin.pb.bao_gia_lich") ||
		!slices.Contains(menuKeys(r), "admin.pb.bao_gia_mat_hang") {
		t.Fatalf("modules Admin (phòng ban chưa mở, khuôn Thông tin Tuyên truyền: 5 mục chung + Báo cáo cho KHTC + Xã báo giá, Kỳ báo giá & giờ khóa + 1 Sắp có): %v", r.body)
	}
	// Admin vào menu mọi cấp (FN-AD-04.07).
	if r := root.appDo(http.MethodGet, "/v1/menu?audience=kn_xa&department=trong_trot&org_ref="+tr.p89.String(), nil); r.status != http.StatusOK {
		t.Fatalf("Admin xem menu Cấp Xã: %v", r.body)
	}

	// Ngưng → menu đóng lại ngay.
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments/"+cn+"/status", map[string]any{"status": "suspended", "reason": "Chưa có cán bộ phụ trách"}),
		http.StatusCreated, "ngưng phòng ban")
	wantCode(t, xa.appDo(http.MethodGet, "/v1/menu?department=chan_nuoi_thuy_san", nil), http.StatusConflict, "party.department_not_open", "sau khi ngưng")
	if r := xa.appDo(http.MethodGet, "/v1/menu", nil); str(r.body, "department", "code") != "trong_trot" {
		t.Fatalf("sau khi ngưng, mặc định lại Trồng trọt: %v", r.body)
	}
}

// W11.06 (Q-051): đơn vị đã nạp seed v7.7 (mã tạm phong_ban_5/6) — nạp lại thêm phòng ban thật; mã tạm
// có tài khoản/dữ liệu giữ nguyên (legacy_kept), hết dùng thì xóa (legacy_retired); sau đó tra cứu mã cũ
// phong_ban_5 trả Phòng Hành chính - Tổng hợp, phong_ban_6 không còn.
func TestW1106_SeedReplacesLegacyTempCodes(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	for _, code := range []string{"phong_ban_5", "phong_ban_6"} {
		wantStatus(t, root.adminDo(http.MethodPost, "/v1/departments", deptBody(tr.p89, code)), http.StatusCreated, "phòng ban tạm v7.7 "+code)
	}
	usage := &fakeUsage{}
	f.mod.UseDepartmentUsage(usage)
	usage.set(1, 0)
	seed := "/v1/orgs/" + tr.p89.String() + "/departments/seed"
	r := root.adminDo(http.MethodPost, seed, nil)
	if created, _ := r.body["created"].([]any); r.status != http.StatusOK || len(created) != 5 ||
		len(r.body["legacy_retired"].([]any)) != 0 || len(r.body["legacy_kept"].([]any)) != 2 {
		t.Fatalf("seed khi mã tạm còn tài khoản: %d %v", r.status, r.body)
	}
	usage.set(0, 0)
	r = root.adminDo(http.MethodPost, seed, nil)
	if retired, _ := r.body["legacy_retired"].([]any); r.status != http.StatusOK || len(retired) != 2 || len(r.body["legacy_kept"].([]any)) != 0 {
		t.Fatalf("seed khi mã tạm hết dùng: %d %v", r.status, r.body)
	}
	if list := items(root.adminDo(http.MethodGet, "/v1/departments?org_ref="+tr.p89.String(), nil)); len(list) != 5 {
		t.Fatalf("còn 5 phòng ban: %v", list)
	}
	r = root.appDo(http.MethodGet, "/v1/departments/phong_ban_5/modules?audience=admin&org_ref="+tr.p89.String(), nil)
	if r.status != http.StatusOK || str(r.body, "department", "code") != "hanh_chinh_tong_hop" {
		t.Fatalf("mã cũ phong_ban_5 → hanh_chinh_tong_hop: %d %v", r.status, r.body)
	}
	wantStatus(t, root.appDo(http.MethodGet, "/v1/departments/phong_ban_6/modules?audience=admin&org_ref="+tr.p89.String(), nil),
		http.StatusNotFound, "phong_ban_6 bỏ")
}

// ---------------------------------------------------------------------------
// FN-AD-04.02 — Khu vực
// ---------------------------------------------------------------------------

// FN-AD-04.02: Khu vực là thuộc tính của Nút xã — gán/đổi/bỏ là sổ (đổi, bỏ cần lý do; cũ → mới); chỉ
// Nút extension_team với khu vực đang dùng của đúng tỉnh; ngừng dùng khu vực còn xã → chặn; Nút xã sang
// tỉnh khác → bản ghi bù bỏ gán trong cùng giao dịch; không di chuyển Nút cấp 1, 2; gọi lặp; khu vực trả
// kèm trong /v1/orgs; cách ly tenant.
func TestFN_AD_04_02_RegionAssignmentCompensating(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	region := func(prov, code, name string) resp {
		return root.adminDo(http.MethodPost, "/v1/regions", map[string]any{"province_code": prov, "code": code, "name": name, "sort_order": 10})
	}
	wantStatus(t, region("30001", "KV1", "Xã không phải tỉnh"), http.StatusUnprocessableEntity, "mã tỉnh là xã")
	r := region("89", "KV1", "Long Xuyên")
	wantStatus(t, r, http.StatusCreated, "tạo khu vực")
	kv1 := str(r.body, "id")
	r = region("89", "KV2", "Châu Đốc")
	kv2 := str(r.body, "id")
	wantCode(t, region("89", "KV1", "Trùng"), http.StatusConflict, "party.region_code_taken", "trùng mã khu vực trong tỉnh")
	wantStatus(t, region("92", "KV9", "Ninh Kiều"), http.StatusCreated, "khu vực tỉnh khác")
	wantStatus(t, f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin).adminDo(http.MethodPost, "/v1/regions",
		map[string]any{"province_code": "89", "code": "KV3", "name": "Tịnh Biên"}), http.StatusForbidden, "Cấp Tỉnh chỉ xem")

	assign := func(ref uuid.UUID, code any, reason string) resp {
		return root.adminDo(http.MethodPost, "/v1/orgs/"+ref.String()+"/region", map[string]any{"region_code": code, "reason": reason})
	}
	r = assign(tr.t30001, "KV1", "")
	if r.status != http.StatusCreated || str(r.body, "action") != "assigned" || str(r.body, "province_code") != "89" {
		t.Fatalf("gán: %d %v", r.status, r.body)
	}
	wantCode(t, assign(tr.t30001, "KV1", ""), http.StatusConflict, "party.region_state", "gán lại cùng khu vực")
	wantStatus(t, assign(tr.t30001, "KV2", ""), http.StatusUnprocessableEntity, "đổi khu vực thiếu lý do")
	// Gọi lặp cùng Idempotency-Key.
	idem := key()
	body := map[string]any{"region_code": "KV2", "reason": "Điều chỉnh theo địa bàn huyện cũ"}
	r1 := root.adminWithKey(http.MethodPost, "/v1/orgs/"+tr.t30001.String()+"/region", idem, body)
	r2 := root.adminWithKey(http.MethodPost, "/v1/orgs/"+tr.t30001.String()+"/region", idem, body)
	if r1.status != http.StatusCreated || str(r1.body, "action") != "changed" || str(r2.body, "region_code") != "KV2" || r2.hdr.Get("Idempotent-Replayed") == "" {
		t.Fatalf("đổi khu vực + gọi lặp: %v / %v", r1.body, r2.body)
	}
	wantCode(t, assign(tr.c1, "KV1", ""), http.StatusUnprocessableEntity, "party.region_node_invalid", "gán cho HTX")
	wantCode(t, assign(tr.t30002, "KV9", ""), http.StatusUnprocessableEntity, "party.region_node_invalid", "khu vực của tỉnh khác")
	wantStatus(t, assign(tr.t30002, "KV1", ""), http.StatusCreated, "gán xã B")

	// Danh sách khu vực (Application API, mọi vai) kèm số xã; tổng = số xã đã gán.
	r = f.as(f.tenant, tr.t30001, port.RoleKNOfficer).appDo(http.MethodGet, "/v1/regions?province_code=89", nil)
	if list := items(r); r.status != http.StatusOK || len(list) != 2 || list[0]["commune_count"] != float64(1) || list[1]["commune_count"] != float64(1) ||
		r.body["assigned_communes"] != float64(2) {
		t.Fatalf("khu vực của tỉnh: %v", r.body)
	}
	// Khu vực trả kèm trong /v1/orgs.
	r = root.appDo(http.MethodGet, "/v1/orgs?kind=extension_team", nil)
	for _, it := range items(r) {
		if it["ref"] == tr.t30001.String() {
			if reg, _ := it["region"].(map[string]any); reg == nil || reg["code"] != "KV2" || reg["name"] != "Châu Đốc" {
				t.Fatalf("/v1/orgs thiếu khu vực: %v", it)
			}
		}
	}

	// Ngừng dùng khu vực còn xã → chặn.
	wantCode(t, root.adminDo(http.MethodPost, "/v1/regions/"+kv2+"/deactivate", map[string]any{"reason": "Gộp vào khu vực Long Xuyên"}),
		http.StatusConflict, "party.region_in_use", "ngừng khu vực còn xã")

	// Không di chuyển Nút cấp 1, 2.
	wantCode(t, root.adminDo(http.MethodPost, "/v1/orgs/"+tr.p92.String()+"/move", map[string]any{"new_parent_ref": nil, "reason": "Thử đưa tỉnh lên gốc"}),
		http.StatusUnprocessableEntity, "party.invalid_hierarchy", "di chuyển Nút cấp 2")
	// Nút xã sang tỉnh khác → bản ghi bù bỏ gán (cùng giao dịch với sự kiện moved).
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/orgs/"+tr.t30001.String()+"/move", map[string]any{"new_parent_ref": tr.p92.String(),
		"new_admin_unit_code": "31001", "reason": "Sáp nhập địa bàn sang Cần Thơ"}), http.StatusOK, "di chuyển Nút xã")
	es := logItems(t, root.adminDo(http.MethodGet, "/v1/orgs/"+tr.t30001.String()+"/region-log", nil))
	var evs []string
	for _, e := range es {
		evs = append(evs, e["event"].(string))
	}
	if !slices.Equal(evs, []string{"assigned", "changed", "cleared"}) {
		t.Fatalf("lịch sử khu vực: %v", evs)
	}
	if o, n, _ := changeOf(es[1], "region_code"); o != "KV1" || n != "KV2" {
		t.Fatalf("đổi khu vực cũ → mới: %v", es[1])
	}
	if o, n, _ := changeOf(es[2], "region_code"); o != "KV2" || n != nil || !strings.Contains(es[2]["reason"].(string), "Sáp nhập địa bàn") {
		t.Fatalf("bản ghi bù khi di chuyển: %v", es[2])
	}
	// Hết xã → ngừng dùng được; Log khu vực có lý do.
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/regions/"+kv2+"/deactivate", map[string]any{"reason": "Gộp vào khu vực Long Xuyên"}), http.StatusCreated, "ngừng khu vực")
	wantCode(t, assign(tr.t30002, "KV2", "Chuyển sang khu vực đã ngừng"), http.StatusUnprocessableEntity, "party.region_node_invalid", "gán khu vực đã ngừng")
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/regions/"+kv2+"/reactivate", nil), http.StatusCreated, "dùng lại")
	wantStatus(t, root.adminDo(http.MethodPut, "/v1/regions/"+kv1, map[string]any{"name": "TP Long Xuyên", "sort_order": 1}), http.StatusOK, "sửa khu vực")
	es = logItems(t, root.adminDo(http.MethodGet, "/v1/regions/"+kv2+"/log", nil))
	if len(es) != 3 || es[1]["event"] != "deactivated" || es[1]["reason"] != "Gộp vào khu vực Long Xuyên" {
		t.Fatalf("log khu vực: %v", es)
	}
	if o, n, _ := changeOf(logItems(t, root.adminDo(http.MethodGet, "/v1/regions/"+kv1+"/log", nil))[1], "name"); o != "Long Xuyên" || n != "TP Long Xuyên" {
		t.Fatal("log sửa tên khu vực")
	}

	// RB-1: sổ gán khu vực chỉ ghi thêm.
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "DELETE FROM pf_party.org_region_event")
		return e
	}); err == nil {
		t.Fatal("DELETE sổ gán khu vực phải bị chặn (RB-1)")
	}
	// Cách ly tenant.
	other := f.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin)
	if list := items(other.appDo(http.MethodGet, "/v1/regions?province_code=89", nil)); len(list) != 0 {
		t.Fatalf("tenant khác thấy khu vực: %v", list)
	}
	wantCode(t, other.adminDo(http.MethodPost, "/v1/orgs/"+tr.t30002.String()+"/region", map[string]any{"region_code": "KV1"}),
		http.StatusNotFound, "party.org_not_found", "tenant khác gán khu vực")
}

// FN-AD-04.02 (An Giang 102 xã/phường/đặc khu): bảo đảm mỗi xã có Nút extension_team gắn mã ĐVHC (gọi
// lặp không tạo thêm); nạp Khu vực tạm (Q-04) gán đủ 102 xã; cây TTKNQG › TTKN tỉnh › Khu vực › Xã;
// tổng theo khu vực: Σ số xã các khu vực = số xã của tỉnh, Σ giá trị các khu vực = Toàn tỉnh (P4 dùng
// port.SumByRegion).
func TestFN_AD_04_02_AnGiangCommunesRegionSums(t *testing.T) {
	f := newFixture(t)
	raw, err := os.ReadFile("../seeddata/regions_an_giang.json")
	if err != nil {
		t.Fatal(err)
	}
	var seed struct {
		Regions     []struct{ Code string } `json:"regions"`
		Assignments []struct {
			CommuneCode string `json:"commune_code"`
			RegionCode  string `json:"region_code"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &seed); err != nil {
		t.Fatal(err)
	}
	if len(seed.Assignments) != 102 || len(seed.Regions) != 26 {
		t.Fatalf("dữ liệu nền An Giang: %d xã, %d khu vực", len(seed.Assignments), len(seed.Regions))
	}
	f.units.add("91", "An Giang", port.LevelProvince, "")
	for _, a := range seed.Assignments {
		f.units.add(a.CommuneCode, "Xã "+a.CommuneCode, port.LevelCommune, "91")
	}
	root := f.root()
	national := root.createOrg(map[string]any{"kind": "ttkn_national", "code": "TTKNQG", "name": "Trung tâm Khuyến nông Quốc gia"})
	p91 := root.createOrg(map[string]any{"kind": "ttkn_province", "code": "TTKN-AG", "name": "TTKN An Giang", "admin_unit_code": "91", "parent_ref": national.String()})

	r := root.adminDo(http.MethodPost, "/v1/orgs/"+p91.String()+"/communes/ensure", map[string]any{"code_prefix": "AG"})
	if created, _ := r.body["created"].([]any); r.status != http.StatusOK || len(created) != 102 || r.body["communes"] != float64(102) {
		t.Fatalf("bảo đảm Nút xã: %d %v", r.status, r.body["communes"])
	}
	r = root.adminDo(http.MethodPost, "/v1/orgs/"+p91.String()+"/communes/ensure", map[string]any{"code_prefix": "AG"})
	if created, _ := r.body["created"].([]any); len(created) != 0 || r.body["existing"] != float64(102) {
		t.Fatalf("bảo đảm lặp: %v", r.body)
	}
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/orgs/"+national.String()+"/communes/ensure", nil), http.StatusUnprocessableEntity, "Nút cấp 1")

	wantCode(t, root.adminDo(http.MethodPost, "/v1/regions/seed", map[string]any{"province_code": "92"}), http.StatusNotFound, "party.seed_unavailable", "tỉnh chưa có dữ liệu nền")
	r = root.adminDo(http.MethodPost, "/v1/regions/seed", map[string]any{"province_code": "91"})
	if r.status != http.StatusOK || r.body["regions_created"] != float64(26) || r.body["assigned"] != float64(102) || len(r.body["missing_node"].([]any)) != 0 {
		t.Fatalf("nạp khu vực: %d %v", r.status, r.body)
	}
	r = root.adminDo(http.MethodPost, "/v1/regions/seed", map[string]any{"province_code": "91"})
	if r.body["regions_created"] != float64(0) || r.body["assigned"] != float64(0) || r.body["already_assigned"] != float64(102) {
		t.Fatalf("nạp lặp: %v", r.body)
	}

	// Σ số xã các khu vực = 102.
	r = root.appDo(http.MethodGet, "/v1/regions?province_code=91", nil)
	sum, perRegion := 0, map[string]int{}
	for _, it := range items(r) {
		n := int(it["commune_count"].(float64))
		sum += n
		perRegion[it["code"].(string)] = n
	}
	if len(perRegion) != 26 || sum != 102 || r.body["assigned_communes"] != float64(102) {
		t.Fatalf("tổng xã theo khu vực: %d / %v", sum, r.body)
	}

	// Cây: TTKNQG › TTKN An Giang › 26 Khu vực › 102 xã (không có nhóm "Chưa gán").
	r = root.appDo(http.MethodGet, "/v1/orgs/tree", nil)
	roots := items(r)
	if len(roots) != 1 || roots[0]["level"] != float64(1) {
		t.Fatalf("gốc cây: %v", r.body)
	}
	prov := roots[0]["children"].([]any)[0].(map[string]any)
	groups := prov["children"].([]any)
	treeSum := 0
	for _, g := range groups {
		gm := g.(map[string]any)
		if gm["kind"] != "region" {
			t.Fatalf("nhóm cây không phải khu vực: %v", gm["kind"])
		}
		n := len(gm["children"].([]any))
		if gm["commune_count"] != float64(n) || perRegion[gm["code"].(string)] != n {
			t.Fatalf("khu vực %v: %d xã", gm["code"], n)
		}
		treeSum += n
	}
	if prov["commune_count"] != float64(102) || len(groups) != 26 || treeSum != 102 {
		t.Fatalf("TTKN An Giang trong cây: %v xã, %d khu vực", prov["commune_count"], len(groups))
	}

	// port.Regions + port.SumByRegion (dòng tổng Khu vực / Toàn tỉnh của P4).
	ctx := tenant.WithID(context.Background(), f.tenant)
	assign, err := f.mod.Regions().CommuneRegions(ctx, "91")
	if err != nil || len(assign) != 102 {
		t.Fatalf("CommuneRegions: %d %v", len(assign), err)
	}
	values := map[string]decimal.Decimal{}
	for i, a := range seed.Assignments {
		values[a.CommuneCode] = decimal.RequireFromString("12.3456").Add(decimal.NewFromInt(int64(i)))
	}
	values["99999"] = decimal.RequireFromString("1.5") // xã chưa có Nút → "Chưa gán"
	tot := port.SumByRegion(values, assign)
	acc, communes := decimal.Zero, 0
	for _, code := range tot.Regions() {
		acc = acc.Add(tot.ByRegion[code])
		communes += tot.Communes[code]
		if code != port.Unassigned && tot.Communes[code] != perRegion[code] {
			t.Fatalf("SumByRegion %s: %d xã, muốn %d", code, tot.Communes[code], perRegion[code])
		}
	}
	if !acc.Equal(tot.Province) || communes != 103 || !tot.ByRegion[port.Unassigned].Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("Σ khu vực = Toàn tỉnh: %s / %s, %d xã", acc, tot.Province, communes)
	}
	want := decimal.Zero
	for _, v := range values {
		want = want.Add(v)
	}
	if !tot.Province.Equal(want) {
		t.Fatalf("Toàn tỉnh: %s, muốn %s", tot.Province, want)
	}
}

// FN-AD-04.02 bảng "Ảnh hưởng" (tài khoản, HTX, thửa, kỳ chưa chốt — đếm qua port, chưa nối thì báo
// "available: false"), phạm vi cây (xem xuống, không ngang, không lên), chỉ Admin, cách ly tenant.
func TestFN_AD_04_02_ImpactAndTreeScope(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)

	r := root.adminDo(http.MethodGet, "/v1/orgs/"+tr.t30001.String()+"/impact", nil)
	wantStatus(t, r, http.StatusOK, "ảnh hưởng Nút xã")
	rows := map[string]map[string]any{}
	for _, it := range r.body["rows"].([]any) {
		m := it.(map[string]any)
		rows[m["kind"].(string)] = m
	}
	if rows["cooperatives"]["count"] != float64(1) || rows["accounts"]["available"] != false || rows["accounts"]["count"] != nil ||
		rows["plots"]["available"] != false || r.body["movable"] != true {
		t.Fatalf("ảnh hưởng chưa nối nguồn: %v", r.body)
	}
	f.mod.UseOrgImpact(fakeImpact{kind: port.ImpactAccounts, n: 3})
	f.mod.UseOrgImpact(fakeImpact{kind: port.ImpactOpenPeriods, n: 2})
	r = root.adminDo(http.MethodGet, "/v1/orgs/"+tr.p89.String()+"/impact", nil)
	rows = map[string]map[string]any{}
	for _, it := range r.body["rows"].([]any) {
		m := it.(map[string]any)
		rows[m["kind"].(string)] = m
	}
	if rows["accounts"]["count"] != float64(3) || rows["open_periods"]["count"] != float64(2) || rows["cooperatives"]["count"] != float64(1) ||
		r.body["movable"] != false || len(r.body["warnings"].([]any)) != 2 || len(r.body["communes"].([]any)) != 3 {
		t.Fatalf("ảnh hưởng Nút tỉnh: %v", r.body)
	}
	wantStatus(t, f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin).adminDo(http.MethodGet, "/v1/orgs/"+tr.p89.String()+"/impact", nil),
		http.StatusForbidden, "Cấp Tỉnh xem bảng ảnh hưởng")

	// Phạm vi cây: Cấp Tỉnh 92 chỉ thấy nhánh 92; Cấp Xã chỉ thấy xã mình.
	r = f.as(f.tenant, tr.p92, port.RoleKNProvinceAdmin).appDo(http.MethodGet, "/v1/orgs/tree", nil)
	if roots := items(r); len(roots) != 1 || roots[0]["ref"] != tr.p92.String() {
		t.Fatalf("cây của Cấp Tỉnh 92: %v", r.body)
	}
	r = f.as(f.tenant, tr.t30002, port.RoleKNOfficer).appDo(http.MethodGet, "/v1/orgs/tree", nil)
	if roots := items(r); len(roots) != 1 || roots[0]["ref"] != tr.t30002.String() || len(roots[0]["children"].([]any)) != 0 {
		t.Fatalf("cây của Cấp Xã: %v", r.body)
	}
	// Admin: cây đầy đủ, xã chưa gán khu vực vào nhóm "Chưa gán khu vực".
	r = root.appDo(http.MethodGet, "/v1/orgs/tree", nil)
	var p89 map[string]any
	for _, ch := range items(r)[0]["children"].([]any) {
		if m := ch.(map[string]any); m["ref"] == tr.p89.String() {
			p89 = m
		}
	}
	if p89 == nil || p89["commune_count"] != float64(2) || p89["children"].([]any)[0].(map[string]any)["kind"] != "region_unassigned" {
		t.Fatalf("cây Admin: %v", r.body)
	}
	// Cách ly tenant.
	other := f.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin)
	if roots := items(other.appDo(http.MethodGet, "/v1/orgs/tree", nil)); len(roots) != 0 {
		t.Fatalf("tenant khác thấy cây: %v", roots)
	}
	wantCode(t, other.adminDo(http.MethodGet, "/v1/orgs/"+tr.p89.String()+"/impact", nil), http.StatusNotFound, "party.org_not_found", "tenant khác xem ảnh hưởng")
}

// hasMenuChild checks the route contract by identity, independent of new module registrations.
func hasMenuChild(parent map[string]any, key, route string) bool {
	children, _ := parent["children"].([]any)
	for _, raw := range children {
		child, ok := raw.(map[string]any)
		if ok && child["key"] == key && child["route"] == route {
			return true
		}
	}
	return false
}

func hasMenuChildren(parent map[string]any, keys []string) bool {
	children, _ := parent["children"].([]any)
	for _, key := range keys {
		found := false
		for _, raw := range children {
			child, ok := raw.(map[string]any)
			if ok && child["key"] == key {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
