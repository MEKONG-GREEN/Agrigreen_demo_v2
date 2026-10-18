package handler_test

// W11.07 — phân quyền trong vai + đơn vị nội bộ: thuộc tính vai vào ngữ cảnh phiên, Lãnh đạo đơn vị xem chéo
// phòng ban chỉ đọc, ủy quyền có hạn, phạm vi Cấp Xã theo cơ sở/hộ, phạm vi nhân sự (HS-37). Kèm ca âm.

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func asgLevel(role string, org uuid.UUID, depts []string, level string, communes ...string) map[string]any {
	m := asg(role, org, depts, communes...)
	m["mgmt_level"] = level
	return m
}

func (f *knFix) setAttrs(user uuid.UUID, aid string, body map[string]any) resp {
	return f.req(f.admin2, f.root, http.MethodPut, "/v1/accounts/users/"+user.String()+"/assignments/"+aid+"/attributes", body,
		map[string]string{"Idempotency-Key": key()})
}

func (f *knFix) probeKN(c caller, ctx string) authz.Principal {
	f.t.Helper()
	if r := f.withCtx(c, http.MethodGet, "/v1/probe", ctx, nil); r.status != http.StatusOK {
		f.t.Fatalf("probe: %d %v", r.status, r.body)
	}
	return f.lastProbe()
}

// seedUnits nạp đơn vị nội bộ mặc định cho tỉnh 91 và thêm một trạm.
func (f *knFix) seedUnits() {
	f.t.Helper()
	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": f.p91.String()})
	if r.status != http.StatusOK {
		f.t.Fatalf("seed đơn vị nội bộ: %d %v", r.status, r.body)
	}
	r = f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.p91.String(), "code": "tram_1",
		"kind": "tram", "name": "Trạm Khuyến nông 1", "sort_order": 60})
	if r.status != http.StatusCreated {
		f.t.Fatalf("tạo trạm: %d %v", r.status, r.body)
	}
}

func TestW1107_InternalUnitCatalog(t *testing.T) {
	f := newKNFixture(t)
	f.seedUnits()
	// Gọi lặp seed an toàn; phòng không có phòng ban P2 (ke_hoach_tai_chinh) tạo không gắn phòng ban.
	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": f.p91.String()})
	if r.status != http.StatusOK || r.body["existing"].(float64) != 6 || len(r.body["created"].([]any)) != 0 {
		t.Fatalf("seed lặp: %d %v", r.status, r.body)
	}
	r = f.adminDo(f.root, http.MethodGet, "/v1/accounts/internal-units?org_ref="+f.p91.String(), nil)
	items, _ := r.body["items"].([]any)
	if len(items) != 7 {
		t.Fatalf("danh mục: %v", r.body)
	}
	byCode := map[string]map[string]any{}
	for _, x := range items {
		m := x.(map[string]any)
		byCode[m["code"].(string)] = m
	}
	if byCode["trong_trot"]["department_code"] != deptTT || byCode["ke_hoach_tai_chinh"]["department_code"] != nil ||
		byCode["tram_1"]["kind_label"] != "Trạm" {
		t.Fatalf("liên kết phòng ban: %v", byCode)
	}
	// Ca âm: trùng mã, loại lạ, trạm gắn phòng ban, Nút không phải cấp 1/2, người không phải Admin.
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.p91.String(), "code": "tram_1",
		"kind": "tram", "name": "x"}), http.StatusConflict, "accounts.internal_unit_code_taken", "trùng mã")
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.p91.String(), "code": "doi_1",
		"kind": "doi", "name": "x"}), http.StatusUnprocessableEntity, "validation_failed", "loại lạ")
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.p91.String(), "code": "tram_2",
		"kind": "tram", "name": "x", "department_code": deptTT}), http.StatusUnprocessableEntity, "validation_failed", "trạm gắn phòng ban")
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.team30292.String(), "code": "tram_3",
		"kind": "tram", "name": "x"}), http.StatusUnprocessableEntity, "accounts.internal_unit_org_invalid", "Nút xã")
	_, an, _ := f.createKN("Nguyễn Văn An", asg(port.KNTinh, f.p91, []string{deptTT}))
	if r := f.req(f.admin2, an, http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": f.p91.String(), "code": "tram_4",
		"kind": "tram", "name": "x"}, nil); r.status != http.StatusForbidden && r.status != http.StatusUnauthorized {
		t.Fatalf("người không phải Admin tạo đơn vị: %d", r.status)
	}
	// Ngừng cần lý do; Log có cũ → mới.
	id := byCode["tram_1"]["id"].(string)
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units/"+id+"/deactivate", map[string]any{"reason": "ngắn"}),
		http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/internal-units/"+id+"/deactivate",
		map[string]any{"reason": "sáp nhập vào Trạm Khuyến nông 2"}); r.status != http.StatusOK || r.body["active"] != false {
		t.Fatalf("ngừng: %d %v", r.status, r.body)
	}
	if r := f.adminDo(f.root, http.MethodGet, "/v1/accounts/internal-units/"+id+"/log", nil); len(r.body["items"].([]any)) != 2 {
		t.Fatalf("log: %v", r.body)
	}
}

func TestW1107_AttributesFlowIntoContext(t *testing.T) {
	f := newKNFixture(t)
	f.seedUnits()
	leadID, lead, cr := f.createKN("Trần Thị Lan", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtTruongPhong))
	staffID, staff, cs := f.createKN("Phạm Văn Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	xaID, xa, cx := f.createKN("Võ Văn Cần", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	otherXaID, _, cox := f.createKN("Lý Thị Đào", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	la, sa, xa1, ox := asgIDs(cr)[port.KNTinh], asgIDs(cs)[port.KNTinh], asgIDs(cx)[port.KNXa], asgIDs(cox)[port.KNXa]
	fac, hh := ids.New(), ids.New()

	r := f.setAttrs(leadID, la, map[string]any{"province_group": "lanh_dao_phong", "duties": []string{"qtnv_bao_gia"},
		"internal_unit_code": "trong_trot", "reason": "phân quyền ban đầu"})
	if r.status != http.StatusOK || str(r.body, "attributes", "province_group_label") != "Lãnh đạo phòng" {
		t.Fatalf("đặt thuộc tính: %d %v", r.status, r.body)
	}
	if r := f.setAttrs(staffID, sa, map[string]any{"province_group": "tong_hop", "internal_unit_code": "trong_trot"}); r.status != http.StatusOK {
		t.Fatalf("thuộc tính cán bộ: %v", r.body)
	}
	if r := f.setAttrs(xaID, xa1, map[string]any{"internal_unit_code": "trong_trot", "facility_refs": []string{fac.String()},
		"household_refs": []string{hh.String()}}); r.status != http.StatusOK {
		t.Fatalf("thuộc tính Cấp Xã: %v", r.body)
	}
	if r := f.setAttrs(otherXaID, ox, map[string]any{"internal_unit_code": "tram_1"}); r.status != http.StatusOK {
		t.Fatalf("thuộc tính Cấp Xã 2: %v", r.body)
	}

	// Ca âm: nhóm/cờ ngoài Cấp Tỉnh, cơ sở ngoài Cấp Xã, mã lạ, đơn vị nội bộ không có, lần gán của người khác.
	wantCode(t, f.setAttrs(xaID, xa1, map[string]any{"province_group": "tong_hop"}), http.StatusUnprocessableEntity, "validation_failed", "nhóm ở Cấp Xã")
	wantCode(t, f.setAttrs(staffID, sa, map[string]any{"facility_refs": []string{fac.String()}}), http.StatusUnprocessableEntity, "validation_failed", "cơ sở ở Cấp Tỉnh")
	wantCode(t, f.setAttrs(staffID, sa, map[string]any{"duties": []string{"sieu_quyen"}}), http.StatusUnprocessableEntity, "validation_failed", "cờ lạ")
	wantCode(t, f.setAttrs(staffID, sa, map[string]any{"province_group": "giam_doc_so"}), http.StatusUnprocessableEntity, "validation_failed", "nhóm lạ")
	wantCode(t, f.setAttrs(staffID, sa, map[string]any{"internal_unit_code": "trai_khong_co"}), http.StatusUnprocessableEntity,
		"accounts.internal_unit_invalid", "đơn vị không có")
	wantCode(t, f.setAttrs(staffID, la, map[string]any{"province_group": "tong_hop"}), http.StatusNotFound, "accounts.assignment_not_found", "lần gán người khác")

	// Ngữ cảnh mang thuộc tính.
	lk := *f.probeKN(lead, f.ctxID(lead, la, deptTT)).KN
	if !lk.Manager || lk.ProvinceGroup != "lanh_dao_phong" || lk.InternalUnit != "trong_trot" || !lk.HasDuty(authz.DutyPriceAdmin) ||
		lk.HasDuty(authz.DutyFormOwner) || !lk.Can(authz.PowerReopenPeriod) || lk.ReadOnly() {
		t.Fatalf("ngữ cảnh lãnh đạo: %+v", lk)
	}
	sp := f.probeKN(staff, f.ctxID(staff, sa, deptTT))
	if sp.KN.Manager || sp.KN.Can(authz.PowerReopenPeriod) || sp.KN.HasDuty(authz.DutyPriceAdmin) {
		t.Fatalf("ngữ cảnh cán bộ: %+v", sp.KN)
	}
	xp := f.probeKN(xa, f.ctxID(xa, xa1, deptTT))
	xk := *xp.KN
	if !xk.AllowsFacility(fac) || xk.AllowsFacility(ids.New()) || !xk.AllowsHousehold(hh) || xk.AllowsHousehold(ids.New()) ||
		xk.InternalUnit != "trong_trot" || !xk.Scope.Allows("30292") || xk.Scope.Allows("30301") {
		t.Fatalf("phạm vi Cấp Xã theo cơ sở/hộ: %+v", xk)
	}
	// Chặn Nút FD giữ nguyên: Cấp Xã có giới hạn cơ sở vẫn bắt buộc Nút, Nút vẫn đặt vào biến DB.
	if !db.RequiresNode(xp) || db.ContextVarsFrom(authz.WithPrincipal(context.Background(), xp)).NodeID == uuid.Nil {
		t.Fatal("Cấp Xã phải bắt buộc Nút (checkNodeScope)")
	}
	noNode := xp
	kn := *xp.KN
	kn.OrgRef = uuid.Nil
	noNode.KN = &kn
	if !db.RequiresNode(noNode) {
		t.Fatal("ngữ cảnh Cấp Xã thiếu Nút vẫn phải bị chặn")
	}
	// GET /v1/me/context trả thuộc tính.
	r = f.withCtx(lead, http.MethodGet, "/v1/me/context", f.ctxID(lead, la, deptTT), nil)
	if r.body["manager"] != true || r.body["internal_unit_code"] != "trong_trot" || r.body["read_only"] != false {
		t.Fatalf("GET /v1/me/context: %v", r.body)
	}

	// Phạm vi nhân sự (BR-04, HS-37): lãnh đạo thấy người của đơn vị + cán bộ xã do đơn vị quản lý; cán bộ chỉ mình.
	people := f.kmod.KNPeople()
	ps, err := people.PeopleInScope(authz.WithPrincipal(context.Background(), f.probeKN(lead, f.ctxID(lead, la, deptTT))))
	if err != nil || ps.All || !ps.Allows(leadID) || !ps.Allows(staffID) || !ps.Allows(xaID) || ps.Allows(otherXaID) ||
		!slices.Contains(ps.CommuneOfficers, xaID) {
		t.Fatalf("phạm vi lãnh đạo: %+v %v", ps, err)
	}
	ps, err = people.PeopleInScope(authz.WithPrincipal(context.Background(), sp))
	if err != nil || ps.All || ps.Allows(leadID) || !ps.Allows(staffID) {
		t.Fatalf("phạm vi cán bộ: %+v %v", ps, err)
	}
	ps, err = people.PeopleInScope(authz.WithPrincipal(context.Background(), xp))
	if err != nil || ps.All || ps.Allows(staffID) {
		t.Fatalf("phạm vi Cấp Xã: %+v %v", ps, err)
	}
	// Đổi thuộc tính có hiệu lực ở request kế tiếp (không cần chọn lại ngữ cảnh).
	ctxLead := f.ctxID(lead, la, deptTT)
	if r := f.setAttrs(leadID, la, map[string]any{"internal_unit_code": "trong_trot"}); r.status != http.StatusOK {
		t.Fatalf("bỏ nhóm: %v", r.body)
	}
	if k := f.probeKN(lead, ctxLead).KN; k.ProvinceGroup != "" || k.HasDuty(authz.DutyPriceAdmin) || !k.Manager {
		t.Fatalf("sau khi bỏ nhóm/cờ (Trưởng phòng vẫn là lãnh đạo): %+v", k)
	}
	// Khung Log tài khoản có sự kiện thuộc tính vai (cũ → mới).
	r = f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+leadID.String()+"/log", nil)
	n := 0
	for _, x := range r.body["items"].([]any) {
		if x.(map[string]any)["event"] == "assignment.attributes_changed" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("Log thuộc tính vai: %d sự kiện, %v", n, r.body)
	}
	// Danh sách vai của màn Detail tài khoản trả thuộc tính.
	r = f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+xaID.String()+"/assignments", nil)
	list, _ := r.body["items"].([]any)
	if len(list) != 1 || len(list[0].(map[string]any)["attributes"].(map[string]any)["facility_refs"].([]any)) != 1 {
		t.Fatalf("danh sách vai kèm thuộc tính: %v", r.body)
	}
}

func TestW1107_UnitLeaderReadOnlyAcrossDepartments(t *testing.T) {
	f := newKNFixture(t)
	_, boss, cb := f.createKN("Đỗ Văn Giám", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtLanhDaoDonVi))
	_, staff, cs := f.createKN("Phạm Văn Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	ba, sa := asgIDs(cb)[port.KNTinh], asgIDs(cs)[port.KNTinh]

	r := f.req(f.app, boss, http.MethodGet, "/v1/me/contexts", nil, nil)
	depts := r.body["assignments"].([]any)[0].(map[string]any)["departments"].([]any)
	ro := map[string]bool{}
	for _, d := range depts {
		m := d.(map[string]any)
		ro[m["code"].(string)] = m["read_only"].(bool)
	}
	if len(ro) != 3 || ro[deptTT] || !ro[deptBVTV] || !ro[deptCN] {
		t.Fatalf("Lãnh đạo đơn vị thấy mọi phòng ban, ngoài lần gán là chỉ xem: %v", ro)
	}
	// Phòng ban khác: chọn được, chỉ xem; đọc được, ghi bị chặn ở middleware (mọi route).
	r = f.selectCtx(boss, ba, deptBVTV)
	if r.status != http.StatusCreated || r.body["read_only"] != true {
		t.Fatalf("chọn phòng ban khác: %d %v", r.status, r.body)
	}
	other := str(r.body, "context_id")
	if k := f.probeKN(boss, other).KN; !k.ViewOnly || !k.ReadOnly() || k.Can(authz.PowerReopenPeriod) || k.Department != deptBVTV {
		t.Fatalf("ngữ cảnh chỉ xem: %+v", k)
	}
	wantCode(t, f.withCtx(boss, http.MethodPost, "/v1/probe/x/write", other, map[string]any{}), http.StatusForbidden,
		"accounts.context_read_only", "ghi trong ngữ cảnh chỉ xem")
	// Phòng ban chưa mở vẫn không chọn được; phòng ban của mình ghi được.
	wantCode(t, f.selectCtx(boss, ba, deptCN), http.StatusConflict, "accounts.department_not_open", "phòng ban chưa mở")
	own := f.ctxID(boss, ba, deptTT)
	if r := f.withCtx(boss, http.MethodPost, "/v1/probe/x/write", own, map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("ghi ở phòng ban của mình: %d %v", r.status, r.body)
	}
	// Ca âm: cán bộ thường không chọn được phòng ban ngoài lần gán.
	wantCode(t, f.selectCtx(staff, sa, deptBVTV), http.StatusUnprocessableEntity, "accounts.department_required", "chuyên viên chọn phòng ban khác")
	r = f.req(f.app, staff, http.MethodGet, "/v1/me/contexts", nil, nil)
	if n := len(r.body["assignments"].([]any)[0].(map[string]any)["departments"].([]any)); n != 1 {
		t.Fatalf("chuyên viên chỉ thấy phòng ban của mình: %d", n)
	}
}

func TestW1107_BanGiamDocAlwaysViewOnly(t *testing.T) {
	f := newKNFixture(t)
	id, bgd, cr := f.createKN("Huỳnh Văn Đốc", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtPhoGiamDoc))
	a := asgIDs(cr)[port.KNTinh]
	if r := f.setAttrs(id, a, map[string]any{"province_group": "ban_giam_doc"}); r.status != http.StatusOK {
		t.Fatalf("nhóm BGĐ: %v", r.body)
	}
	ctx := f.ctxID(bgd, a, deptTT)
	if k := f.probeKN(bgd, ctx).KN; !k.ViewOnly {
		t.Fatalf("BGĐ ở phòng ban của mình phải chỉ xem: %+v", k)
	}
	wantCode(t, f.withCtx(bgd, http.MethodPost, "/v1/probe/x/write", ctx, map[string]any{}), http.StatusForbidden, "accounts.context_read_only", "BGĐ ghi")
	if r := f.selectCtx(bgd, a, deptBVTV); r.status != http.StatusCreated || r.body["read_only"] != true {
		t.Fatalf("BGĐ xem phòng ban khác: %v", r.body)
	}
}

func TestW1107_TimeLimitedDelegation(t *testing.T) {
	f := newKNFixture(t)
	leadID, lead, cr := f.createKN("Trần Thị Lan", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtTruongPhong))
	staffID, staff, cs := f.createKN("Phạm Văn Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	outsiderID, _, _ := f.createKN("Lê Văn Cường", asg(port.KNTinh, f.p91, []string{deptBVTV}))
	la, sa := asgIDs(cr)[port.KNTinh], asgIDs(cs)[port.KNTinh]
	leadCtx, staffCtx := f.ctxID(lead, la, deptTT), f.ctxID(staff, sa, deptTT)
	until := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	deleg := func(c caller, ctx string, body map[string]any) resp {
		return f.withCtx(c, http.MethodPost, "/v1/me/delegations", ctx, body)
	}
	body := func(grantee uuid.UUID, power string, d time.Duration) map[string]any {
		return map[string]any{"grantee_ref": grantee.String(), "power": power, "valid_to": until(d), "reason": "đi công tác Hà Nội một tuần"}
	}

	// Ca âm.
	wantCode(t, deleg(staff, staffCtx, body(leadID, authz.PowerReopenPeriod, 24*time.Hour)), http.StatusForbidden,
		"accounts.delegation_forbidden", "cán bộ không phải lãnh đạo ủy quyền")
	wantCode(t, deleg(lead, leadCtx, body(outsiderID, authz.PowerReopenPeriod, 24*time.Hour)), http.StatusUnprocessableEntity,
		"accounts.delegation_grantee_invalid", "người nhận khác phòng ban")
	wantCode(t, deleg(lead, leadCtx, body(staffID, authz.PowerReopenPeriod, 40*24*time.Hour)), http.StatusUnprocessableEntity,
		"validation_failed", "quá 30 ngày")
	wantCode(t, deleg(lead, leadCtx, body(leadID, authz.PowerReopenPeriod, 24*time.Hour)), http.StatusUnprocessableEntity,
		"validation_failed", "tự ủy quyền")
	wantCode(t, deleg(lead, leadCtx, body(staffID, "xoa_du_lieu", 24*time.Hour)), http.StatusUnprocessableEntity, "validation_failed", "quyền lạ")
	b := body(staffID, authz.PowerReopenPeriod, 24*time.Hour)
	b["department_code"] = deptBVTV
	wantCode(t, deleg(lead, leadCtx, b), http.StatusForbidden, "accounts.delegation_forbidden", "phòng ban khác ngữ cảnh")

	// Ủy quyền hợp lệ → người nhận có quyền ở request kế tiếp, chỉ quyền được giao.
	r := deleg(lead, leadCtx, body(staffID, authz.PowerReopenPeriod, 7*24*time.Hour))
	if r.status != http.StatusCreated || r.body["status"] != "active" {
		t.Fatalf("ủy quyền: %d %v", r.status, r.body)
	}
	did := str(r.body, "delegation_id")
	k := f.probeKN(staff, staffCtx).KN
	if !k.Can(authz.PowerReopenPeriod) || k.Can(authz.PowerApprove) || !slices.Equal(k.Powers, []string{authz.PowerReopenPeriod}) {
		t.Fatalf("quyền nhận: %+v", k)
	}
	if r := f.withCtx(staff, http.MethodGet, "/v1/me/context", staffCtx, nil); !slices.Equal(toStrings(r.body["powers"]), []string{"mo_lai_ky"}) {
		t.Fatalf("GET /v1/me/context powers: %v", r.body)
	}
	// Người nhận không thu hồi được (không phải người giao); người giao thu hồi → mất quyền ngay.
	wantCode(t, f.withCtx(staff, http.MethodPost, "/v1/me/delegations/"+did+"/revoke", staffCtx, map[string]any{"reason": "tự thu hồi quyền của mình"}),
		http.StatusNotFound, "accounts.delegation_not_found", "người nhận thu hồi")
	if r := f.withCtx(lead, http.MethodPost, "/v1/me/delegations/"+did+"/revoke", leadCtx, map[string]any{"reason": "đã về cơ quan sớm"}); r.status != http.StatusOK ||
		r.body["status"] != "revoked" {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	if k := f.probeKN(staff, staffCtx).KN; k.Can(authz.PowerReopenPeriod) {
		t.Fatalf("sau thu hồi vẫn còn quyền: %+v", k)
	}
	wantCode(t, f.withCtx(lead, http.MethodPost, "/v1/me/delegations/"+did+"/revoke", leadCtx, map[string]any{"reason": "thu hồi lần hai nữa"}),
		http.StatusConflict, "accounts.delegation_state", "thu hồi lần hai")
	// Admin xem ủy quyền của tài khoản; ủy quyền chưa tới hiệu lực không cho quyền.
	r = deleg(lead, leadCtx, map[string]any{"grantee_ref": staffID.String(), "power": authz.PowerApprove, "valid_from": until(48 * time.Hour),
		"valid_to": until(72 * time.Hour), "reason": "nghỉ phép tuần sau"})
	if r.status != http.StatusCreated || r.body["status"] != "scheduled" {
		t.Fatalf("ủy quyền hẹn trước: %v", r.body)
	}
	if k := f.probeKN(staff, staffCtx).KN; k.Can(authz.PowerApprove) {
		t.Fatal("ủy quyền chưa tới hiệu lực đã có quyền")
	}
	r = f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+staffID.String()+"/delegations", nil)
	if len(r.body["items"].([]any)) != 2 {
		t.Fatalf("Admin xem ủy quyền: %v", r.body)
	}
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/delegations/"+str(r.body["items"].([]any)[0].(map[string]any), "delegation_id")+"/revoke",
		map[string]any{"reason": "Admin thu hồi theo đề nghị"}); r.status != http.StatusOK {
		t.Fatalf("Admin thu hồi: %d %v", r.status, r.body)
	}
}
