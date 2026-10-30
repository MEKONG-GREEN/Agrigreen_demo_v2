package knaccounts_test

// KNSPLIT-2-ROLE-CATALOG-KN (#95): catalog vai realm do KN khai báo (extension.RoleCatalog trên nền
// accport.NativeRoleCatalog) cho cùng hành vi HTTP quan sát được như bridge legacy, trên PostgreSQL thật (sổ
// pf_accounts, idempotency). Đặt ở tests/ vì mã KN chỉ được import port/ của accounts; ở đây cần dựng module accounts.

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// observeRoleCatalog chạy cùng kịch bản HTTP trên một module accounts và trả kết quả đã chuẩn hóa (không id, tập
// sắp xếp) để so trước/sau.
func observeRoleCatalog(t *testing.T, roles *accport.RoleCatalog) map[string]any {
	f := newFixture(t, fixtureDeps{Roles: roles})
	out := map[string]any{}
	nat, prov := f.as(f.national, accport.RoleAccountAdmin), f.as(f.p91, accport.RoleAccountAdminProvince)
	platform := f.as(f.national, "platform_admin")

	// 1. Catalog HTTP theo người gọi (kể cả vai KN — không phải quản trị tài khoản).
	callers := map[string]caller{"account_admin": nat, "account_admin_province": prov, "platform_admin": platform,
		"kn_national_admin": f.as(f.national, extension.RealmRoleKNNationalAdmin),
		"kn_province_admin": f.as(f.p91, extension.RealmRoleKNProvinceAdmin),
		"kn_officer":        f.as(f.team30292, extension.RealmRoleKNOfficer)}
	for name, c := range callers {
		r := c.do(http.MethodGet, "/v1/accounts/grants", nil)
		g := map[string]any{"status": r.status, "code": r.body["code"]}
		if r.status == http.StatusOK {
			g["grantable"], g["mfa_roles"] = sortedList(t, r.body["grantable"]), sortedList(t, r.body["mfa_roles"])
			kinds := map[string][]string{}
			for role, v := range r.body["role_org_kinds"].(map[string]any) {
				kinds[role] = sortedList(t, v)
			}
			g["role_org_kinds"] = kinds
		}
		out["grants/"+name] = g
	}

	// 2. Tạo tài khoản: vai KN + vai native mẫu × loại tổ chức × người cấp.
	orgs := map[string]func() string{"national": f.national.String, "p91": f.p91.String, "team30292": f.team30292.String,
		"coop30292": f.coop30292.String, "team31120": f.team31120.String}
	roleSet := append(slices.Clone(accport.KNOfficerRoles), "coop_manager")
	for cname, c := range map[string]caller{"account_admin": nat, "account_admin_province": prov} {
		for oname, org := range orgs {
			for _, role := range roleSet {
				out["create/"+cname+"/"+oname+"/"+role] = f.createObserved(c, org(), role)
			}
		}
	}

	// 3. Sửa lần gán: thêm vai KN vào tài khoản có sẵn, đổi tổ chức sai loại, quản trị tỉnh thao tác vai ngoài quyền.
	officer := f.mustCreate(nat, f.team30292.String(), extension.RealmRoleKNOfficer)
	coop := f.mustCreate(nat, f.coop30292.String(), "coop_manager")
	provAdmin := f.mustCreate(nat, f.p91.String(), extension.RealmRoleKNProvinceAdmin)
	steps := []struct {
		name string
		c    caller
		path string
		body map[string]any
	}{
		{"officer+kn_user", nat, officer, map[string]any{"roles": []string{"kn_officer", "kn_user"}}},
		{"coop+kn_officer", nat, coop, map[string]any{"roles": []string{"coop_manager", "kn_officer"}}},
		{"coop+kn_user", nat, coop, map[string]any{"roles": []string{"coop_manager", "kn_user"}}},
		{"coop-kn_user", nat, coop, map[string]any{"roles": []string{"coop_manager"}}},
		{"officer->coop-org", nat, officer, map[string]any{"org_ref": f.coop30292.String()}},
		{"prov:officer-roles", prov, officer, map[string]any{"roles": []string{"kn_officer"}}},
		{"prov:province-admin", prov, provAdmin, map[string]any{"roles": []string{"kn_province_admin"}}},
		{"prov:officer->national", prov, officer, map[string]any{"roles": []string{"kn_officer", "kn_national_admin"}}},
	}
	for _, s := range steps {
		r := s.c.do(http.MethodPost, "/v1/accounts/users/"+s.path+"/assignment", s.body)
		out["assign/"+s.name] = map[string]any{"status": r.status, "code": r.body["code"], "user": f.userState(nat, s.path)}
	}
	for name, c := range map[string]caller{"nat": nat, "prov": prov} {
		r := c.do(http.MethodGet, "/v1/accounts/users/"+provAdmin, nil)
		out["read-province-admin/"+name] = map[string]any{"status": r.status, "code": r.body["code"]}
	}

	// 4. Nhập CSV của quản trị tỉnh: lý do từ chối theo catalog vai.
	csv := "ho_ten,email,org_ref,vai_tro\n"
	for i, role := range append(roleSet, "kn_admin_tu_che") {
		csv += "Cán bộ " + role + "," + "cb" + string(rune('a'+i)) + "-" + f.tenant.String()[:8] + "@vi-du.vn," + f.team30292.String() + "," + role + "\n"
	}
	r := prov.raw(http.MethodPost, "/v1/accounts/users/import", "k-"+f.tenant.String(), "text/csv", []byte(csv))
	rows := []any{}
	if items, ok := r.body["rows"].([]any); ok {
		for _, it := range items {
			m := it.(map[string]any)
			rows = append(rows, map[string]any{"row": m["row"], "status": m["status"], "reason": m["reason"]})
		}
	}
	out["import"] = map[string]any{"status": r.status, "created": r.body["created"], "rejected": r.body["rejected"], "rows": rows}
	return out
}

func TestKNRoleCatalogHTTPParityWithLegacyOnPostgres(t *testing.T) {
	kn, err := extension.RoleCatalog(accport.NativeRoleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	before, after := observeRoleCatalog(t, nil), observeRoleCatalog(t, kn)
	b, _ := json.MarshalIndent(before, "", " ")
	a, _ := json.MarshalIndent(after, "", " ")
	if string(a) != string(b) {
		t.Fatalf("KN owner role catalog changed observable behaviour\nlegacy:\n%s\nkn:\n%s", b, a)
	}
	// Không vô nghĩa: các điểm chốt của chính sách hiện hành có mặt trong kết quả chung.
	created := after["create/account_admin/team30292/kn_officer"].(map[string]any)
	if created["status"] != http.StatusCreated || !slices.Contains(created["required_actions"].([]string), accport.ActionConfigureTOTP) ||
		created["mfa_required"] != true {
		t.Fatalf("kn_officer lost D24 TOTP enrollment: %v", created)
	}
	if r := after["create/account_admin_province/team30292/kn_province_admin"].(map[string]any); r["status"] != http.StatusForbidden {
		t.Fatalf("province admin granted kn_province_admin: %v", r)
	}
	if r := after["create/account_admin/coop30292/kn_officer"].(map[string]any); r["status"] != http.StatusUnprocessableEntity {
		t.Fatalf("kn_officer escaped its organization kind: %v", r)
	}
	if r := after["grants/kn_province_admin"].(map[string]any); r["status"] != http.StatusForbidden {
		t.Fatalf("KN realm role became an account administrator: %v", r)
	}
	if g := after["grants/account_admin_province"].(map[string]any); !slices.Equal(g["grantable"].([]string), []string{"coop_manager", "kn_officer"}) {
		t.Fatalf("province grantable changed: %v", g)
	}
	// Nhập CSV của quản trị tỉnh ở Tổ KN: chỉ kn_officer được tạo (coop_manager cần HTX, vai KN khác vượt quyền).
	if imp := after["import"].(map[string]any); imp["created"] != float64(1) || imp["rejected"] != float64(5) {
		t.Fatalf("CSV import outcome changed: %v", imp)
	}
}

// Instance có catalog KN không ảnh hưởng instance khác chạy cùng tiến trình (không trạng thái toàn cục).
func TestKNRoleCatalogInstanceIsolationOnPostgres(t *testing.T) {
	kn, err := extension.RoleCatalog(accport.NativeRoleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	withKN := newFixture(t, fixtureDeps{Roles: kn})
	nativeOnly := newFixture(t, fixtureDeps{Roles: accport.NativeRoleCatalog()})
	if r := nativeOnly.as(nativeOnly.national, accport.RoleAccountAdmin).do(http.MethodPost, "/v1/accounts/users",
		person(nativeOnly.team30292.String(), extension.RealmRoleKNOfficer)); r.status == http.StatusCreated {
		t.Fatalf("native-only instance accepted a KN role: %d %v", r.status, r.body)
	}
	id := withKN.mustCreate(withKN.as(withKN.national, accport.RoleAccountAdmin), withKN.team30292.String(), extension.RealmRoleKNOfficer)
	if id == "" {
		t.Fatal("KN instance rejected kn_officer")
	}
	g := nativeOnly.as(nativeOnly.national, accport.RoleAccountAdmin).do(http.MethodGet, "/v1/accounts/grants", nil)
	if g.status != http.StatusOK || slices.Contains(sortedList(t, g.body["grantable"]), extension.RealmRoleKNOfficer) {
		t.Fatalf("KN registration leaked into another instance: %d %v", g.status, g.body)
	}
}
