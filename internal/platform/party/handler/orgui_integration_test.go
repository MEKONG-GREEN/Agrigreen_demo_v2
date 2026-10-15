package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// ORGUI — quyết định PO 14/10/2026: Nút hành chính / TTKN (kể cả xã) chỉ Admin nền tảng tạo;
// vai KN không tạo nhưng vẫn đổi tên / di chuyển Nút cũ trong phạm vi.
func TestOrgNodeCreateOnlyPlatformAdmin(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	team := func(code string) map[string]any {
		return map[string]any{"kind": "extension_team", "code": code, "name": "Tổ KN " + code, "admin_unit_code": "30002", "parent_ref": tr.p89.String()}
	}

	// KN tỉnh tạo xã trong chính tỉnh của mình → 403 forbidden (trước đây 201).
	r := prov.adminDo(http.MethodPost, "/v1/orgs", team("TKN-KN-"+ids.New().String()[:6]))
	wantCode(t, r, http.StatusForbidden, "forbidden", "KN tỉnh tạo Nút xã")
	if !strings.Contains(str(r.body, "detail"), "Admin nền tảng") {
		t.Fatalf("403 phải nêu lý do: %v", r.body)
	}
	for _, kind := range []map[string]any{
		{"kind": "ttkn_province", "code": "TTKN-KN-" + ids.New().String()[:6], "name": "TTKN tỉnh do KN tạo", "admin_unit_code": "89", "parent_ref": tr.national.String()},
		{"kind": "partner", "code": "DT-KN-" + ids.New().String()[:6], "name": "Đối tác do KN tạo"},
	} {
		wantCode(t, prov.adminDo(http.MethodPost, "/v1/orgs", kind), http.StatusForbidden, "forbidden", "KN tỉnh tạo "+kind["kind"].(string))
	}
	// kn_national_admin không thuộc vai quản trị dữ liệu chủ → 403 như trước.
	wantStatus(t, f.as(f.tenant, tr.national, port.RoleKNNationalAdmin).adminDo(http.MethodPost, "/v1/orgs", team("TKN-QG-"+ids.New().String()[:6])),
		http.StatusForbidden, "KN QG tạo Nút xã")

	// ORG-CREATE-001: mọi vai Admin cũng phải dùng luồng Neo để tạo HTX.
	for _, u := range []caller{root, f.as(f.tenant, tr.national, port.RoleRegistryAdmin)} {
		r := u.adminDo(http.MethodPost, "/v1/orgs", map[string]any{"kind": "cooperative", "name": "HTX tạo vòng", "admin_unit_code": "30001"})
		wantCode(t, r, http.StatusUnprocessableEntity, "party.cooperative_requires_anchor", "HTX phải qua Neo")
		if str(r.body, "redirect") != "/quan-tri/neo-htx" {
			t.Fatalf("thiếu đường chuyển: %v", r.body)
		}
	}
	// Admin nền tảng: registry_admin riêng (gắn Nút TW để có phạm vi) và platform_admin riêng đều tạo được.
	wantStatus(t, f.as(f.tenant, tr.national, port.RoleRegistryAdmin).adminDo(http.MethodPost, "/v1/orgs", team("TKN-RA-"+ids.New().String()[:6])),
		http.StatusCreated, "registry_admin tạo Nút xã")
	wantStatus(t, f.as(f.tenant, uuid.Nil, "platform_admin").adminDo(http.MethodPost, "/v1/orgs", team("TKN-PA-"+ids.New().String()[:6])),
		http.StatusCreated, "platform_admin tạo Nút xã")

	// Vai KN giữ đổi tên và di chuyển Nút cũ trong tỉnh; không tạo HTX qua tổ chức chung.
	wantStatus(t, prov.adminDo(http.MethodPost, "/v1/orgs/"+tr.t30002.String()+"/rename", map[string]any{"name": "Tổ KNCĐ Xã B (đổi tên)", "reason": "Đổi tên theo quyết định tỉnh"}),
		http.StatusOK, "KN tỉnh đổi tên Nút xã")
	wantStatus(t, prov.adminDo(http.MethodPost, "/v1/orgs/"+tr.c1.String()+"/move", map[string]any{"new_parent_ref": tr.t30002.String(), "new_admin_unit_code": "30002", "reason": "Sáp nhập xã theo nghị quyết"}),
		http.StatusOK, "KN tỉnh di chuyển Nút trong tỉnh")
	wantStatus(t, prov.adminDo(http.MethodPost, "/v1/orgs", map[string]any{"kind": "cooperative", "name": "HTX do KN tỉnh lập " + ids.New().String()[:6], "admin_unit_code": "30001", "parent_ref": tr.t30001.String()}),
		http.StatusForbidden, "KN tỉnh không tạo HTX qua tổ chức chung")
}

// ORGUI — nhập cây bởi vai KN: dòng tạo Nút mới bị loại theo dòng (không 403 cả tệp), dòng có sẵn vẫn xử lý;
// dòng HTX mới bị loại với lý do chỉ sang Neo HTX (D2) dù người nhập là Admin nền tảng.
func TestOrgImportCreateRulesORGUI(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	header := []string{"Mã tổ chức", "Loại", "Tên", "Mã đơn vị hành chính", "Mã tổ chức cấp trên", "Trạng thái"}
	var p89 string
	for _, o := range items(root.adminDo(http.MethodGet, "/v1/orgs?limit=200", nil)) {
		if o["ref"] == tr.p89.String() {
			p89 = o["code"].(string)
		}
	}
	if p89 == "" {
		t.Fatal("không thấy mã TTKN tỉnh 89")
	}
	newTeam := "TKN-IMP-" + ids.New().String()[:6]

	body := xlsxBytes(t, header, [][]string{
		{newTeam, "extension_team", "Tổ mới qua tệp", "30002", p89, ""},
		{"", "cooperative", "HTX mới qua tệp", "30001", "", ""},
	})
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/orgs/import", key(), tabular.ContentTypeXLSX, body)
	if r.status != http.StatusOK || r.body["created"] != float64(0) || r.body["rejected"] != float64(2) {
		t.Fatalf("KN tỉnh nhập tệp có dòng tạo mới: %d %v", r.status, r.body)
	}
	res := rowsOf(r)
	if !strings.Contains(res[0]["reason"].(string), "Admin nền tảng") {
		t.Fatalf("dòng tạo Nút xã bởi KN phải nêu lý do quyền: %v", res[0])
	}
	if !strings.Contains(res[1]["reason"].(string), "Neo HTX") || !strings.Contains(res[1]["reason"].(string), "/quan-tri/neo-htx") {
		t.Fatalf("dòng HTX mới phải chỉ sang Neo HTX: %v", res[1])
	}

	// Admin nền tảng: Nút xã tạo được; HTX mới vẫn bị loại.
	r = root.call(f.admin.URL, http.MethodPost, "/v1/orgs/import", key(), tabular.ContentTypeXLSX, body)
	res = rowsOf(r)
	if r.status != http.StatusOK || res[0]["status"] != "created" || res[1]["status"] != "rejected" || !strings.Contains(res[1]["reason"].(string), "Neo HTX") {
		t.Fatalf("Admin nền tảng nhập: %d %v", r.status, r.body)
	}
}
