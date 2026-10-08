package handler_test

// W1.4 (R-MOD-08): vai của module chưa bật cho đơn vị không cấp được (tạo tài khoản, đổi vai).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fakeModules là trạng thái bật module theo tenant (party/port.ModuleEnablement).
type fakeModules map[uuid.UUID]map[string]bool

func (f fakeModules) DisabledModules(_ context.Context, tid uuid.UUID) (map[string]bool, error) {
	return f[tid], nil
}

func (f fakeModules) ModuleEnabled(_ context.Context, tid uuid.UUID, m string) (bool, error) {
	return !f[tid][m], nil
}

func TestRoleOfDisabledModuleNotGrantable(t *testing.T) {
	f := newFixture(t)
	mods := fakeModules{f.tenant: {"pf/geo": true, "pf/cooperative": true}}
	mod := accounts.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{Directory: f.dir, Scope: f.party, Orgs: f.party, Modules: mods})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	f.admin = srv
	nat := f.nationalAdmin()

	// plot_editor chỉ pf/cooperative khai (W3.3), geo_admin chỉ pf/geo → chặn; kn_officer do pf/party (lõi) khai → cấp được.
	r := nat.do(http.MethodPost, "/v1/accounts/users", person(f.team30292, "kn_officer", "plot_editor"))
	if r.status != http.StatusConflict || r.body["code"] != "accounts.role_module_not_enabled" || r.body["role"] != "plot_editor" {
		t.Fatalf("tạo với vai của module tắt: %d %v", r.status, r.body)
	}
	id := nat.create(person(f.team30292, "kn_officer"))

	// Đổi vai: thêm vai của module tắt → chặn; giữ vai cũ / thêm vai module bật → được.
	r = nat.do(http.MethodPost, "/v1/accounts/users/"+id.String()+"/assignment", map[string]any{"roles": []string{"kn_officer", "geo_admin"}})
	if r.status != http.StatusConflict || r.body["code"] != "accounts.role_module_not_enabled" {
		t.Fatalf("thêm geo_admin: %d %v", r.status, r.body)
	}
	r = nat.do(http.MethodPost, "/v1/accounts/users/"+id.String()+"/assignment", map[string]any{"roles": []string{"kn_officer", "mrv_viewer"}})
	if r.status != http.StatusOK && r.status != http.StatusCreated {
		t.Fatalf("thêm mrv_viewer (module bật): %d %v", r.status, r.body)
	}

	// Bật lại pf/geo, pf/cooperative cho tenant → cấp được.
	mods[f.tenant] = nil
	nat.create(person(f.team30292, "kn_officer", "plot_editor"))
}

// KN P1: vai trò KN (kn_tw/kn_tinh/kn_xa) là nghiệp vụ của pf/extension — tắt module thì không gán được.
func TestKNAssignmentModuleDisabled(t *testing.T) {
	f := newKNFixture(t)
	mods := fakeModules{f.tenant: {"pf/extension": true}}
	kmod := accounts.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{Directory: f.dir, Scope: f.party, Orgs: f.party, Departments: f.depts, Modules: mods})
	adm := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	kmod.RegisterAdmin(adm.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), kmod.Middleware(),
		idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api"))))
	f.admin2 = httptest.NewServer(adm)
	t.Cleanup(f.admin2.Close)

	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Trần Thị Bình", "email": "kn-mod@vi-du.vn",
		"phone": "0912349999", "assignments": []map[string]any{asg("kn_tinh", f.p91, []string{deptTT})}})
	if r.status != http.StatusConflict || r.body["code"] != "accounts.role_module_not_enabled" || r.body["role"] != "kn_tinh" {
		t.Fatalf("gán vai trò KN khi pf/extension tắt: %d %v", r.status, r.body)
	}
	mods[f.tenant] = nil
	f.createKN("Trần Thị Bình", asg("kn_tinh", f.p91, []string{deptTT}))
}
