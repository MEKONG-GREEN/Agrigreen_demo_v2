package masterdata_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

// fakeFormUsage giả P3: số mẫu báo cáo dùng mỗi mục.
type fakeFormUsage map[string]int

func (f fakeFormUsage) FormsUsing(_ context.Context, catalog string, codes []string) (map[string]int, error) {
	out := map[string]int{}
	for _, c := range codes {
		if n, ok := f[catalog+"/"+c]; ok {
			out[c] = n
		}
	}
	return out, nil
}

// knOfficer là cán bộ KN như luồng thật: luôn có org_ref (Nút công tác). FD chặn kn_officer không Nút
// (403 node_scope_required) ngay khi mở giao dịch của module.
func knOfficer(t *testing.T, f *fixture, tid uuid.UUID) caller {
	c := f.caller(tid, "kn_officer")
	c.token = f.iss.Sign(t, authztest.Token{Subject: c.actor, TenantID: tid, Roles: []string{"kn_officer"}, MFA: true, OrgRef: ids.New()})
	return c
}

func codesOf(body map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	items, _ := body["items"].([]any)
	for _, it := range items {
		m := it.(map[string]any)
		out[m["code"].(string)] = m
	}
	return out
}

// KN v7.7 FN-AD-04.05 (gói P2, pf 000063): sáu tab Danh mục nông nghiệp — Giống lúa, Vật tư, Cây trồng
// (nhóm › cây), Sâu bệnh (gắn cây trồng bị ảnh hưởng, ngưỡng không bắt buộc), Loại máy, Mô hình — và
// loại báo cáo. Trùng mã/tên → chặn; tham chiếu phải là mục đang dùng; vô hiệu hóa giữ lịch sử
// (cũ → mới); gọi lặp cùng Idempotency-Key; cột "biểu đang dùng"; cách ly tenant.
func TestFN_AD_04_05_AgriCatalogs(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := knOfficer(t, f, tid)

	// Dữ liệu nền tạm (Q-01) có đủ danh mục mới, kể cả loại báo cáo "dot_xuat" cho menu đột xuất.
	for catalog, code := range map[string]string{port.CatalogCrop: "RAU_MUONG", port.CatalogMaterial: "PHAN_URE",
		port.CatalogProductionModel: "1P5G", port.CatalogReportType: "dot_xuat"} {
		if st, body := reader.get("/v1/masterdata/catalogs/" + catalog + "/" + code); st != http.StatusOK {
			t.Fatalf("seed %s/%s: %d %v", catalog, code, st, body)
		}
	}
	st, body := admin.adm(http.MethodGet, "/v1/masterdata/catalogs", nil)
	if st != http.StatusOK {
		t.Fatalf("catalogs: %d %v", st, body)
	}
	names := map[string]string{}
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		names[m["catalog"].(string)] = m["name"].(string)
	}
	if names["crop"] != "Cây trồng" || names["material"] != "Vật tư" || names["production_model"] != "Mô hình sản xuất" || names["report_type"] != "Loại báo cáo" {
		t.Fatalf("tên danh mục: %v", names)
	}

	// Cây trồng phân cấp: cha phải là nhóm cấp 1 đang dùng, không phải chính nó.
	crop := func(code, name, parent string) (int, map[string]any) {
		attrs := map[string]string{"is_group": "false"}
		if parent != "" {
			attrs["parent_code"] = parent
		}
		return admin.adm(http.MethodPost, "/v1/masterdata/catalogs/crop/items", map[string]any{"code": code, "name": name, "attrs": attrs})
	}
	if st, body := crop("DUA_HAU", "Dưa hấu", "KHONG_CO"); st != http.StatusUnprocessableEntity || body["code"] != "masterdata.catalog_ref_invalid" {
		t.Fatalf("cha không có: %d %v", st, body)
	}
	if st, body := crop("DUA_HAU", "Dưa hấu", "RAU_MUONG"); st != http.StatusUnprocessableEntity || body["code"] != "masterdata.catalog_ref_invalid" {
		t.Fatalf("cha là cây cấp 2 (chỉ 2 cấp nhóm › cây): %d %v", st, body)
	}
	if st, body := crop("DUA_HAU", "Dưa hấu", "RAU_MAU"); st != http.StatusCreated {
		t.Fatalf("tạo cây: %d %v", st, body)
	}
	// Trùng mã, trùng tên (không phân biệt hoa thường) → chặn.
	if st, body := crop("DUA_HAU", "Dưa hấu 2", "RAU_MAU"); st != http.StatusConflict || body["code"] != "masterdata.catalog_item_exists" {
		t.Fatalf("trùng mã: %d %v", st, body)
	}
	if st, body := crop("DUA_HAU_2", "DƯA HẤU", "RAU_MAU"); st != http.StatusConflict || body["code"] != "masterdata.catalog_item_name_taken" {
		t.Fatalf("trùng tên: %d %v", st, body)
	}

	// Gọi lặp cùng Idempotency-Key → như lần đầu, không ghi sổ thêm.
	idem := "k-" + ids.New().String()
	mat := map[string]any{"code": "VOI_BOT", "name": "Vôi bột", "attrs": map[string]string{"unit": "kg", "group": "Cải tạo đất"}}
	st1, b1 := admin.do(f.admin, http.MethodPost, "/v1/masterdata/catalogs/material/items", idem, mat)
	st2, b2 := admin.do(f.admin, http.MethodPost, "/v1/masterdata/catalogs/material/items", idem, mat)
	if st1 != http.StatusCreated || st2 != http.StatusCreated || b1["code"] != b2["code"] {
		t.Fatalf("idempotent: %d %v / %d %v", st1, b1, st2, b2)
	}
	if st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/material/items", map[string]any{"code": "X1", "name": "Thiếu ĐVT", "attrs": map[string]string{}}); st != http.StatusUnprocessableEntity {
		t.Fatalf("vật tư thiếu ĐVT: %d %v", st, body)
	}

	// Sâu bệnh gắn cây trồng bị ảnh hưởng (mã phải có, chuẩn hóa bỏ trùng + sắp xếp); ngưỡng không bắt buộc.
	pest := func(code, crops string) (int, map[string]any) {
		return admin.adm(http.MethodPost, "/v1/masterdata/catalogs/pest/items", map[string]any{"code": code, "name": "Sâu thử " + code,
			"attrs": map[string]string{"kind": "sau_hai", "affected_crops": crops}})
	}
	if st, body := pest("sau_thu_1", "XOAI,KHONG_CO"); st != http.StatusUnprocessableEntity || body["code"] != "masterdata.catalog_ref_invalid" {
		t.Fatalf("cây bị ảnh hưởng không có: %d %v", st, body)
	}
	st, body = pest("sau_thu_1", " XOAI ,DUA_HAU,XOAI")
	if st != http.StatusCreated || body["attrs"].(map[string]any)["affected_crops"] != "DUA_HAU,XOAI" {
		t.Fatalf("sâu bệnh: %d %v", st, body)
	}
	// Điền ngưỡng sau (PUT) → lịch sử ghi cũ → mới.
	st, body = admin.do(f.admin, http.MethodPut, "/v1/masterdata/catalogs/pest/items/sau_thu_1", "", map[string]any{"name": "Sâu thử sau_thu_1",
		"attrs": map[string]string{"kind": "sau_hai", "affected_crops": "DUA_HAU,XOAI", "severity_thresholds": "nhẹ < 10%; nặng ≥ 30% (tạm)"}})
	if st != http.StatusOK {
		t.Fatalf("điền ngưỡng: %d %v", st, body)
	}
	// Vô hiệu hóa mục đã dùng: cho phép, có lý do; lịch sử giữ nguyên các bản trước.
	if st, body := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/pest/items/sau_thu_1/deactivate", map[string]any{"reason": "Gộp vào mục sâu cuốn lá"}); st != http.StatusCreated && st != http.StatusOK {
		t.Fatalf("vô hiệu hóa: %d %v", st, body)
	}
	st, body = admin.adm(http.MethodGet, "/v1/masterdata/catalogs/pest/items/sau_thu_1/history", nil)
	if items, _ := body["items"].([]any); st != http.StatusOK || len(items) != 3 {
		t.Fatalf("lịch sử (tạo, sửa, vô hiệu hóa): %d %v", st, body)
	}

	// Cột "biểu đang dùng": vắng khi chưa nối P3; có khi nối.
	st, body = admin.adm(http.MethodGet, "/v1/masterdata/catalogs/crop/items", nil)
	if c := codesOf(body); st != http.StatusOK || c["XOAI"] == nil || c["XOAI"]["forms_in_use"] != nil {
		t.Fatalf("chưa nối P3: %d %v", st, body)
	}
	f.mod.UseFormUsage(fakeFormUsage{"crop/XOAI": 2})
	st, body = admin.adm(http.MethodGet, "/v1/masterdata/catalogs/crop/items", nil)
	if c := codesOf(body); st != http.StatusOK || c["XOAI"]["forms_in_use"] != float64(2) || c["DUA_HAU"]["forms_in_use"] != float64(0) {
		t.Fatalf("biểu đang dùng: %d %v", st, body)
	}

	// Cách ly tenant: tenant khác không thấy mục vừa tạo.
	other := f.seeded()
	if st, _ := knOfficer(t, f, other).get("/v1/masterdata/catalogs/crop/DUA_HAU"); st != http.StatusNotFound {
		t.Fatalf("tenant khác thấy mục: %d", st)
	}
}
