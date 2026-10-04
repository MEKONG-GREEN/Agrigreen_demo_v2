package handler_test

// W1.4 (R-MOD-08, pf 000088): bật module theo đơn vị — Admin API bật/tắt + Log, đọc của ngữ cảnh hiện
// tại, cổng route trả module_not_enabled, lõi luôn bật, menu lọc, cách ly tenant, sổ chỉ ghi thêm,
// idempotency.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func moduleItem(t *testing.T, r resp, name string) map[string]any {
	t.Helper()
	for _, it := range items(r) {
		if it["module"] == name {
			return it
		}
	}
	t.Fatalf("không có module %s: %v", name, r.body)
	return nil
}

// gateServer dựng một route giả của module qua cổng của pf/party (như gốc lắp ráp).
func (f *fixture) gateServer(module string) *httptest.Server {
	f.t.Helper()
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	g := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()))
	if gate := f.mod.ModuleGate(module); gate != nil {
		g = g.Group("", gate)
	}
	g.GET("/probe", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	srv := httptest.NewServer(eng)
	f.t.Cleanup(srv.Close)
	return srv
}

func (c caller) probe(srv *httptest.Server) resp {
	c.f.t.Helper()
	return c.call(srv.URL, http.MethodGet, "/v1/probe", "", "application/json", nil)
}

func TestModuleEnablement(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	other := f.as(ids.New(), ids.New(), port.RoleRegistryAdmin, authz.RolePlatformAdmin)
	officer := f.as(f.tenant, ids.New(), port.RoleKNOfficer)

	// Mặc định mọi module bật (không có dòng) — hành vi trước W1.4 giữ nguyên.
	r := officer.appDo(http.MethodGet, "/v1/me/modules", nil)
	wantStatus(t, r, http.StatusOK, "đọc module của ngữ cảnh")
	for _, it := range items(r) {
		if it["enabled"] != true {
			t.Fatalf("mặc định phải bật: %v", it)
		}
	}
	if m := moduleItem(t, r, "pf/party"); m["enable"] != "always" || m["kind"] != "kernel" {
		t.Fatalf("pf/party: %v", m)
	}
	metering := f.gateServer("pf/metering")
	wantStatus(t, root.probe(metering), http.StatusOK, "route module bật")

	// Lõi / module luôn bật: không tắt được, không có cổng.
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/party/enablement", map[string]any{"enabled": false, "reason": "Thử tắt module lõi"}),
		http.StatusConflict, "party.module_always_on", "tắt lõi")
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/mkg/hub/enablement", map[string]any{"enabled": false, "reason": "Thử tắt module mkg"}),
		http.StatusConflict, "party.module_always_on", "tắt mkg")
	if f.mod.ModuleGate("pf/party") != nil || f.mod.ModuleGate("pf/accounts") != nil {
		t.Fatal("module luôn bật không được có cổng")
	}
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/khong_co/enablement", map[string]any{"enabled": false, "reason": "Module không tồn tại"}),
		http.StatusNotFound, "party.module_not_found", "module lạ")
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", map[string]any{"enabled": false}),
		http.StatusUnprocessableEntity, "validation_failed", "tắt thiếu lý do")
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", map[string]any{"reason": "Thiếu trường enabled"}),
		http.StatusUnprocessableEntity, "validation_failed", "thiếu enabled")
	wantCode(t, officer.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", map[string]any{"enabled": false, "reason": "Cán bộ không có quyền"}),
		http.StatusForbidden, "forbidden", "không phải quản trị")
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", map[string]any{"enabled": true}),
		http.StatusConflict, "party.module_enablement_state", "bật module đang bật")

	// Tắt pf/metering (idempotent theo Idempotency-Key).
	k := key()
	body := map[string]any{"enabled": false, "reason": "Đơn vị chưa dùng Public API"}
	r = root.adminWithKey(http.MethodPost, "/v1/modules/pf/metering/enablement", k, body)
	wantStatus(t, r, http.StatusCreated, "tắt pf/metering")
	if r.body["enabled"] != false || r.body["reason"] != "Đơn vị chưa dùng Public API" {
		t.Fatalf("tắt: %v", r.body)
	}
	r = root.adminWithKey(http.MethodPost, "/v1/modules/pf/metering/enablement", k, body)
	if r.status != http.StatusCreated && r.status != http.StatusOK {
		t.Fatalf("gọi lặp cùng khóa: %d %v", r.status, r.body)
	}
	wantCode(t, root.adminWithKey(http.MethodPost, "/v1/modules/pf/metering/enablement", k, map[string]any{"enabled": true}),
		http.StatusUnprocessableEntity, "idempotency_key_reused", "cùng khóa khác nội dung")
	if n := f.countIn(f.tenant, "pf_party.module_enablement_event"); n != 1 {
		t.Fatalf("gọi lặp phải ghi một dòng sổ, có %d", n)
	}
	wantCode(t, root.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", body),
		http.StatusConflict, "party.module_enablement_state", "tắt module đang tắt")

	// Cổng route: tenant đã tắt → 403 module_not_enabled; tenant khác vẫn qua; module khác vẫn qua.
	r = officer.probe(metering)
	wantCode(t, r, http.StatusForbidden, "module_not_enabled", "route module đã tắt")
	if r.body["module"] != "pf/metering" {
		t.Fatalf("problem phải nêu module: %v", r.body)
	}
	wantStatus(t, other.probe(metering), http.StatusOK, "tenant khác")
	wantStatus(t, officer.probe(f.gateServer("pf/geo")), http.StatusOK, "module khác")

	// Đọc của ngữ cảnh + cách ly tenant.
	if m := moduleItem(t, officer.appDo(http.MethodGet, "/v1/me/modules", nil), "pf/metering"); m["enabled"] != false {
		t.Fatalf("tenant đã tắt: %v", m)
	}
	if m := moduleItem(t, other.appDo(http.MethodGet, "/v1/me/modules", nil), "pf/metering"); m["enabled"] != true {
		t.Fatalf("tenant khác không bị ảnh hưởng: %v", m)
	}
	if n := f.countIn(other.tid, "pf_party.module_enablement_current"); n != 0 {
		t.Fatalf("tenant khác thấy %d dòng", n)
	}
	r = root.adminDo(http.MethodGet, "/v1/modules", nil)
	wantStatus(t, r, http.StatusOK, "Admin liệt kê")
	if m := moduleItem(t, r, "pf/metering"); m["enabled"] != false || m["updated_by"] != root.actor.String() {
		t.Fatalf("Admin liệt kê: %v", m)
	}

	// Menu không hiện màn của module đã tắt; màn của module khác giữ nguyên.
	keys := menuKeys(root.appDo(http.MethodGet, "/v1/menu?audience=admin", nil))
	for _, gone := range []string{"admin.qt.cong_cu.su_dung_api", "admin.qt.cong_cu.bang_ke", "admin.qt.cong_cu.nhat_ky_xuat"} {
		if slices.Contains(keys, gone) {
			t.Fatalf("menu còn %s: %v", gone, keys)
		}
	}
	for _, kept := range []string{"admin.qt.cong_cu.tham_so", "admin.qt.cong_cu.khach_hang_api", "admin.qt.phong_ban"} {
		if !slices.Contains(keys, kept) {
			t.Fatalf("menu mất %s: %v", kept, keys)
		}
	}
	if keys := menuKeys(other.appDo(http.MethodGet, "/v1/menu?audience=admin", nil)); !slices.Contains(keys, "admin.qt.cong_cu.su_dung_api") {
		t.Fatalf("menu tenant khác: %v", keys)
	}

	// Bật lại: cổng mở ngay trong tiến trình (xóa đệm), Log đủ hai dòng.
	r = root.adminDo(http.MethodPost, "/v1/modules/pf/metering/enablement", map[string]any{"enabled": true})
	wantStatus(t, r, http.StatusCreated, "bật lại")
	wantStatus(t, officer.probe(metering), http.StatusOK, "route sau khi bật lại")
	es := logItems(t, root.adminDo(http.MethodGet, "/v1/modules/pf/metering/log", nil))
	if len(es) != 2 || es[0]["event"] != "disabled" || es[1]["event"] != "enabled" || es[0]["actor_ref"] != root.actor.String() {
		t.Fatalf("log: %v", es)
	}
	if o, n, ok := changeOf(es[0], "status"); !ok || o != "enabled" || n != "disabled" || es[0]["reason"] != "Đơn vị chưa dùng Public API" {
		t.Fatalf("log tắt: %v", es[0])
	}

	// Sổ chỉ ghi thêm; chuỗi băm liền.
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE pf_party.module_enablement_event SET reason = 'sửa lén'")
		return e
	})
	if err == nil {
		t.Fatal("UPDATE sổ bật module phải bị chặn")
	}
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		v, e := ledger.Verify(context.Background(), tx, "pf_party.module_enablement_event", f.tenant, "module:pf/metering")
		if e == nil && (!v.OK || v.RowsChecked != 2) {
			t.Fatalf("chuỗi băm: %+v", v)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

// Port cho pf/accounts: DisabledModules/ModuleEnabled theo tenant.
func TestModuleEnablementPort(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	en := f.mod.ModuleEnablement()
	ctx := context.Background()
	wantStatus(t, root.adminDo(http.MethodPost, "/v1/modules/pf/geo/enablement", map[string]any{"enabled": false, "reason": "Chưa triển khai bản đồ"}),
		http.StatusCreated, "tắt pf/geo")
	d, err := en.DisabledModules(ctx, f.tenant)
	if err != nil || !d["pf/geo"] || len(d) != 1 {
		t.Fatalf("DisabledModules: %v %v", d, err)
	}
	if on, err := en.ModuleEnabled(ctx, f.tenant, "pf/geo"); err != nil || on {
		t.Fatalf("pf/geo phải tắt: %v %v", on, err)
	}
	if on, _ := en.ModuleEnabled(ctx, f.tenant, "pf/party"); !on {
		t.Fatal("lõi luôn bật")
	}
	if on, _ := en.ModuleEnabled(ctx, ids.New(), "pf/geo"); !on {
		t.Fatal("tenant khác bật mặc định")
	}
}
