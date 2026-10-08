package handler_test

// Nông hộ và xã viên HTX (W3.3 — chuyển từ pf/party/handler; đường dẫn, JSON, mã lỗi giữ nguyên).

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func xlsxBytes(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Sheet1", header, rows); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// householdEvents đọc sự kiện nông hộ của tenant trong shared.outbox (vai ag_outbox_publisher).
func (f *fixture) householdEvents(t *testing.T, typ, ref string) []map[string]string {
	t.Helper()
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	rows, err := pub.Query(context.Background(), `SELECT coalesce(payload->>'source', ''), coalesce(payload->>'change', ''),
	    coalesce(payload->>'cooperative_ref', ''), coalesce(payload->>'prev_cooperative_ref', ''), coalesce(payload->>'commune_code', ''),
	    payload::text
	  FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 AND payload->>'household_ref' = $3 ORDER BY occurred_at`, f.tenant, typ, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var src, change, coop, prev, commune, raw string
		if err := rows.Scan(&src, &change, &coop, &prev, &commune, &raw); err != nil {
			t.Fatal(err)
		}
		out = append(out, map[string]string{"source": src, "change": change, "cooperative_ref": coop, "prev": prev, "commune": commune, "raw": raw})
	}
	return out
}

// KN US-HTX-05: nông hộ đăng ký từ App HTX phát pf.party.household.registered (mã
// định danh chung, không phát lại khi trùng); vào/chuyển/rời HTX phát
// pf.party.household.membership_changed cùng giao dịch. Không có PII trong sự kiện.
func TestHouseholdEventsKNUSHTX05(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1b := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Lúa Vàng", "admin_unit_code": "30001", "parent_ref": tr.t30001.String()})
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)

	r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907000001"))
	if r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %d %v", r.status, r.body)
	}
	ref := str(r.body, "household", "household_ref")
	// Trùng chỉ mục mù → không phát lần hai.
	if r2 := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907000001")); r2.status != http.StatusOK {
		t.Fatalf("trùng: %d", r2.status)
	}
	reg := f.householdEvents(t, coopport.EventHouseholdRegistered, ref)
	if len(reg) != 1 || reg[0]["source"] != coopport.HouseholdSourceHTXApp || reg[0]["cooperative_ref"] != tr.c1.String() || reg[0]["commune"] != "30001" {
		t.Fatalf("registered: %v", reg)
	}
	for _, pii := range []string{"0907000001", "Nguyễn Văn An", "Ấp 1"} {
		if strings.Contains(reg[0]["raw"], pii) {
			t.Fatalf("sự kiện chứa dữ liệu cá nhân: %s", reg[0]["raw"])
		}
	}

	path := "/v1/households/" + ref + "/membership"
	// Quản lý HTX không chuyển hộ sang HTX khác.
	if r := mgr.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX chuyển hộ: %d %v", r.status, r.body)
	}
	officer := f.as(f.tenant, tr.t30001, partyport.RoleKNOfficer)
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu lý do: %d", r.status)
	}
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusOK || r.body["change"] != "transferred" || str(r.body, "household", "cooperative_ref") != c1b.String() {
		t.Fatalf("chuyển HTX: %d %v", r.status, r.body)
	}
	if r := officer.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": c1b.String(), "reason": "Hộ xin chuyển sang HTX Lúa Vàng"}); r.status != http.StatusConflict {
		t.Fatalf("đổi lại cùng HTX: %d", r.status)
	}
	// Quản lý HTX Lúa Vàng cho hộ rời HTX của mình.
	mgrB := f.as(f.tenant, c1b, partyport.RoleCoopManager)
	if r := mgrB.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": nil, "reason": "Hộ không còn canh tác lúa"}); r.status != http.StatusOK || r.body["change"] != "left" {
		t.Fatalf("rời HTX: %d %v", r.status, r.body)
	}
	// Cán bộ xã khác không thấy hộ.
	if r := f.as(f.tenant, tr.t30002, partyport.RoleKNOfficer).appDo(http.MethodPost, path, map[string]any{"cooperative_ref": tr.c1.String(), "reason": "Vào lại HTX Lúa Xanh"}); r.status != http.StatusNotFound {
		t.Fatalf("ngoài xã: %d", r.status)
	}
	// Quản lý HTX Lúa Xanh cho hộ (chưa thuộc HTX nào) vào lại HTX của mình.
	if r := mgr.appDo(http.MethodPost, path, map[string]any{"cooperative_ref": tr.c1.String(), "reason": "Hộ đăng ký vào lại HTX"}); r.status != http.StatusOK || r.body["change"] != "joined" {
		t.Fatalf("vào HTX: %d %v", r.status, r.body)
	}
	ch := f.householdEvents(t, coopport.EventHouseholdMembershipChanged, ref)
	want := []struct{ change, coop, prev string }{
		{"transferred", c1b.String(), tr.c1.String()}, {"left", "", c1b.String()}, {"joined", tr.c1.String(), ""},
	}
	if len(ch) != len(want) {
		t.Fatalf("membership_changed: %v", ch)
	}
	for i, w := range want {
		if ch[i]["change"] != w.change || ch[i]["cooperative_ref"] != w.coop || ch[i]["prev"] != w.prev {
			t.Fatalf("sự kiện %d: %v, muốn %+v", i, ch[i], w)
		}
		if strings.Contains(ch[i]["raw"], "canh tác") {
			t.Fatal("lý do không đi vào sự kiện")
		}
	}
	// Tenant khác không thấy hộ.
	if r := f.as(uuid.New(), tr.t30001, partyport.RoleKNOfficer).appDo(http.MethodPost, path, map[string]any{"cooperative_ref": nil, "reason": "Thử từ tenant khác"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác: %d", r.status)
	}
}

// KN US-HTX-02: cán bộ xã chỉ tạo nông hộ trong xã phụ trách.
func TestCommuneOfficerCannotCreateOutsideCommune(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	officer := f.as(f.tenant, tr.t30001, partyport.RoleKNOfficer)

	if r := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c2, "0902000001")); r.status != http.StatusForbidden || r.body["code"] != "party.out_of_scope" {
		t.Fatalf("HTX ngoài xã phải 403 party.out_of_scope: %d %v", r.status, r.body)
	}
	other := farmer(tr.c1, "0902000002")
	other["commune_code"] = "30002"
	if r := officer.appDo(http.MethodPost, "/v1/households", other); r.status != http.StatusForbidden {
		t.Fatalf("xã của hộ ngoài phạm vi phải 403: %d %v", r.status, r.body)
	}
	if r := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000003")); r.status != http.StatusCreated {
		t.Fatalf("tạo trong xã: %d %v", r.status, r.body)
	}
	// Quản lý HTX chỉ thao tác nông hộ của HTX mình (HTX US-HH-01 AC-3).
	mgrOther := f.as(f.tenant, tr.c2, partyport.RoleCoopManager)
	if r := mgrOther.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000004")); r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX khác phải 403: %d %v", r.status, r.body)
	}
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0902000005")); r.status != http.StatusCreated {
		t.Fatalf("quản lý HTX tạo hộ của HTX mình: %d %v", r.status, r.body)
	}
}

// KN US-HTX-05, HTX US-HH-01: tạo chủ hộ qua kho định danh, trùng → dùng lại mã cũ, báo "trùng".
func TestHouseholdDedupeViaVault(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	officer := f.as(f.tenant, tr.t30001, partyport.RoleKNOfficer)

	r1 := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0905000001"))
	if r1.status != http.StatusCreated || r1.body["duplicate"] != false {
		t.Fatalf("tạo hộ: %d %v", r1.status, r1.body)
	}
	ref := str(r1.body, "household", "household_ref")
	r2 := officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0905000001"))
	if r2.status != http.StatusOK || r2.body["duplicate"] != true || r2.body["duplicate_label"] != "trùng" || str(r2.body, "household", "household_ref") != ref {
		t.Fatalf("trùng SĐT phải dùng lại mã cũ: %d %v", r2.status, r2.body)
	}
	lst := officer.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil)
	it := items(lst)
	if len(it) != 1 || it[0]["household_ref"] != ref || it[0]["label"] != "Nguyễn A." || it[0]["masked_phone"] != "xxx 001" {
		t.Fatalf("danh sách nông hộ kèm nhãn che: %v", lst.body)
	}
	if _, ok := it[0]["phone"]; ok {
		t.Fatal("danh sách không được có SĐT đầy đủ")
	}
	p := f.vault.usedPurposes()
	if !slices.Contains(p, "registry.admin") || !slices.Contains(p, "extension.field_support") {
		t.Fatalf("mục đích gọi kho định danh: %v", p)
	}
	// Cục KTHT không xem nông hộ.
	if r := f.as(f.tenant, tr.national, partyport.RoleCGHViewer).appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil); r.status != http.StatusForbidden {
		t.Fatalf("cgh_viewer xem nông hộ phải 403: %d", r.status)
	}
}

// KN US-HTX-03, HTX US-HH-02: nhập CSV, kết quả theo dòng.
func TestImportHouseholdsCSV(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1Code, c2Code := f.coopCodes(tr)
	csvBody := "ho_ten,so_dien_thoai,cccd,dia_chi,ma_htx,dien_tich_ha,ngay_vao_htx\n" +
		"Trần Thị Bình,0906000001,,Ấp 2," + c1Code + ",2.5,2023-05-01\n" +
		"Trần Thị Bình,0906000001,,Ấp 2," + c1Code + ",2.5,2023-05-01\n" +
		"Lê Văn Cường,,,Ấp 3," + c1Code + ",1,\n" +
		"Phạm Văn Dũng,0906000004,,Ấp 4,HTX999999,1,\n" +
		"Võ Thị Em,0906000005,,Ấp 5," + c2Code + ",1,\n" +
		"Đỗ Văn Phúc,0906000006,,Ấp 6," + c1Code + ",abc,\n"
	prov := f.as(f.tenant, tr.p89, partyport.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), "text/csv", []byte(csvBody))
	if r.status != http.StatusOK || r.body["accepted"] != float64(1) || r.body["duplicate"] != float64(1) || r.body["rejected"] != float64(4) {
		t.Fatalf("kết quả nhập: %d %v", r.status, r.body)
	}
	rows, _ := r.body["rows"].([]any)
	want := []string{"accepted", "duplicate", "rejected", "rejected", "rejected", "rejected"}
	for i, raw := range rows {
		row := raw.(map[string]any)
		if row["status"] != want[i] || row["row"] != float64(i+2) {
			t.Fatalf("dòng %d: %v", i+2, row)
		}
		if reason, _ := row["reason"].(string); strings.Contains(reason, "0906") || strings.Contains(reason, "Trần") {
			t.Fatalf("lý do không được chứa dữ liệu cá nhân: %q", reason)
		}
	}
	// Tệp thiếu cột bắt buộc → từ chối cả tệp.
	bad := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), "text/csv", []byte("ho_ten,dia_chi\nA,B\n"))
	if bad.status != http.StatusUnprocessableEntity || bad.body["code"] != "party.import_invalid" {
		t.Fatalf("tệp sai mẫu: %d %v", bad.status, bad.body)
	}
}

// HTX US-HH-02 AC-2..4: sao chép hàng loạt nông hộ, sửa riêng họ tên/SĐT từng hồ sơ; hồ sơ
// lỗi bị loại với "Thiếu họ tên" / "SĐT đã tồn tại", không chặn hồ sơ khác; định danh mới chỉ ở kho.
func TestHouseholdBulkCopy_HTX_US_HH_02(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	h1 := str(mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0906000001")).body, "household", "household_ref")
	h2 := str(mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0906000002")).body, "household", "household_ref")
	officer := f.as(f.tenant, tr.p92, partyport.RoleKNProvinceAdmin)
	hFar := str(officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c2, "0906000009")).body, "household", "household_ref")
	// HTX khác cùng xã 30001: trong phạm vi xã nhưng không phải HTX của quản lý.
	sameCommune := f.root().createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cùng Xã", "admin_unit_code": "30001"})
	hOther := str(f.root().appDo(http.MethodPost, "/v1/households", farmer(sameCommune, "0906000008")).body, "household", "household_ref")

	k := key()
	payload := map[string]any{"items": []map[string]any{
		{"source_ref": h1, "full_name": "Nguyễn Văn Bản Sao", "phone": "0906000101"},
		{"source_ref": h1, "full_name": " ", "phone": "0906000102"},
		{"source_ref": h2, "full_name": "Trần Thị Trùng", "phone": "0906000001"},
		{"source_ref": hOther, "full_name": "Hộ HTX Khác", "phone": "0906000103"},
		{"source_ref": h2, "full_name": "Lê Văn Hai", "phone": "0906000104"},
		{"source_ref": hFar, "full_name": "Hộ Ngoài Xã", "phone": "0906000107"},
	}}
	b := mustJSON(t, payload)
	r := mgr.call(f.app.URL, http.MethodPost, "/v1/households/copy", k, "application/json", b)
	if r.status != http.StatusOK || r.body["created"] != float64(2) || r.body["rejected"] != float64(4) {
		t.Fatalf("sao chép hàng loạt: %d %v", r.status, r.body)
	}
	rows := rowsOf(r)
	wantReason := []string{"", "Thiếu họ tên", "SĐT đã tồn tại", "Bạn không có quyền truy cập nông hộ này.", "", "Không tìm thấy nông hộ nguồn"}
	for i, w := range wantReason {
		reason, _ := rows[i]["reason"].(string)
		if reason != w || rows[i]["index"] != float64(i+1) {
			t.Fatalf("hồ sơ %d: %v", i+1, rows[i])
		}
	}
	list := items(mgr.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))
	if len(list) != 4 {
		t.Fatalf("HTX có 2 hộ gốc + 2 bản sao: %d", len(list))
	}
	newRef := rows[0]["household_ref"].(string)
	for _, it := range list {
		if it["household_ref"] == newRef && (it["cultivated_area_ha"] != "1.25" || it["cooperative_ref"] != tr.c1.String() || it["commune_code"] != "30001") {
			t.Fatalf("bản sao giữ HTX, xã, diện tích của hộ nguồn: %v", it)
		}
	}
	// Gọi lặp cùng khóa → phát lại đúng kết quả lần đầu, không tạo thêm.
	r2 := mgr.call(f.app.URL, http.MethodPost, "/v1/households/copy", k, "application/json", b)
	if r2.hdr.Get("Idempotent-Replayed") != "true" || rowsOf(r2)[0]["household_ref"] != newRef {
		t.Fatalf("gọi lặp: %v", r2.body)
	}
	if n := len(items(mgr.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))); n != 4 {
		t.Fatalf("gọi lặp không tạo thêm: %d", n)
	}
	// Quản lý HTX không sao chép sang HTX khác.
	r3 := mgr.appDo(http.MethodPost, "/v1/households/copy", map[string]any{"target_cooperative_ref": tr.c2.String(),
		"items": []map[string]any{{"source_ref": h1, "full_name": "Hộ Sang HTX Khác", "phone": "0906000105"}}})
	if r3.status != http.StatusOK || rowsOf(r3)[0]["status"] != "rejected" {
		t.Fatalf("sao chép sang HTX khác: %d %v", r3.status, r3.body)
	}
	// Quá 50 hồ sơ → 422; tenant khác không thấy hộ nguồn.
	many := make([]map[string]any, 51)
	for i := range many {
		many[i] = map[string]any{"source_ref": h1, "full_name": "A", "phone": "0906"}
	}
	if r := mgr.appDo(http.MethodPost, "/v1/households/copy", map[string]any{"items": many}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("quá 50 hồ sơ: %d", r.status)
	}
	stranger := f.as(ids.New(), tr.c1, partyport.RoleCoopManager)
	if r := stranger.appDo(http.MethodPost, "/v1/households/copy", map[string]any{"items": []map[string]any{{"source_ref": h1, "full_name": "B", "phone": "0906000106"}}}); r.status == http.StatusOK && rowsOf(r)[0]["status"] != "rejected" {
		t.Fatalf("tenant khác sao chép hộ: %v", r.body)
	}
}

// HTX US-HH-03: gọi nhanh nông hộ — SĐT đọc từ kho định danh lúc bấm (mục đích
// extension.contact), không lưu đệm; hộ của HTX khác → "Bạn không có quyền truy cập nông hộ này."
func TestHouseholdQuickCall_HTX_US_HH_03(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	h := str(mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907100001")).body, "household", "household_ref")

	r := mgr.appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil)
	if r.status != http.StatusOK || r.body["phone"] != "0907100001" || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("SĐT gọi nhanh: %d %v %v", r.status, r.body, r.hdr)
	}
	if !slices.Contains(f.vault.usedPurposes(), "extension.contact") {
		t.Fatal("đọc SĐT phải kèm mục đích extension.contact")
	}
	other := f.as(f.tenant, tr.c2, partyport.RoleCoopManager)
	if r := other.appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil); r.status != http.StatusNotFound && r.status != http.StatusForbidden {
		t.Fatalf("quản lý HTX khác: %d", r.status)
	}
	// Cùng xã, HTX khác trong xã → 403 kèm thông điệp AC-3 của US-HH-01.
	sameCommune := f.root().createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cùng Xã", "admin_unit_code": "30001"})
	if r := f.as(f.tenant, sameCommune, partyport.RoleCoopManager).appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil); r.status != http.StatusForbidden ||
		r.body["detail"] != "Bạn không có quyền truy cập nông hộ này." {
		t.Fatalf("HTX cùng xã: %d %v", r.status, r.body)
	}
	if r := mgr.appDo(http.MethodGet, "/v1/households/"+ids.New().String()+"/contact", nil); r.status != http.StatusNotFound {
		t.Fatalf("hộ không có: %d", r.status)
	}
	if r := f.as(ids.New(), tr.c1, partyport.RoleCoopManager).appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác: %d", r.status)
	}
	// Chủ thể đã xóa định danh / không có trong kho → 404 party.household_contact_unavailable.
	f.vault.mu.Lock()
	for ref := range f.vault.subjects {
		if ref.String() == h {
			delete(f.vault.subjects, ref)
		}
	}
	f.vault.mu.Unlock()
	if r := mgr.appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil); r.status != http.StatusNotFound || r.body["code"] != "party.household_contact_unavailable" {
		t.Fatalf("không còn SĐT: %d %v", r.status, r.body)
	}
	if r := f.as(f.tenant, tr.national, partyport.RoleCGHViewer).appDo(http.MethodGet, "/v1/households/"+h+"/contact", nil); r.status != http.StatusForbidden {
		t.Fatalf("cgh_viewer: %d", r.status)
	}
}

// HTX US-HH-01 AC-2: tìm nông hộ đúng SĐT qua chỉ mục mù của kho định danh (không lộ danh sách);
// hộ HTX khác hoặc SĐT lạ → rỗng; giá trị không phải SĐT → 422.
func TestHouseholdSearchByPhone_HTX_US_HH_01(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	h := str(mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0908200001")).body, "household", "household_ref")
	_ = mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0908200002"))
	officer := f.as(f.tenant, tr.p92, partyport.RoleKNProvinceAdmin)
	_ = officer.appDo(http.MethodPost, "/v1/households", farmer(tr.c2, "0908200003"))
	base := "/v1/cooperatives/" + tr.c1.String() + "/households?phone="

	if l := items(mgr.appDo(http.MethodGet, base+"0908200001", nil)); len(l) != 1 || l[0]["household_ref"] != h || l[0]["masked_phone"] != "xxx 001" {
		t.Fatalf("tìm theo SĐT: %v", l)
	}
	if l := items(mgr.appDo(http.MethodGet, base+"0908200003", nil)); len(l) != 0 {
		t.Fatalf("hộ HTX khác không được trả: %v", l)
	}
	if l := items(mgr.appDo(http.MethodGet, base+"0908299999", nil)); len(l) != 0 {
		t.Fatalf("SĐT lạ: %v", l)
	}
	if r := mgr.appDo(http.MethodGet, base+"nguyen", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("không phải SĐT: %d %v", r.status, r.body)
	}
	if !slices.Contains(f.vault.usedPurposes(), "registry.admin") {
		t.Fatal("tra SĐT qua kho phải kèm mục đích")
	}
	// Quản lý HTX khác không tra được hộ của HTX này.
	if r := f.as(f.tenant, tr.c2, partyport.RoleCoopManager).appDo(http.MethodGet, base+"0908200001", nil); r.status != http.StatusNotFound {
		t.Fatalf("HTX khác tra: %d", r.status)
	}
}

// KN US-HTX-03: nhập nông hộ bằng tệp Excel .xlsx (tiêu đề tiếng Việt có dấu), kết quả theo dòng.
func TestImportHouseholdsXLSX(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1, c2 := f.coopCodes(tr)
	header := []string{"Họ tên", "Số điện thoại", "CCCD", "Địa chỉ", "Mã HTX", "Diện tích (ha)", "Ngày vào HTX"}
	body := xlsxBytes(t, header, [][]string{
		{"Trần Thị Bình", "0907000001", "", "Ấp 2", c1, "2.5", "2023-05-01"},
		{"Trần Thị Bình", "0907000001", "", "Ấp 2", c1, "2.5", "2023-05-01"},
		{"Lê Văn Cường", "", "", "Ấp 3", c1, "1", ""},
		{},
		{"Phạm Văn Dũng", "0907000004", "", "Ấp 4", "HTX999999", "1", ""},
		{"Võ Thị Em", "0907000005", "", "Ấp 5", c2, "1", ""},
	})
	prov := f.as(f.tenant, tr.p89, partyport.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), tabular.ContentTypeXLSX, body)
	if r.status != http.StatusOK || r.body["accepted"] != float64(1) || r.body["duplicate"] != float64(1) || r.body["rejected"] != float64(3) {
		t.Fatalf("kết quả nhập .xlsx: %d %v", r.status, r.body)
	}
	// Số dòng là dòng thật của trang tính (dòng trống thứ 5 bị bỏ qua).
	wantStatus := []string{"accepted", "duplicate", "rejected", "rejected", "rejected"}
	wantLine := []float64{2, 3, 4, 6, 7}
	for i, row := range rowsOf(r) {
		if row["status"] != wantStatus[i] || row["row"] != wantLine[i] {
			t.Fatalf("dòng %d: %v", i, row)
		}
		if reason, _ := row["reason"].(string); strings.Contains(reason, "0907") || strings.Contains(reason, "Trần") {
			t.Fatalf("lý do không được chứa dữ liệu cá nhân: %q", reason)
		}
	}
	if got := len(items(prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))); got != 1 {
		t.Fatalf("HTX 1 có đúng 1 nông hộ: %d", got)
	}
}

// Ô công thức trong .xlsx: cả tệp bị từ chối, không tính công thức.
func TestImportHouseholdsXLSXFormulaRejected(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c1, _ := f.coopCodes(tr)
	x := excelize.NewFile()
	_ = x.SetSheetRow("Sheet1", "A1", &[]any{"ho_ten", "so_dien_thoai", "ma_htx", "dien_tich_ha"})
	_ = x.SetSheetRow("Sheet1", "A2", &[]any{"Ngô Văn Giàu", "0907000009", c1, "1"})
	_ = x.SetSheetRow("Sheet1", "A3", &[]any{"Ngô Thị Hoa", "0907000010", c1})
	_ = x.SetCellFormula("Sheet1", "D3", "D2*2")
	var buf bytes.Buffer
	if _, err := x.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	prov := f.as(f.tenant, tr.p89, partyport.RoleKNProvinceAdmin)
	r := prov.call(f.admin.URL, http.MethodPost, "/v1/households/import", key(), tabular.ContentTypeXLSX, buf.Bytes())
	if r.status != http.StatusUnprocessableEntity || r.body["code"] != "party.import_invalid" || !strings.Contains(str(r.body, "detail"), "D3") {
		t.Fatalf("ô công thức phải bị từ chối cả tệp: %d %v", r.status, r.body)
	}
	if got := len(items(prov.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String()+"/households", nil))); got != 0 {
		t.Fatalf("không nạp dòng nào khi tệp bị từ chối: %d", got)
	}
}

// GIS US-BND-02: pf/party cài geo/port.CoopMarkers (điểm vị trí hồ sơ HTX trong khung nhìn)
// và CooperativeSites (vị trí + số hộ cho mô phỏng); cách ly tenant.
func TestCoopMarkersAndSites(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	if r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0904000010")); r.status != http.StatusCreated {
		t.Fatalf("tạo hộ: %v", r.body)
	}
	ctx := tenant.WithID(context.Background(), f.tenant)
	ms, err := f.svc.MarkersInBBox(ctx, 105.0, 10.0, 106.0, 11.0, 100)
	if err != nil || len(ms) != 1 || ms[0].Ref != tr.c1 || ms[0].CommuneCode != "30001" || ms[0].Lat.String() != "10.3861" {
		t.Fatalf("marker HTX: %+v %v", ms, err)
	}
	if ms, _ := f.svc.MarkersInBBox(ctx, 100.0, 20.0, 101.0, 21.0, 100); len(ms) != 0 {
		t.Fatalf("ngoài khung nhìn: %+v", ms)
	}
	if ms, _ := f.svc.MarkersInBBox(tenant.WithID(context.Background(), ids.New()), 105, 10, 106, 11, 100); len(ms) != 0 {
		t.Fatal("tenant khác")
	}
	sites, err := f.svc.ActiveCooperatives(ctx)
	if err != nil || len(sites) != 2 {
		t.Fatalf("HTX đang hoạt động: %+v %v", sites, err)
	}
	for _, s := range sites {
		switch s.Ref {
		case tr.c1:
			if s.Members != 1 || s.Lon == nil || s.Lon.String() != "105.4352" {
				t.Fatalf("c1: %+v", s)
			}
		case tr.c2:
			if s.Members != 0 || s.Lon != nil {
				t.Fatalf("c2 chưa có toạ độ: %+v", s)
			}
		}
	}
}
