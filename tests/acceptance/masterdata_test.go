package acceptance

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Dữ liệu chủ (pf/masterdata): đơn vị hành chính, danh mục, tham số.

func (w *world) units(parent string) []map[string]any {
	w.t.Helper()
	return w.want(w.root.Do(harness.App, http.MethodGet, "/v1/masterdata/admin-units?limit=1000&parent="+parent, "", nil), 200, "đơn vị con của "+parent).Items()
}

// Nguồn: mekong-green/tests/htx-operations.test.ts — "Dropdown hành chính lọc dần: xã theo tỉnh, ấp theo xã"
// và "Cây hành chính phủ đủ CẢ các tỉnh đã bị sáp nhập".
// Khác prototype: G1 dùng danh mục hành chính chính thức 2 cấp tỉnh → xã/phường (không có cấp ấp).
func TestPrototypeDonViHanhChinhLocDan(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "Dropdown hành chính lọc dần: xã theo tỉnh, ấp theo xã")
	nguon(t, "htx-operations.test.ts", "Cây hành chính phủ đủ CẢ các tỉnh đã bị sáp nhập")
	w := newWorld(t)
	provinces := w.units("")
	if len(provinces) != 6 {
		t.Fatalf("ĐBSCL sau sắp xếp 2025 có 6 tỉnh/thành, nhận %d", len(provinces))
	}
	byProvince := map[string]map[string]bool{}
	for _, p := range provinces {
		code := str(p, "code")
		communes := w.units(code)
		if len(communes) == 0 {
			t.Fatalf("tỉnh %s không có xã nào", str(p, "name"))
		}
		byProvince[code] = map[string]bool{}
		for _, c := range communes {
			if str(c, "parent_code") != code || str(c, "level") != "commune" {
				t.Fatalf("xã %v không thuộc tỉnh đã chọn %s", c, code)
			}
			byProvince[code][str(c, "code")] = true
		}
	}
	if !byProvince[provCT][communeNK] || !byProvince[provCT][communeBT] || !byProvince[provAG][communeLX] {
		t.Fatal("thiếu xã/phường dùng trong kiểm thử")
	}
	for code := range byProvince[provCT] {
		if byProvince[provAG][code] {
			t.Fatalf("xã %s lọt vào cả Cần Thơ và An Giang", code)
		}
	}
}

// Nguồn: mekong-green/tests/import.test.ts — "Quy đổi địa giới 2025 — 12 tỉnh cũ gộp về 6 tỉnh/thành".
// Khác prototype: bảng quy đổi là lịch sử hiệu lực của đơn vị hành chính (valid_to + successors);
// danh mục gồm 14 tỉnh cũ (kể cả Tây Ninh, Long An) gộp về 6.
func TestPrototypeQuyDoiDiaGioi2025(t *testing.T) {
	nguon(t, "import.test.ts", "Quy đổi địa giới 2025 — 12 tỉnh cũ gộp về 6 tỉnh/thành")
	w := newWorld(t)
	cases := map[string]string{"93": "92", "94": "92", "89": "91", "87": "82", "83": "86", "84": "86", "95": "96", "72": "80"}
	for old, cur := range cases {
		r := w.want(w.root.Do(harness.App, http.MethodGet, "/v1/masterdata/admin-units/"+old+"/current", "", nil), 200, "quy đổi "+old)
		codes, _ := r.Body["current_codes"].([]any)
		if len(codes) != 1 || codes[0] != cur {
			t.Fatalf("tỉnh cũ %s phải quy về %s: %s", old, cur, trim(r.Raw))
		}
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-HTX-13: tạo mùa vụ có kiểm tra và không trùng mã".
// Khác prototype: mã được lưu đúng như nhập (không tự viết hoa); tháng bắt đầu/kết thúc đi cùng nhau.
func TestPrototypeMuaVuKhongTrungMa(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-HTX-13: tạo mùa vụ có kiểm tra và không trùng mã")
	w := newWorld(t)
	post := func(body map[string]any) harness.Resp {
		return w.root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/catalogs/season/items", "", body)
	}
	attrs := map[string]string{"start_month": "11", "end_month": "3"}
	r := w.want(post(map[string]any{"code": "DX-2027-2028", "name": "Đông Xuân 2027-2028", "attrs": attrs}), 201, "tạo mùa vụ")
	if str(r.Body, "code") != "DX-2027-2028" {
		t.Fatalf("mã mùa vụ: %s", trim(r.Raw))
	}
	wantProblem(t, post(map[string]any{"code": "DX-2027-2028", "name": "Trùng", "attrs": attrs}), 409, "masterdata.catalog_item_exists", "trùng mã mùa vụ")
	wantFieldError(t, post(map[string]any{"code": "X", "name": "Sai tháng", "attrs": map[string]string{"start_month": "13", "end_month": "3"}}), "attrs.start_month", "tháng ngoài 1–12")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-CGH-05/06/10: sửa được chủng loại máy, mã không trùng, danh mục trả định mức hiện hành".
// Khác prototype: mã là khóa bất biến (không đổi mã qua sửa); định mức không là cột của danh mục mà
// là tham số có phiên bản cgh.norm_ha_per_machine:<loại>:<khâu> tra theo ngày.
func TestPrototypeSuaChungLoaiMay(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-CGH-05/06/10: sửa được chủng loại máy, mã không trùng, danh mục trả định mức hiện hành")
	w := newWorld(t)
	item := "/v1/masterdata/catalogs/machine_type/items/MLD"
	wantFieldError(t, w.root.Do(harness.Admin, http.MethodPut, item, "", map[string]any{"code": "MGS", "name": "Đổi mã"}), "code", "đổi mã sang mã khác")
	r := w.want(w.root.Do(harness.Admin, http.MethodPut, item, "", map[string]any{"name": "Máy làm đất (tên mới UAT)", "attrs": map[string]string{"farm_stage": "lam_dat"}}), 200, "sửa tên chủng loại")
	if str(r.Body, "name") != "Máy làm đất (tên mới UAT)" {
		t.Fatalf("sửa tên: %s", trim(r.Raw))
	}
	h := w.want(w.root.Do(harness.Admin, http.MethodGet, item+"/history", "", nil), 200, "lịch sử danh mục").Items()
	if len(h) < 2 || str(h[len(h)-1], "action") != "updated" {
		t.Fatalf("lịch sử phải ghi lần sửa: %v", h)
	}
	norm := w.want(w.root.Do(harness.App, http.MethodGet, "/v1/masterdata/params/value?key=cgh.norm_ha_per_machine:MLD:lam_dat", "", nil), 200, "định mức hiện hành")
	if str(norm.Body, "value") != "120" || str(norm.Body, "unit") != "ha/máy/vụ" || str(norm.Body, "source_doc") == "" {
		t.Fatalf("định mức hiện hành phải là ha/máy/vụ kèm văn bản: %s", trim(norm.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Giống đang được vụ tham chiếu thì chỉ ẩn, không xoá; khôi phục lại được".
// Khác prototype: không có xóa hẳn (kể cả giống chưa dùng) — chỉ ngừng dùng kèm lý do; báo cáo cũ
// giữ nguyên, báo cáo mới không nhận giống đã ngừng.
func TestPrototypeGiongNgungDungChiAn(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Giống đang được vụ tham chiếu thì chỉ ẩn, không xoá; khôi phục lại được")
	w := newWorld(t)
	old := w.want(w.sowing(w.officer, communeNK, w.coopNK, "OM18", "4"), 201, "báo cáo dùng giống OM18")
	item := "/v1/masterdata/catalogs/rice_variety/items/OM18"
	wantFieldError(t, w.root.Do(harness.Admin, http.MethodPost, item+"/deactivate", "", map[string]any{"reason": "ngắn"}), "reason", "lý do ngừng dùng quá ngắn")
	w.want(w.root.Do(harness.Admin, http.MethodPost, item+"/deactivate", "", map[string]any{"reason": "Giống thoái hóa, ngừng khuyến cáo"}), 200, "ngừng dùng giống")
	app := refsOf(w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/masterdata/catalogs/rice_variety", "", nil), 200, "danh mục giống").Items(), "code")
	adm := refsOf(w.want(w.root.Do(harness.Admin, http.MethodGet, "/v1/masterdata/catalogs/rice_variety/items", "", nil), 200, "danh mục giống (quản trị)").Items(), "code")
	if app["OM18"] || !adm["OM18"] {
		t.Fatal("giống ngừng dùng phải ẩn khỏi danh mục vận hành nhưng còn ở màn quản trị")
	}
	wantProblem(t, w.sowing(w.officerBT, communeBT, w.coopBT, "OM18", "2"), 422, "extension.catalog_code_invalid", "báo cáo mới dùng giống đã ngừng")
	w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/extension/field-reports/"+str(old.Body, "id"), "", nil), 200, "báo cáo cũ vẫn đọc được")
	w.want(w.root.Do(harness.Admin, http.MethodPost, item+"/reactivate", "", map[string]any{"reason": "Khôi phục theo đề nghị của tỉnh"}), 200, "khôi phục giống")
	w.want(w.sowing(w.officer, communeNK, w.coopNK2, "OM18", "2"), 201, "báo cáo dùng giống sau khôi phục")
}
