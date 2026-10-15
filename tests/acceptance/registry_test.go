package acceptance

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Sổ HTX, tổ chức và nông hộ (pf/party + kho định danh).

// Nguồn: mekong-green/tests/htx-operations.test.ts — "MST-01 — mã số thuế phải 10 hoặc 13 chữ số"
// và "Khuyến nông lập hồ sơ HTX kèm MST; MST không được trùng".
// MST duy nhất theo tenant (RLS); tạo qua Neo D2, HTX tự nhận quyền sau khi xác minh MST.
func TestPrototypeMaSoThueHTX(t *testing.T) {
	nguon(t, "htx-operations.test.ts", "MST-01 — mã số thuế phải 10 hoặc 13 chữ số")
	nguon(t, "htx-operations.test.ts", "Khuyến nông lập hồ sơ HTX kèm MST; MST không được trùng")
	w := newWorld(t)
	coop := func(name, tax string) harness.Resp {
		return w.root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", map[string]any{
			"created_by": "mkg", "mst": tax,
			"new_cooperative": map[string]any{"name": name, "admin_unit_code": communeNK, "parent_ref": w.teamNK},
		})
	}
	r := w.want(coop("HTX Tân Tiến", "1600999888"), 201, "HTX có MST 10 số")
	ref := str(r.Body, "org_ref")
	got := w.want(w.root.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+ref, "", nil), 200, "đọc HTX")
	if str(got.Body, "tax_code") != "1600999888" {
		t.Fatalf("MST lưu sai: %s", trim(got.Raw))
	}
	w.want(coop("HTX đơn vị phụ thuộc", "1600123456001"), 201, "MST 13 số (đơn vị phụ thuộc)")
	// Viết khác dạng (có dấu gạch) vẫn là cùng MST sau chuẩn hóa → trùng.
	wantProblem(t, coop("HTX trùng MST", "1600-999-888"), 409, "cooperative.htx_anchor_mst_taken", "MST trùng")
	for _, bad := range []string{"12345", "abc"} {
		wantProblem(t, coop("HTX MST sai "+bad, bad), 422, "cooperative.htx_anchor_mst_malformed", "MST sai định dạng "+bad)
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "Vô hiệu hoá HTX cần lý do ≥ 20 ký tự, khoá tài khoản liên quan và khôi phục được".
// Khác prototype: không khóa tài khoản trực tiếp — tài khoản thuộc HTX mất phạm vi (mặc định chặn) và
// sự kiện pf.party.org.deactivated được phát; mọi thay đổi là dòng mới trong sổ chỉ ghi thêm.
func TestPrototypeVoHieuHoaHTXCanLyDo(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "Vô hiệu hoá HTX cần lý do ≥ 20 ký tự, khoá tài khoản liên quan và khôi phục được")
	w := newWorld(t)
	w.household(w.officerBT, w.coopBT, "Nguyễn Văn Tổng Hợp", "0999000111")
	manager := w.user(w.coopBT, false, "coop_manager") // HTX duy nhất của xã Cái Răng
	if n := len(w.want(manager.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200, "quản lý HTX xem HTX").Items()); n != 1 {
		t.Fatalf("quản lý HTX phải thấy HTX mình, thấy %d", n)
	}
	path := "/v1/cooperatives/" + w.coopBT
	wantFieldError(t, w.root.Do(harness.Admin, http.MethodPost, path+"/deactivate", "", map[string]any{"reason": "ngắn"}), "reason", "lý do quá ngắn")
	reason := "HTX giải thể theo quyết định của UBND xã năm 2026"
	r := w.want(w.root.Do(harness.Admin, http.MethodPost, path+"/deactivate", "", map[string]any{"reason": reason}), 200, "vô hiệu hóa HTX")
	if num(r.Body, "active_households") != 1 {
		t.Fatalf("phải báo số hộ đang hoạt động bị ảnh hưởng: %s", trim(r.Raw))
	}
	active := refsOf(w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200, "danh sách HTX").Items(), "ref")
	if active[w.coopBT] {
		t.Fatal("HTX vô hiệu hóa không được còn trong danh sách hoạt động")
	}
	all := refsOf(w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/cooperatives?include_inactive=true", "", nil), 200, "danh sách kể cả ngừng").Items(), "ref")
	if !all[w.coopBT] {
		t.Fatal("include_inactive phải thấy HTX đã vô hiệu hóa")
	}
	if n := len(w.want(manager.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200, "quản lý HTX sau vô hiệu hóa").Items()); n != 0 {
		t.Fatalf("tài khoản thuộc HTX đã vô hiệu hóa phải mất phạm vi, còn thấy %d", n)
	}
	w.want(w.root.Do(harness.Admin, http.MethodPost, path+"/reactivate", "", map[string]any{"reason": "Tái tham gia đề án"}), 200, "khôi phục HTX")
	active = refsOf(w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200, "danh sách sau khôi phục").Items(), "ref")
	if !active[w.coopBT] {
		t.Fatal("khôi phục xong phải thấy lại HTX")
	}
	ev := w.want(w.root.Do(harness.Admin, http.MethodGet, "/v1/orgs/"+w.coopBT+"/events", "", nil), 200, "lịch sử HTX").Items()
	var actions []string
	for _, e := range ev {
		actions = append(actions, str(e, "action"))
	}
	if strings.Join(actions, ",") != "created,deactivated,reactivated" || str(ev[1], "reason") != reason {
		t.Fatalf("lịch sử phải có created → deactivated (kèm lý do) → reactivated: %v", actions)
	}
}

// Nguồn: mekong-green/tests/db-constraints.test.ts — "CCCD: mã hoá khi lưu, dịch vụ giải mã, che chỉ lộ 3 số cuối, giá trị che gửi lại không ghi đè"
// và htx-operations.test.ts — "Khuyến nông lập hồ sơ HTX kèm MST; MST không được trùng" (contactName/contactPhone).
// Khác prototype: họ tên/SĐT người đại diện KHÔNG nằm ở cụm pf (kể cả dạng mã hóa) — chỉ ở kho
// định danh; pf giữ representative_ref (R-04).
func TestPrototypeNguoiDaiDienHTXOKhoDinhDanh(t *testing.T) {
	nguon(t, "db-constraints.test.ts", "CCCD: mã hoá khi lưu, dịch vụ giải mã, che chỉ lộ 3 số cuối, giá trị che gửi lại không ghi đè")
	w := newWorld(t)
	r := w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", map[string]any{
		"created_by": "mkg", "new_cooperative": map[string]any{
			"name": "HTX có người đại diện", "admin_unit_code": communeNK, "parent_ref": w.teamNK,
			"representative": map[string]any{"full_name": "Phạm Văn Đại Diện", "phone": "0999000209"},
		},
	}), 201, "tạo HTX kèm đại diện qua Neo")
	got := w.want(w.root.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+str(r.Body, "org_ref"), "", nil), 200, "đọc HTX")
	org := got.Body
	rep := str(org, "representative_ref")
	if rep == "" {
		t.Fatalf("HTX phải có representative_ref: %s", trim(r.Raw))
	}
	if containsAny(string(got.Raw)+string(r.Raw), "Phạm Văn Đại Diện", "0999000209") {
		t.Fatalf("pf không được trả dữ liệu cá nhân của người đại diện: %s", trim(got.Raw))
	}
	lk := w.root.DoRaw(harness.Vault, http.MethodPost, "/v1/subjects/lookup",
		http.Header{"Content-Type": {"application/json"}, "X-Purpose": {"registry.dedup"}},
		strings.NewReader(`{"purpose":"registry.dedup","value":"0999 000 209"}`))
	if lk.Status != 200 || str(lk.Body, "ref") != rep {
		t.Fatalf("kho định danh phải tìm được người đại diện theo SĐT: %d %s", lk.Status, trim(lk.Raw))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-ADM-02: trùng số điện thoại bị chặn; định dạng SĐT/email sai bị chặn; …"
// và "UAT DEF-CGH-08: số điện thoại được che theo mẫu 4 đầu · 3 cuối".
// Khác prototype: trùng SĐT không trả lỗi mà trả 200 duplicate + ref của hộ đã có (chống trùng
// bằng chỉ mục mù ở kho định danh); nhãn hiển thị do kho định danh che (chỉ kiểm không lộ số đầy đủ
// và giữ 3 số cuối — mẫu che khác prototype, xem báo cáo).
func TestPrototypeNongHoTrungSoDienThoai(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-ADM-02: trùng số điện thoại bị chặn; định dạng SĐT/email sai bị chặn; đăng nhập ưu tiên tài khoản hoạt động")
	nguon(t, "brd-2026.test.ts", "UAT DEF-CGH-08: số điện thoại được che theo mẫu 4 đầu · 3 cuối")
	w := newWorld(t)
	first := w.household(w.officer, w.coopNK, "Trần Thị Mô Phỏng", "0999000101")
	dup := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": w.coopNK, "identity": map[string]any{"full_name": "Tên Khác Hẳn", "phone": "0999 000 101"},
	}), 200, "hộ trùng SĐT (khác cách viết)")
	if dup.Body["duplicate"] != true || str(obj(dup.Body, "household"), "household_ref") != first {
		t.Fatalf("trùng SĐT phải trả duplicate + ref hộ cũ: %s", trim(dup.Raw))
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": w.coopNK, "identity": map[string]any{"full_name": "Sai SĐT", "phone": "12345"},
	}), 422, "validation_failed", "SĐT sai định dạng")
	wantFieldError(t, w.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": w.coopNK, "identity": map[string]any{"phone": "0999000102"},
	}), "identity", "thiếu họ tên")

	items := w.want(w.officer.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+w.coopNK+"/households", "", nil), 200, "hộ của HTX").Items()
	if len(items) != 1 {
		t.Fatalf("HTX phải có đúng 1 hộ (không sinh bản trùng), có %d", len(items))
	}
	masked := str(items[0], "masked_phone")
	// UAT DEF-CGH-08: che theo mẫu 4 số đầu · 3 số cuối.
	if masked != "0999•••101" {
		t.Fatalf("SĐT phải được che theo mẫu 4 đầu · 3 cuối: %q", masked)
	}
	if containsAny(str(items[0], "label"), "Trần Thị Mô Phỏng") {
		t.Fatalf("nhãn hiển thị phải che bớt họ tên: %q", str(items[0], "label"))
	}
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-KN-02: cán bộ xã có HTX/đầu mối phụ trách chỉ thấy địa bàn xã, không phải cả tỉnh".
// và scopes.test.ts — "SA-08: admin KN tỉnh An Giang thấy cán bộ An Giang, KHÔNG thấy Đồng Tháp, …".
// Khác prototype: phạm vi suy ra từ đơn vị công tác (org_ref) trong token, không từ bảng phạm vi riêng.
func TestPrototypeCanBoXaChiThayDiaBanXa(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-KN-02: cán bộ xã có HTX/đầu mối phụ trách chỉ thấy địa bàn xã, không phải cả tỉnh")
	nguon(t, "scopes.test.ts", "SA-08: admin KN tỉnh An Giang thấy cán bộ An Giang, KHÔNG thấy Đồng Tháp, ERP, hay super admin")
	w := newWorld(t)
	seen := func(u *harness.User) map[string]bool {
		return refsOf(w.want(u.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200, "danh sách HTX").Items(), "ref")
	}
	xa := seen(w.officer)
	if !xa[w.coopNK] || !xa[w.coopNK2] || xa[w.coopBT] || xa[w.coopLX] || len(xa) != 2 {
		t.Fatalf("cán bộ xã Ninh Kiều chỉ thấy 2 HTX của xã: %v", xa)
	}
	tinh := seen(w.provAdmin)
	if !tinh[w.coopNK] || !tinh[w.coopNK2] || !tinh[w.coopBT] || tinh[w.coopLX] {
		t.Fatalf("TTKN Cần Thơ thấy mọi HTX của tỉnh, không thấy An Giang: %v", tinh)
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/households", "", map[string]any{
		"cooperative_ref": w.coopBT, "identity": map[string]any{"full_name": "Người Xã Khác", "phone": "0999000121"},
	}), 403, "party.out_of_scope", "cán bộ xã ghi hộ vào HTX xã khác")
}

// Nguồn: mekong-green/tests/brd-2026.test.ts — "UAT DEF-SYS-01: lỗi ràng buộc CSDL được dịch sang thông điệp nghiệp vụ tiếng Việt"
// (UNIQUE org_nodes.code → "Mã đầu mối tổ chức đã tồn tại").
// Khác prototype: không dịch chuỗi lỗi CSDL — mỗi vi phạm có mã lỗi ổn định trong problem+json
// (party.org_code_taken) kèm tiêu đề tiếng Việt và trace_id.
func TestPrototypeMaToChucTrung(t *testing.T) {
	nguon(t, "brd-2026.test.ts", "UAT DEF-SYS-01: lỗi ràng buộc CSDL được dịch sang thông điệp nghiệp vụ tiếng Việt")
	w := newWorld(t)
	body := func(name string) map[string]any {
		return map[string]any{"kind": "extension_team", "code": "TKN-TRUNG", "name": name, "admin_unit_code": communeNK, "parent_ref": w.orgP92}
	}
	w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", body("Tổ KN thứ nhất")), 201, "tạo tổ KN")
	r := wantProblem(t, w.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", body("Tổ KN thứ hai")), 409, "party.org_code_taken", "mã tổ chức trùng")
	if !regexp.MustCompile(`Mã tổ chức`).MatchString(str(r.Body, "title")) {
		t.Fatalf("tiêu đề lỗi phải là thông điệp nghiệp vụ tiếng Việt: %s", trim(r.Raw))
	}
	// Con không cùng địa bàn với cha → vi phạm cây phân cấp (không phải lỗi CSDL thô).
	wantProblem(t, w.root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{
		"kind": "extension_team", "code": "TKN-SAI", "name": "Tổ sai tỉnh", "admin_unit_code": communeLX, "parent_ref": w.orgP92,
	}), 422, "party.invalid_hierarchy", "tổ xã An Giang dưới TTKN Cần Thơ")
}
