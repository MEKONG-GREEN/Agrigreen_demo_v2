package kn77_test

// KNSPLIT-2-MENU-PARITY (#122): kiểm parity TRỰC TIẾP giữa cây menu KN do ứng dụng sở hữu (#76, nối ở gốc lắp ráp
// thật — party.Deps.Menus) và đường compatibility của pf/party (Menus nil, extension.NavigationPolicy) trên CÙNG
// PostgreSQL, cùng tenant, cùng ngữ cảnh phiên X-AG-Context của pf/accounts, cùng nguồn FormAvailability:
//
//   - HTTP GET /v1/menu và /v1/departments/{code}/modules: mọi đối tượng (kn_tw, kn_tinh, kn_xa, admin) × mọi đơn
//     vị × mọi phòng ban (6 bộ menu, alias generic của mã có khuôn riêng, chưa mở, ngưng, đã xóa) × đường vai realm
//     và đường phiên (traits Lãnh đạo/nhiệm vụ/chỉ xem) × các bộ module tắt;
//   - ContextNavigation (callback chọn ngữ cảnh) với 4 bộ traits.
//
// Khác biệt có chủ đích được kiểm TƯỜNG MINH (docs/dev/kn-menu-parity.md), không bị bỏ qua:
//   - Admin không gắn Nút thấy hai đơn vị có phòng ban cùng mã: khóa mục cha thêm ".<org_ref>" (#76, khóa anh em
//     duy nhất); nhãn, trạng thái, mục con, route giữ nguyên.
//   - org_ref tường minh khác đơn vị của phiên đang chọn: đường đăng ký trả 403 (#75 checkMenuUnit); web không gửi
//     org_ref cho /v1/menu.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// menuParity giữ hai bề mặt Application API trên cùng gốc lắp ráp: configured (st.App, Menus của KN) và
// compatibility (cùng mọi module, chỉ pf/party dựng với Menus nil).
type menuParity struct {
	s      *agScenario
	compat *party.Module
	url    string
	// counts: số ca khớp theo bề mặt/chế độ, và số ca hai đường cùng từ chối.
	counts map[string]int
}

func newMenuParity(t *testing.T, s *agScenario, clk clock.Clock) *menuParity {
	t.Helper()
	st := s.s
	policy, err := extension.NavigationPolicy()
	if err != nil {
		t.Fatal(err)
	}
	compat := party.New(st.PF(dbadmin.AppPlatform), clk, party.Deps{AdminUnits: st.Mods.MasterData.AdminUnits(), Vault: st.VaultClient, NavigationPolicy: policy})
	compat.UseSessionContext(st.Mods.Accounts.SessionContext())
	compat.UseFormAvailability(st.Mods.Reports.FormAvailability())
	mods := st.Mods
	mods.Party = compat
	srv := httptest.NewServer(assembly.NewRouter(assembly.Deps{Log: telemetry.NewLogger("compat", io.Discard), Verifier: st.Issuer.Verifier(),
		Ops: st.PF(dbadmin.AppPlatform), Modules: mods.List()}))
	t.Cleanup(srv.Close)
	return &menuParity{s: s, compat: compat, url: srv.URL, counts: map[string]int{}}
}

var orgKeySuffix = regexp.MustCompile(`"key":"(phong_ban\.[a-z0-9_]+)\.[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"`)

// same gọi path trên cả hai bề mặt với cùng token/ngữ cảnh: 200 → thân giống từng byte; lỗi → cùng mã HTTP và mã
// lỗi. dupKeys = ca Admin không gắn Nút có mã phòng ban trùng: khóa ".<org_ref>" là khác biệt duy nhất được phép.
func (p *menuParity) same(kind string, u *agUser, path string, dupKeys bool) agResp {
	p.s.t.Helper()
	got := u.do(p.s.s.App.URL, http.MethodGet, path, nil)
	want := u.do(p.url, http.MethodGet, path, nil)
	if got.status >= 500 || want.status >= 500 {
		p.s.t.Fatalf("%s %s: lỗi máy chủ configured=%d %s compat=%d %s", kind, path, got.status, got.raw, want.status, want.raw)
	}
	if got.status != want.status || (got.status != http.StatusOK && got.code() != want.code()) {
		p.s.t.Fatalf("%s %s: configured=%d %s compat=%d %s", kind, path, got.status, got.raw, want.status, want.raw)
	}
	if got.status != http.StatusOK {
		p.counts[kind+" denied"]++
		return got
	}
	raw := got.raw
	if dupKeys {
		raw = orgKeySuffix.ReplaceAll(raw, []byte(`"key":"$1"`))
		if bytes.Equal(raw, got.raw) {
			p.s.t.Fatalf("%s %s: mã trùng nhưng khóa không có hậu tố org_ref: %s", kind, path, got.raw)
		}
		p.counts[kind+" (khóa trùng có hậu tố)"]++
	} else if orgKeySuffix.Match(raw) {
		p.s.t.Fatalf("%s %s: hậu tố org_ref khi mã không trùng: %s", kind, path, got.raw)
	}
	if !bytes.Equal(raw, want.raw) {
		p.s.t.Fatalf("%s %s:\nconfigured %s\ncompat     %s", kind, path, got.raw, want.raw)
	}
	p.counts[kind]++
	return got
}

type parityUnit struct {
	ref   uuid.UUID
	codes []string // mã phòng ban (kể cả chưa mở/ngưng); đã xóa không có trong danh sách
	gone  []string // đã xóa: phải 404 ở cả hai đường
}

func TestKNSPLIT2MenuParityConfiguredMatchesCompatibility(t *testing.T) {
	clk := clock.NewFixed(t36)
	st := harness.NewAt(t, clk)
	s := &agScenario{t: t, s: st, clk: clk, tid: harness.NewTenant()}
	s.rootA = s.admin(authz.RolePlatformAdmin)
	p := newMenuParity(t, s, clk)
	adm := func(method, path string, body any, status int, what string) agResp {
		return s.want(s.rootA.adm(method, path, body), status, what)
	}

	// ---- Cây tổ chức + phòng ban: mọi bộ menu, alias generic, mọi trạng thái ----
	adm(http.MethodPost, "/v1/masterdata/seed", map[string]any{}, http.StatusOK, "seed masterdata")
	s.nat = str(obj(adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "PAR-NAT", "name": "TTKN Quốc gia"}, http.StatusCreated, "Nút TW").body, "org"), "ref")
	s.p91 = str(obj(adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_province", "code": "PAR-AG", "name": "TTKN An Giang", "parent_ref": s.nat,
		"admin_unit_code": agProv}, http.StatusCreated, "Nút tỉnh").body, "org"), "ref")
	adm(http.MethodPost, "/v1/orgs/"+s.p91+"/departments/seed", map[string]any{}, http.StatusOK, "5 phòng ban mặc định (Trồng trọt mở, còn lại chưa mở)")
	create := func(org, code, profile, status string) string {
		r := adm(http.MethodPost, "/v1/departments", map[string]any{"org_ref": org, "code": code, "name": "Phòng " + code, "sort_order": 50,
			"status": status, "menu_profile": profile}, http.StatusCreated, "phòng ban "+code)
		return str(r.body, "id")
	}
	prov := parityUnit{ref: uuid.MustParse(s.p91), codes: []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"}}
	for _, profile := range []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", partyport.MenuProfileGeneric} {
		code := "pb_" + profile
		create(s.p91, code, profile, partyport.DeptActive)
		prov.codes = append(prov.codes, code)
	}
	create(s.p91, "pb_ngung", "trong_trot", partyport.DeptSuspended)
	prov.codes = append(prov.codes, "pb_ngung")
	retired := create(s.p91, "pb_da_xoa", "ke_hoach_tai_chinh", partyport.DeptActive)
	adm(http.MethodPost, "/v1/departments/"+retired+"/status", map[string]any{"status": partyport.DeptSuspended, "reason": "Ngưng trước khi xóa"}, http.StatusCreated, "ngưng")
	if r := s.rootA.adm(http.MethodPost, "/v1/departments/"+retired+"/retire", map[string]any{"reason": "Kiểm parity phòng đã xóa"}); r.status != http.StatusOK && r.status != http.StatusCreated {
		t.Fatalf("xóa phòng ban: %d %s", r.status, r.raw)
	}
	prov.gone = []string{"pb_da_xoa"}
	// Đơn vị TW: alias bộ menu "generic" của mã có khuôn riêng (seed trước W11.06) + phòng mới generic + Trồng trọt.
	nat := parityUnit{ref: uuid.MustParse(s.nat)}
	for _, code := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "phong_moi"} {
		create(s.nat, code, partyport.MenuProfileGeneric, partyport.DeptActive)
		nat.codes = append(nat.codes, code)
	}
	create(s.nat, "trong_trot", "trong_trot", partyport.DeptActive)
	nat.codes = append(nat.codes, "trong_trot")
	units := []parityUnit{prov, nat}

	// ---- Người dùng: vai realm (không phiên) và phiên KN thật với traits ----
	realm := func(role string, org uuid.UUID) *agUser {
		id := uuid.New()
		return &agUser{s: s, id: id, tid: s.tid, token: st.Issuer.Sign(t, authztest.Token{Subject: id, TenantID: s.tid, Roles: []string{role}, OrgRef: org, TTL: 3 * time.Hour})}
	}
	realmUsers := []struct {
		user     *agUser
		audience string
		unit     parityUnit
	}{
		{realm(partyport.RoleKNNationalAdmin, nat.ref), partyport.AudienceTW, nat},
		{realm(partyport.RoleKNProvinceAdmin, prov.ref), partyport.AudienceTinh, prov},
		{realm(partyport.RoleKNOfficer, prov.ref), partyport.AudienceXa, prov},
	}
	active := []string{"trong_trot"}
	for _, profile := range []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", partyport.MenuProfileGeneric} {
		active = append(active, "pb_"+profile)
	}
	natActive := nat.codes
	mgr, mgrA := s.createKN("Lãnh đạo phòng", "parity-ld@vi-du.vn", "0912122001", asgKN(authz.KNTinh, s.p91, active, "truong_phong"))
	cv, cvA := s.createKN("Chuyên viên", "parity-cv@vi-du.vn", "0912122002", asgKN(authz.KNTinh, s.p91, active, "chuyen_vien"),
		asgKN(authz.KNXa, s.p91, active, "chuyen_vien", agC1), asgKN(authz.KNTW, s.nat, natActive, "chuyen_vien"))
	duty, dutyA := s.createKN("QTNV báo giá", "parity-bg@vi-du.vn", "0912122003", asgKN(authz.KNTinh, s.p91, active, "chuyen_vien"))
	adm(http.MethodPut, "/v1/accounts/users/"+duty.id.String()+"/assignments/"+dutyA[authz.KNTinh]+"/attributes",
		map[string]any{"duties": []string{authz.DutyPriceAdmin}, "reason": "Kiểm parity menu"}, http.StatusOK, "nhiệm vụ QTNV báo giá")
	leader, leaderA := s.createKN("Lãnh đạo chỉ xem", "parity-bgd@vi-du.vn", "0912122004", asgKN(authz.KNTinh, s.p91, active, "pho_giam_doc"))
	adm(http.MethodPut, "/v1/accounts/users/"+leader.id.String()+"/assignments/"+leaderA[authz.KNTinh]+"/attributes",
		map[string]any{"province_group": "ban_giam_doc", "reason": "Kiểm parity menu"}, http.StatusOK, "nhóm chỉ xem")
	adminID := st.Directory.Seed(accport.NewUser{Username: "parity-admin", Email: "parity-admin@vi-du.vn", FirstName: "Admin", LastName: "Parity", TenantID: s.tid}, true, authz.RolePlatformAdmin)
	adminAsg := adm(http.MethodPost, "/v1/accounts/users/"+adminID.String()+"/assignments", map[string]any{"role": authz.KNAdmin, "department_codes": []string{}, "commune_codes": []string{}},
		http.StatusCreated, "gán Admin KN")
	adminUser := &agUser{s: s, id: adminID, tid: s.tid, token: st.Issuer.Sign(t, authztest.Token{Subject: adminID, TenantID: s.tid, Roles: s.realm(adminID), MFA: true, TTL: 3 * time.Hour})}
	type session struct {
		user  *agUser
		asg   string
		codes []string
	}
	sessions := []session{{mgr, mgrA[authz.KNTinh], active}, {cv, cvA[authz.KNTinh], active}, {cv, cvA[authz.KNXa], active}, {cv, cvA[authz.KNTW], natActive},
		{duty, dutyA[authz.KNTinh], active}, {leader, leaderA[authz.KNTinh], active}}
	support := adm(http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"target_assignment_id": mgrA[authz.KNTinh], "department_code": "trong_trot",
		"reason": "Kiểm parity menu hỗ trợ"}, http.StatusCreated, "phiên hỗ trợ")
	supporter := *s.rootA
	supporter.ctx = str(support.body, "context_id")

	q := func(kv ...string) string {
		v := url.Values{}
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] != "" {
				v.Set(kv[i], kv[i+1])
			}
		}
		if len(v) == 0 {
			return ""
		}
		return "?" + v.Encode()
	}
	matrix := func() {
		t.Helper()
		// Quản trị nền tảng (không phiên): mọi đối tượng × đơn vị × phòng ban × hai bề mặt.
		for _, u := range units {
			org := u.ref.String()
			for _, aud := range []string{partyport.AudienceTW, partyport.AudienceTinh, partyport.AudienceXa} {
				p.same("menu/"+aud, s.rootA, "/v1/menu"+q("audience", aud, "org_ref", org), false) // phòng ban mở duy nhất hoặc 422
				for _, code := range append(slices.Clone(u.codes), u.gone...) {
					p.same("menu/"+aud, s.rootA, "/v1/menu"+q("audience", aud, "org_ref", org, "department", code), false)
					p.same("modules/"+aud, s.rootA, "/v1/departments/"+code+"/modules"+q("audience", aud, "org_ref", org), false)
				}
			}
			p.same("menu/admin", s.rootA, "/v1/menu"+q("audience", "admin", "org_ref", org), false)
			for _, code := range append(slices.Clone(u.codes), u.gone...) {
				p.same("modules/admin", s.rootA, "/v1/departments/"+code+"/modules"+q("audience", "admin", "org_ref", org), false)
			}
		}
		// Admin không gắn Nút: mọi đơn vị → mã trùng giữa TW và tỉnh (khác biệt có chủ đích về khóa).
		p.same("menu/admin", s.rootA, "/v1/menu", true)
		p.same("menu/admin", s.rootA, "/v1/menu"+q("audience", "admin"), true)
		// Vai realm KN gắn Nút (đường suy đơn vị từ token).
		for _, r := range realmUsers {
			p.same("menu/"+r.audience, r.user, "/v1/menu", false)
			for _, code := range append(slices.Clone(r.unit.codes), r.unit.gone...) {
				p.same("menu/"+r.audience, r.user, "/v1/menu"+q("department", code), false)
				p.same("modules/"+r.audience, r.user, "/v1/departments/"+code+"/modules", false)
			}
		}
		// Phiên KN thật (traits Lãnh đạo/nhiệm vụ/chỉ xem), phiên Admin, phiên hỗ trợ.
		for _, ss := range sessions {
			for _, code := range ss.codes {
				u := ss.user.as(ss.asg, code)
				p.same("session/menu", u, "/v1/menu", false)
				p.same("session/modules", u, "/v1/departments/"+code+"/modules", false)
			}
		}
		adminCtx := adminUser.as(str(obj(adminAsg.body, "assignment"), "assignment_id"), "")
		p.same("session/menu", adminCtx, "/v1/menu", true)
		for _, code := range prov.codes {
			p.same("session/modules", adminCtx, "/v1/departments/"+code+"/modules"+q("org_ref", s.p91), false)
		}
		p.same("session/menu", &supporter, "/v1/menu", false)
	}
	navigation := func() {
		t.Helper()
		configured, compat := st.Mods.Party.ContextNavigation(), p.compat.ContextNavigation()
		ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: s.tid, Roles: []string{authz.RolePlatformAdmin}, MFA: true}), s.tid)
		check := func(in partyport.NavigationRequest) {
			t.Helper()
			got, gerr := configured(ctx, in)
			want, werr := compat(ctx, in)
			if got != want || !sameProblem(gerr, werr) {
				t.Fatalf("ContextNavigation %s/%s/%s traits=%v/%v/%v: configured=%q %v compat=%q %v", in.Audience, in.UnitRef, in.Department.Code,
					in.FilterTraits, in.Manager, in.Duties, got, gerr, want, werr)
			}
			if gerr != nil {
				p.counts["navigation denied"]++
			} else {
				p.counts["navigation"]++
			}
		}
		check(partyport.NavigationRequest{Audience: partyport.AudienceAdmin})
		for _, u := range units {
			check(partyport.NavigationRequest{Audience: partyport.AudienceAdmin, UnitRef: u.ref})
			depts, err := st.Mods.Party.Departments().UnitDepartments(ctx, u.ref)
			if err != nil {
				t.Fatal(err)
			}
			if len(depts) != len(u.codes) {
				t.Fatalf("đơn vị %s: %d phòng ban, muốn %d", u.ref, len(depts), len(u.codes))
			}
			for _, d := range depts {
				for _, aud := range []string{partyport.AudienceTW, partyport.AudienceTinh, partyport.AudienceXa} {
					for _, tr := range []struct {
						filter, manager bool
						duties          []string
					}{{}, {filter: true}, {filter: true, manager: true}, {filter: true, duties: []string{authz.DutyPriceAdmin}}} {
						check(partyport.NavigationRequest{Audience: aud, UnitRef: u.ref, Department: d, FilterTraits: tr.filter, Manager: tr.manager, Duties: tr.duties})
					}
				}
			}
		}
	}

	// ---- Bộ module tắt: mỗi lượt đổi trạng thái rồi vượt TTL cache module của cả hai đường ----
	disabledSets := [][]string{nil, {"pf/extension"}, {"pf/hrm"}, {"pf/price"}, {"pf/geo"}, {"pf/masterdata", "pf/reports", "pf/sync", "pf/metering", "pf/mechanization"},
		{"pf/extension", "pf/hrm", "pf/price", "pf/geo", "pf/masterdata", "pf/reports", "pf/sync", "pf/metering", "pf/mechanization"}}
	setEnabled := func(modules []string, enabled bool) {
		for _, m := range modules {
			adm(http.MethodPost, "/v1/modules/"+m+"/enablement", map[string]any{"enabled": enabled, "reason": "Kiểm parity menu theo module"}, http.StatusCreated, "đổi module "+m)
		}
		clk.Advance(11 * time.Second)
	}
	for _, set := range disabledSets {
		setEnabled(set, false)
		matrix()
		navigation()
		setEnabled(set, true)
	}

	// ---- Bằng chứng nối thật + khác biệt #75 tường minh: org_ref khác đơn vị của phiên ----
	cross := mgr.as(mgrA[authz.KNTinh], "trong_trot")
	got := cross.do(st.App.URL, http.MethodGet, "/v1/menu"+q("org_ref", s.nat), nil)
	want := cross.do(p.url, http.MethodGet, "/v1/menu"+q("org_ref", s.nat), nil)
	if got.status != http.StatusForbidden || want.status != http.StatusOK {
		t.Fatalf("org_ref khác đơn vị phiên: configured=%d (muốn 403) compat=%d (200): %s / %s", got.status, want.status, got.raw, want.raw)
	}
	for _, kind := range []string{"menu/kn_tw", "menu/kn_tinh", "menu/kn_xa", "menu/admin", "modules/kn_tinh", "modules/admin", "session/menu", "session/modules", "navigation",
		"menu/admin (khóa trùng có hậu tố)", "menu/kn_tinh denied", "modules/admin denied", "navigation denied"} {
		if p.counts[kind] == 0 {
			t.Fatalf("thiếu độ phủ %q: %v", kind, p.counts)
		}
	}
	t.Logf("parity: %v", p.counts)
}

// sameProblem: hai lỗi cùng là nil, hoặc cùng mã problem (chi tiết có thể khác).
func sameProblem(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var pa, pb *problem.Problem
	if errors.As(a, &pa) && errors.As(b, &pb) {
		return pa.Status == pb.Status && pa.Code == pb.Code
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
