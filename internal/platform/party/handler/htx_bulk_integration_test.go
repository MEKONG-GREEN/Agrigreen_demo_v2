package handler_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// pfText gom mọi cột văn bản của sổ tổ chức và hồ sơ HTX (giả của pf/cooperative, W3.3) trong tenant để kiểm không có
// PII ở pf.
func (f *fixture) pfText(t *testing.T) string {
	t.Helper()
	var out string
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT
		   coalesce((SELECT string_agg(e.name || ' ' || coalesce(e.details::text, '') || ' ' || coalesce(e.reason, ''), ' ') FROM pf_party.org_event e), '')`).Scan(&out)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.profiles.mu.Lock()
	defer f.profiles.mu.Unlock()
	for _, p := range f.profiles.profiles[f.tenant] {
		out += " " + p.Address + " " + p.TaxCode
	}
	return out
}

// HTX US-HTX-02: nhập HTX hàng loạt từ tệp mẫu .xlsx — tạo mọi dòng hợp lệ trong một lần,
// loại theo dòng với "Thiếu cột bắt buộc: …", tệp sai định dạng → "Sai định dạng file";
// người đại diện + SĐT chỉ vào kho định danh; gọi lặp cùng khóa không tạo trùng.
func TestCooperativeImport_HTX_US_HTX_02(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)

	// Tệp mẫu tải về có đủ cột bắt buộc.
	st, hdr, data := prov.download("/v1/cooperatives/import-template.xlsx")
	if st != http.StatusOK || hdr.Get("Content-Type") != tabular.ContentTypeXLSX || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("tệp mẫu: %d %v", st, hdr)
	}
	tb, err := tabular.ReadBytes(data, tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"Tên HTX", "Địa chỉ", "Người đại diện", "SĐT", "Mã xã"} {
		if !slices.Contains(tb.Header, h) {
			t.Fatalf("tệp mẫu thiếu cột %q: %v", h, tb.Header)
		}
	}

	header := []string{"Tên HTX", "Địa chỉ", "Người đại diện", "SĐT", "Mã xã", "Mã số thuế", "Diện tích (ha)", "Ngày thành lập", "Mã tổ chức cấp trên"}
	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	teamCode := ""
	for _, it := range items(national.appDo(http.MethodGet, "/v1/orgs?kind=extension_team", nil)) {
		if it["ref"] == tr.t30001.String() {
			teamCode = it["code"].(string)
		}
	}
	body := xlsxBytes(t, header, [][]string{
		{"HTX Phú Hòa", "Ấp Phú Hòa, xã A", "Lê Thị Đại Diện", "0918000001", "30001", "0312345678", "80", "2021-06-01", teamCode},
		{"HTX Thiếu Đại Diện", "Ấp 2", "", "0918000002", "30001", "", "", "", ""},
		{"HTX Lúa Xanh", "Ấp 3", "Trần Văn Trùng", "0918000003", "30001", "", "", "", ""}, // trùng tên HTX đã có
		{"HTX Ngoài Tỉnh", "Ấp 4", "Võ Văn Ngoài", "0918000004", "31001", "", "", "", ""}, // xã thuộc tỉnh 92
		{"HTX Bình An", "Ấp Bình An", "Phạm Văn Bình", "0918000005", "30002", "", "12,5", "", ""},
	})
	k := key()
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", k, tabular.ContentTypeXLSX, body)
	if r.status != http.StatusOK || r.body["created"] != float64(2) || r.body["rejected"] != float64(3) {
		t.Fatalf("nhập HTX: %d %v", r.status, r.body)
	}
	rows := rowsOf(r)
	want := []struct{ status, reason string }{
		{"created", ""}, {"rejected", "Thiếu cột bắt buộc: Người đại diện"},
		{"rejected", "Tên HTX này đã tồn tại, vui lòng chọn tên khác."}, {"rejected", ""}, {"created", ""},
	}
	for i, w := range want {
		reason, _ := rows[i]["reason"].(string)
		if rows[i]["status"] != w.status || (w.reason != "" && reason != w.reason) || rows[i]["row"] != float64(i+2) {
			t.Fatalf("dòng %d: %v", i+2, rows[i])
		}
	}
	if reason, _ := rows[3]["reason"].(string); reason == "" {
		t.Fatalf("dòng ngoài phạm vi phải có lý do: %v", rows[3])
	}
	newRef := rows[0]["org_ref"].(string)
	coop := prov.appDo(http.MethodGet, "/v1/cooperatives/"+newRef, nil)
	if coop.status != http.StatusOK || coop.body["address"] != "Ấp Phú Hòa, xã A" || coop.body["tax_code"] != "0312345678" ||
		coop.body["parent_ref"] != tr.t30001.String() || coop.body["representative_ref"] == nil || !strings.HasPrefix(str(coop.body, "code"), "HTX") {
		t.Fatalf("HTX nhập: %v", coop.body)
	}
	// Người đại diện + SĐT chỉ ở kho định danh (R-04).
	if txt := f.pfText(t); strings.Contains(txt, "0918000001") || strings.Contains(txt, "Lê Thị Đại Diện") {
		t.Fatal("SĐT/họ tên người đại diện không được nằm ở pf_party")
	}
	if !slices.Contains(f.vault.usedPurposes(), "registry.admin") {
		t.Fatal("người đại diện phải ghi vào kho định danh với mục đích registry.admin")
	}
	// Gọi lặp cùng khóa: phát lại đúng phản hồi, không tạo thêm HTX.
	r2 := prov.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", k, tabular.ContentTypeXLSX, body)
	if r2.status != http.StatusOK || r2.hdr.Get("Idempotent-Replayed") != "true" || rowsOf(r2)[0]["org_ref"] != newRef {
		t.Fatalf("gọi lặp: %d %v", r2.status, r2.body)
	}
	// Khóa khác, cùng tệp: dòng đã tạo nay trùng tên → bị loại, không tạo HTX thứ hai.
	r3 := prov.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", key(), tabular.ContentTypeXLSX, body)
	if r3.body["created"] != float64(0) {
		t.Fatalf("nhập lại tệp cũ không được tạo trùng: %v", r3.body)
	}

	// Thiếu hẳn cột "Người đại diện": từng dòng bị loại với tên cột.
	noRep := xlsxBytes(t, []string{"Tên HTX", "Địa chỉ", "SĐT", "Mã xã"}, [][]string{{"HTX Mới 1", "Ấp 1", "0918000010", "30001"}, {"HTX Mới 2", "Ấp 2", "0918000011", "30001"}})
	r4 := prov.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", key(), tabular.ContentTypeXLSX, noRep)
	if r4.status != http.StatusOK || r4.body["rejected"] != float64(2) || rowsOf(r4)[1]["reason"] != "Thiếu cột bắt buộc: Người đại diện" {
		t.Fatalf("thiếu cột: %d %v", r4.status, r4.body)
	}
	// Tệp không phải bảng tính → 422 "Sai định dạng file".
	r5 := prov.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", key(), "application/octet-stream", []byte{0x00, 0x01, 0x02, 0xff})
	if r5.status != http.StatusUnprocessableEntity || r5.body["code"] != "party.import_invalid" || !strings.HasPrefix(str(r5.body, "detail"), "Sai định dạng file") {
		t.Fatalf("sai định dạng: %d %v", r5.status, r5.body)
	}
	// Quản lý HTX không nhập hồ sơ HTX khác (Admin API, vai quản trị dữ liệu chủ).
	mgr := f.as(f.tenant, tr.c1, port.RoleCoopManager)
	if r := mgr.call(f.admin.URL, http.MethodPost, "/v1/cooperatives/import", key(), tabular.ContentTypeXLSX, body); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX nhập HTX: %d", r.status)
	}
	// Cách ly tenant: tenant khác không thấy HTX vừa nhập.
	other := f.as(ids.New(), tr.p89, port.RoleKNProvinceAdmin)
	if r := other.appDo(http.MethodGet, "/v1/cooperatives/"+newRef, nil); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác đọc HTX: %d", r.status)
	}
}

// HTX US-HTX-03: sao chép hồ sơ HTX — giữ trường chung (địa bàn, cấu trúc), sửa riêng Tên và
// Người đại diện, KHÔNG chép người đại diện; tên trùng → "Tên HTX này đã tồn tại, vui lòng chọn tên khác."
func TestCooperativeCopy_HTX_US_HTX_03(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	src := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Nguồn", "admin_unit_code": "30001", "parent_ref": tr.t30001.String(),
		"representative": map[string]any{"full_name": "Đỗ Văn Nguồn", "phone": "0919000001"},
		"cooperative":    map[string]any{"tax_code": "0319999999", "address": "Ấp Nguồn", "area_ha": "55.5", "established_on": "2018-01-02", "location": map[string]any{"lat": "10.1", "lon": "105.2"}}})
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	path := "/v1/cooperatives/" + src.String() + "/copy"

	if r := prov.adminDo(http.MethodPost, path, map[string]any{"name": "htx nguồn "}); r.status != http.StatusConflict ||
		r.body["code"] != "party.cooperative_name_taken" || r.body["title"] != "Tên HTX này đã tồn tại, vui lòng chọn tên khác." {
		t.Fatalf("tên trùng: %d %v", r.status, r.body)
	}
	k := key()
	body := []byte(`{"name":"HTX Nguồn 2","representative":{"full_name":"Mai Thị Mới","phone":"0919000002"}}`)
	r := prov.call(f.admin.URL, http.MethodPost, path, k, "application/json", body)
	if r.status != http.StatusCreated || r.body["source_ref"] != src.String() {
		t.Fatalf("sao chép: %d %v", r.status, r.body)
	}
	cp := str(r.body, "org", "ref")
	srcCoop := prov.appDo(http.MethodGet, "/v1/cooperatives/"+src.String(), nil).body
	got := prov.appDo(http.MethodGet, "/v1/cooperatives/"+cp, nil).body
	if got["name"] != "HTX Nguồn 2" || got["admin_unit_code"] != "30001" || got["parent_ref"] != tr.t30001.String() ||
		got["address"] != "Ấp Nguồn" || got["area_ha"] != srcCoop["area_ha"] || got["established_on"] != "2018-01-02" {
		t.Fatalf("bản sao phải giữ địa bàn, cấu trúc: %v", got)
	}
	if got["tax_code"] != nil || got["location_provisional"] != true || got["code"] == srcCoop["code"] {
		t.Fatalf("bản sao không chép MST, toạ độ, Mã HTX: %v", got)
	}
	if got["representative_ref"] == nil || got["representative_ref"] == srcCoop["representative_ref"] {
		t.Fatalf("người đại diện không được sao chép từ hồ sơ nguồn: %v / %v", got["representative_ref"], srcCoop["representative_ref"])
	}
	// Không gửi người đại diện → bản sao chưa có người đại diện (không kế thừa từ nguồn).
	r2 := prov.adminDo(http.MethodPost, path, map[string]any{"name": "HTX Nguồn 3"})
	if r2.status != http.StatusCreated || r2.body["org"].(map[string]any)["representative_ref"] != nil {
		t.Fatalf("bản sao không người đại diện: %d %v", r2.status, r2.body)
	}
	// Gọi lặp cùng khóa → phát lại.
	if r := prov.call(f.admin.URL, http.MethodPost, path, k, "application/json", body); r.hdr.Get("Idempotent-Replayed") != "true" || str(r.body, "org", "ref") != cp {
		t.Fatalf("gọi lặp: %d %v", r.status, r.body)
	}
	// Phạm vi: TTKN tỉnh khác, tenant khác → 404; quản lý HTX → 403.
	if r := f.as(f.tenant, tr.p92, port.RoleKNProvinceAdmin).adminDo(http.MethodPost, path, map[string]any{"name": "HTX X"}); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác sao chép: %d", r.status)
	}
	if r := f.as(ids.New(), uuid0(), port.RoleRegistryAdmin).adminDo(http.MethodPost, path, map[string]any{"name": "HTX Y"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác sao chép: %d", r.status)
	}
	if r := f.as(f.tenant, src, port.RoleCoopManager).adminDo(http.MethodPost, path, map[string]any{"name": "HTX Z"}); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX sao chép: %d", r.status)
	}
	if txt := f.pfText(t); strings.Contains(txt, "0919000002") || strings.Contains(txt, "Mai Thị Mới") {
		t.Fatal("người đại diện của bản sao không được nằm ở pf_party")
	}
}

func uuid0() uuid.UUID { return uuid.Nil }
