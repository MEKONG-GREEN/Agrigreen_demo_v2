package knaccounts_test

// KNSPLIT-2-ATTRIBUTE-CATALOG-KN (#79): catalog thuộc tính do KN khai báo (extension.AttributeCatalog) cho cùng hành
// vi HTTP như bridge cũ trên PostgreSQL thật: danh mục HTTP, nhãn lần gán, thuộc tính (nhóm/nhiệm vụ), nhập CSV theo
// nhãn/mã cấp quản lý, ngữ cảnh phiên và quyền chỉ xem ở phòng mình/phòng khác. Chạy thêm lượt bật đồng bộ cả ba
// adapter chủ KN (#95 vai, #72 luật lần gán, #79 catalog) như rollout dự kiến.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// appDo gọi Application API với ngữ cảnh phiên tùy chọn (X-AG-Context).
func (f *fixture) appDo(user uuid.UUID, method, path, ctxID string, body any) resp {
	f.t.Helper()
	c := caller{f: f, token: f.iss.Sign(f.t, authztest.Token{Subject: user, TenantID: f.tenant, Roles: []string{accport.RoleKNUser}})}
	saved := f.srv
	f.srv = f.app
	defer func() { f.srv = saved }()
	if ctxID == "" {
		return c.do(method, path, body)
	}
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	return c.rawWith(method, path, "k-"+ids.New().String(), "application/json", b, map[string]string{authz.ContextHeader: ctxID})
}

func observeAttributeCatalog(t *testing.T, d fixtureDeps) map[string]any {
	f := newFixture(t, d)
	root := f.as(f.national, authz.RolePlatformAdmin)
	out := map[string]any{}
	record := func(r resp) map[string]any { return map[string]any{"status": r.status, "body": f.normalize(r.body)} }

	// 1. Danh mục HTTP.
	out["catalog"] = record(root.do(http.MethodGet, "/v1/accounts/mgmt-levels", nil))

	// 2. Tạo tài khoản KN: Cấp Tỉnh với từng cấp quản lý (phòng trồng trọt + BVTV), Cấp Xã, Cấp Quốc gia.
	phone := 0
	create := func(as ...map[string]any) resp {
		phone++
		list := make([]any, len(as))
		for i := range as {
			list[i] = as[i]
		}
		return root.do(http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Lê Thị Hoa",
			"email": "attr-" + ids.New().String()[24:] + "@vi-du.vn", "phone": fmt.Sprintf("0913%06d", phone), "assignments": list})
	}
	type user struct {
		id  uuid.UUID
		aid string
	}
	users := map[string]user{}
	levels := []string{"", "chuyen_vien", "phu_trach", "pho_truong_phong", "truong_phong", "lanh_dao_don_vi", "pho_giam_doc", "giam_doc"}
	for _, lvl := range levels {
		r := create(f.asgBody(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: lvl}))
		out["create/tinh/"+lvl] = record(r)
		if r.status == http.StatusCreated {
			list := r.body["assignments"].([]any)
			users["tinh/"+lvl] = user{id: uuid.MustParse(userID(r)), aid: list[0].(map[string]any)["assignment_id"].(string)}
		}
	}
	for name, c := range map[string]asgCase{
		"xa": {role: authz.KNXa, org: "p91", depts: []string{"trong_trot"}, communes: []string{"30292"}, mgmt: "chuyen_vien"},
		"tw": {role: authz.KNTW, org: "national", depts: []string{"trong_trot"}, mgmt: "giam_doc"},
	} {
		r := create(f.asgBody(c))
		out["create/"+name] = record(r)
		list := r.body["assignments"].([]any)
		users[name] = user{id: uuid.MustParse(userID(r)), aid: list[0].(map[string]any)["assignment_id"].(string)}
	}

	// 3. Thuộc tính: nhóm × nhiệm vụ (kể cả mã lạ, nhóm cho vai không có nhóm).
	attrs := func(name string, u user, group string, duties []string) {
		r := root.do(http.MethodPut, "/v1/accounts/users/"+u.id.String()+"/assignments/"+u.aid+"/attributes",
			map[string]any{"province_group": group, "duties": duties, "reason": "Phân công theo quyết định của Giám đốc"})
		out["attrs/"+name] = record(r)
	}
	groups := []string{"", "vien_chuc", "tong_hop", "chu_nhiem", "lanh_dao_phong", "ban_giam_doc", "nhom_la"}
	for _, g := range groups {
		attrs("tinh/chuyen_vien/"+g, users["tinh/chuyen_vien"], g, nil)
	}
	attrs("tinh/chuyen_vien/duties", users["tinh/chuyen_vien"], "tong_hop", []string{authz.DutyPriceAdmin, authz.DutyFormOwner, authz.DutyScheduleFollower})
	attrs("tinh/chuyen_vien/duty-la", users["tinh/chuyen_vien"], "tong_hop", []string{"nhiem_vu_la"})
	attrs("xa/group", users["xa"], "vien_chuc", nil)
	attrs("xa/duty", users["xa"], "", []string{authz.DutyFormOwner})
	attrs("tw/group", users["tw"], "lanh_dao_phong", nil)
	attrs("tinh/truong_phong/board", users["tinh/truong_phong"], "ban_giam_doc", nil)
	attrs("tinh/pho_giam_doc/leader", users["tinh/pho_giam_doc"], "lanh_dao_phong", nil)

	// 4. Nhãn lần gán và ngữ cảnh phiên: phòng mình (trong_trot) / phòng khác (BVTV) / phòng chưa mở.
	keys := make([]string, 0, len(users))
	for k := range users {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		u := users[k]
		out["list/"+k] = record(root.do(http.MethodGet, "/v1/accounts/users/"+u.id.String()+"/assignments", nil))
		out["contexts/"+k] = record(f.appDo(u.id, http.MethodGet, "/v1/me/contexts", "", nil))
		for _, dept := range []string{"trong_trot", "bao_ve_thuc_vat", "chan_nuoi"} {
			r := f.appDo(u.id, http.MethodPost, "/v1/me/context", "", map[string]any{"assignment_id": u.aid, "department_code": dept})
			out["select/"+k+"/"+dept] = record(r)
			if id, _ := r.body["context_id"].(string); id != "" {
				out["current/"+k+"/"+dept] = record(f.appDo(u.id, http.MethodGet, "/v1/me/context", id, nil))
			}
		}
	}

	// 5. Nhập CSV tài khoản KN: cột cấp quản lý theo mã, nhãn, chữ hoa/khoảng trắng và mã lạ.
	csv := "ho_ten,email,so_dien_thoai,vai_tro,ma_nut,phong_ban,cap_quan_ly\n"
	for i, cell := range []string{"chuyen_vien", "Trưởng phòng", "LÃNH ĐẠO ĐƠN VỊ", "  Phó Giám đốc ", "giam doc", "Không có", "Ban Giám đốc (chỉ xem)", ""} {
		csv += fmt.Sprintf("Cán bộ %d,csv%d-%s@vi-du.vn,0914%06d,kn_tinh,%s,trong_trot,%s\n", i, i, f.tenant.String()[:8], i, f.p91, cell)
	}
	imp := root.raw(http.MethodPost, "/v1/accounts/kn-users/import", "k-"+f.tenant.String(), "text/csv", []byte(csv))
	out["import"] = record(imp)
	return out
}

// rewriteImportReason thay lý do từ chối của các dòng nhập CSV (status "rejected"), trả số dòng đã thay.
func rewriteImportReason(obs map[string]any, from, to string) int {
	n := 0
	body, _ := obs["import"].(map[string]any)["body"].(map[string]any)
	rows, _ := body["rows"].([]any)
	for _, r := range rows {
		m := r.(map[string]any)
		if m["status"] == "rejected" && m["reason"] == from {
			m["reason"] = to
			n++
		}
	}
	return n
}

func diffObservations(t *testing.T, label string, before, after map[string]any) {
	t.Helper()
	var diffs []string
	for k, b := range before {
		bj, _ := json.Marshal(b)
		aj, _ := json.Marshal(after[k])
		if string(bj) != string(aj) {
			diffs = append(diffs, k+"\n  legacy: "+string(bj)+"\n  "+label+": "+string(aj))
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 || len(before) != len(after) {
		t.Fatalf("%s: %d observable differences (of %d/%d):\n%s", label, len(diffs), len(before), len(after), strings.Join(diffs, "\n"))
	}
}

func TestKNAttributeCatalogHTTPParityWithLegacyOnPostgres(t *testing.T) {
	attrs, err := extension.AttributeCatalog()
	if err != nil {
		t.Fatal(err)
	}
	roles, err := extension.RoleCatalog(accport.NativeRoleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	before := observeAttributeCatalog(t, fixtureDeps{})
	diffObservations(t, "kn-attributes", before, observeAttributeCatalog(t, fixtureDeps{Attributes: attrs}))
	all := observeAttributeCatalog(t, fixtureDeps{Attributes: attrs, Roles: roles, AssignmentRules: extension.AssignmentRules()})
	// Khác biệt ĐÃ BIẾT, thuộc thiết kế #68 (không thuộc adapter KN): khi có AssignmentRules, accounts kiểm cú pháp mã
	// cấp quản lý TRƯỚC khi gọi adapter, nên ô CSV là nhãn lạ (vd "Không có") báo "mã cấp quản lý không hợp lệ" thay
	// cho câu danh mục; dòng vẫn bị từ chối như cũ. Chốt đúng 2 dòng (7, 8) rồi so phần còn lại giống hệt.
	if n := rewriteImportReason(all, "mã cấp quản lý không hợp lệ", "cấp quản lý không có trong danh mục (tạm, chờ khách Q-05)"); n != 2 {
		t.Fatalf("known structure-first CSV difference: want 2 rows, got %d", n)
	}
	diffObservations(t, "kn-all-owners", before, all)

	// Không vô nghĩa: ma trận chạm các điểm chốt của quyền.
	js, _ := json.Marshal(all)
	s := string(js)
	for _, want := range []string{`"read_only":true`, `"read_only":false`, "Ban Giám đốc (chỉ xem)", "Lãnh đạo đơn vị", "Người phụ trách biểu"} {
		if !strings.Contains(s, want) {
			t.Fatalf("matrix lacks %q", want)
		}
	}
	statuses := map[int]int{}
	for k, v := range all {
		if strings.HasPrefix(k, "attrs/") {
			statuses[v.(map[string]any)["status"].(int)]++
		}
	}
	if statuses[http.StatusOK] == 0 || statuses[http.StatusUnprocessableEntity] == 0 {
		t.Fatalf("attributes matrix lacks accepted or rejected cases: %v", statuses)
	}
}

// Catalog cấu hình không hợp lệ không bao giờ rơi về bridge: accounts.New từ chối lúc khởi động.
func TestKNAttributeCatalogInvalidConfigurationFailsClosed(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("invalid attribute catalog was accepted")
		}
	}()
	newFixture(t, fixtureDeps{Attributes: &accport.AttributeCatalog{}})
}
