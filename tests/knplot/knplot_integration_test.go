package knplot_test

// KN-STANDALONE-5-TT-DATA (#271): thửa ruộng KN (Trồng trọt FN-XA-03.05) ở kho KN pf_extension trên PostgreSQL thật qua
// module pf/extension (vai r_extension). Đặt ở tests/ vì phải tạo thửa nguồn ở bảng HTX (pf_coop.plot) bằng SQL quản trị
// để kiểm lần chép một lần + đếm trước/sau + quay lui, và đếm bảng HTX — mã KN không được nhắc bảng HTX. Ngữ cảnh phiên KN
// đi qua middleware X-AG-Context + projection phiên của KN (như tests/knrecords). Cổng bật module của pf/party gắn như
// gốc lắp ráp để kiểm FN chạy khi tenant TẮT pf/cooperative và pf/mechanization.

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const dept = "trong_trot"

var t0 = time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	mod    *extension.Module
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	team   uuid.UUID // Nút tổ khuyến nông của Cấp Xã (phạm vi dữ liệu)
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.mod = extension.New(pool, clock.NewFixed(t0), extension.Deps{})
	f.mod.UseKNPlotVault(clienttest.New())
	f.mod.UseKNRecordsVault(clienttest.New())
	pty := party.New(pool, clock.NewFixed(t0))
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	g := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops))
	// Như gốc lắp ráp (assembly.Modules.List): route của module per_tenant đi qua cổng bật module của pf/party.
	f.mod.RegisterApp(g.Group("", pty.ModuleGate("pf/extension")))
	// Mốc đối chứng: route của module HTX bị chặn khi tenant tắt module (403 module_not_enabled).
	for _, m := range []string{"pf/cooperative", "pf/mechanization"} {
		if gate := pty.ModuleGate(m); gate != nil {
			g.GET("/probe/"+strings.TrimPrefix(m, "pf/"), gate, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		}
	}
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	f.team = ids.New()
	f.admin(`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
	  VALUES ($1, $2, 'extension_team', $3, 'Tổ khuyến nông 31117', '31117', true, $4, 1)`, f.tenant, f.team, "T"+f.team.String()[24:], ids.New())
	return f
}

func (f *fix) admin(sql string, args ...any) {
	f.t.Helper()
	if _, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("admin: %v\n%s", err, sql)
	}
}

func (f *fix) scalar(sql string, args ...any) string {
	f.t.Helper()
	var s *string
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		f.t.Fatalf("admin: %v\n%s", err, sql)
	}
	if s == nil {
		return ""
	}
	return *s
}

// script chạy một tệp SQL (migration chép, kịch bản quay lui) trong một giao dịch bằng vai quản trị; role khác rỗng →
// SET LOCAL ROLE (migration chạy đúng vai ag_migrator NOBYPASSRLS như khi triển khai).
func (f *fix) script(path, role string) {
	f.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if role != "" {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+role); err != nil {
			f.t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		f.t.Fatalf("%s: %v", path, err)
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

func repoFile(t *testing.T, parts ...string) string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(self), "..", ".."}, parts...)...)
}

// disable tắt module cho tenant (bảng chiếu cấu hình module của pf/party).
func (f *fix) disable(module string) {
	f.admin(`INSERT INTO pf_party.module_enablement_current (tenant_id, module, status, reason, last_event_id, last_seq, updated_by, updated_at)
	  VALUES ($1, $2, 'disabled', 'Tenant KN chạy độc lập (#271)', $3, 1, $4, now())`, f.tenant, module, ids.New(), ids.New())
}

// coopRecord ghi một hồ sơ KN loại HTX (sổ kn_producer) trực tiếp — như dữ liệu KN đã tự nhập.
func (f *fix) coopRecord(commune, name string, htx *uuid.UUID) uuid.UUID {
	ref := ids.New()
	f.admin(`INSERT INTO pf_extension.kn_producer (id, tenant_id, stream_key, producer_ref, kind, event, commune_code, name, person_ref,
	    htx_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $8, $3, 'cooperative', 'created', $4, $5, $6, $7, now(), $6, $9, 'test')`,
		ids.New(), f.tenant, ref, commune, name, ids.New(), htx, "kn_producer:"+ref.String(), "seed:"+ref.String())
	return ref
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		f.t.Fatal(err)
	}
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
		if raw == "" {
			c.Next()
			return
		}
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(raw)
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			f.t.Errorf("owner session projection: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
	kc    *authz.KNContext
	actor uuid.UUID
}

func (f *fix) user(tid uuid.UUID, role, department string, units ...string) user {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: department}
	switch role {
	case authz.KNXa:
		kc.Communes, kc.Scope, kc.OrgRef = units, authz.Scope{Units: units}, f.team
	case authz.KNTW:
		kc.Scope = authz.Scope{All: true}
	case authz.KNAdmin:
		kc.Scope, kc.Department = authz.Scope{All: true}, ""
	default:
		kc.Scope = authz.Scope{Units: units}
	}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID, kc: kc, actor: actor}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
	hdr    http.Header
}

func (u user) do(method, path, key string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if key == "" && method == http.MethodPost {
		key = "k-" + ids.New().String()
	}
	if key != "" {
		r.Header.Set(idempotency.Header, key)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: res.StatusCode, raw: raw, body: m, hdr: res.Header}
}

func (u user) get(p string) resp         { return u.do(http.MethodGet, p, "", nil) }
func (u user) post(p string, b any) resp { return u.do(http.MethodPost, p, "", b) }

// ctx là ngữ cảnh có Principal + phiên KN của người dùng (gọi Source/Sink đồng bộ như pf/sync).
func (u user) ctx() context.Context {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		u.f.t.Fatal(err)
	}
	cp := *u.kc
	p := authz.Principal{TenantID: u.f.tenantOf(u), ActorRef: u.actor, Roles: []string{"kn_user"}, KN: &cp}
	selected, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		u.f.t.Fatal(err)
	}
	p.SelectedSession = &selected
	return authz.WithPrincipal(context.Background(), p)
}

func (f *fix) tenantOf(user) uuid.UUID { return f.tenant }

func str(m any, path ...string) string {
	for _, k := range path {
		mm, _ := m.(map[string]any)
		m = mm[k]
	}
	s, _ := m.(string)
	return s
}

func list(m map[string]any, k string) []map[string]any {
	l, _ := m[k].([]any)
	out := []map[string]any{}
	for _, x := range l {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

func want(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
}

const baseLat, baseLon = 10.05, 105.6

// rect dựng Polygon chữ nhật: góc tây nam cách gốc (east, north) mét, rộng w, cao h (m).
func rect(east, north, w, h float64) map[string]any {
	dLat := 1 / 111320.0
	dLon := 1 / (111320.0 * math.Cos(baseLat*math.Pi/180))
	x0, y0 := baseLon+east*dLon, baseLat+north*dLat
	x1, y1 := x0+w*dLon, y0+h*dLat
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0, y0}}}}
}

func body(commune string, geom map[string]any) map[string]any {
	return map[string]any{"commune_code": commune, "lot_name": "Lô bờ kênh", "farmer_name": "Nguyễn Văn An", "note": "Lúa OM18",
		"declared_area_ha": "1.0", "shape": "square", "geometry": geom}
}

// denyCP là điểm kiểm soát xuất giả luôn báo vượt hạn mức (A5: xuất nội bộ vẫn đi qua và không bị chặn).
type denyCP struct {
	mu  sync.Mutex
	got []meter.Egress
}

func (d *denyCP) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, e)
	return meter.Decision{Allowed: false, Reason: "quota_exceeded", Mode: meter.ModeEnforce}, nil
}

// htxRows đếm dòng của tenant ở các bảng thửa/máy của module HTX — luồng KN không được ghi.
func (f *fix) htxRows() string {
	return f.scalar(`SELECT (SELECT count(*) FROM pf_coop.plot WHERE tenant_id = $1) || '/' ||
	  (SELECT count(*) FROM pf_coop.plot_boundary WHERE tenant_id = $1) || '/' || (SELECT count(*) FROM pf_coop.plot_event WHERE tenant_id = $1)`, f.tenant)
}

// FN-XA-03.05 chạy trọn khi tenant tắt pf/cooperative và pf/mechanization: vẽ, danh sách, chi tiết, sửa thuộc tính
// (gán HTX là hồ sơ KN), sửa ranh giới, Log, xuất tệp; không ghi bảng HTX.
func TestFN_XA_03_05_StandaloneWithHTXModulesDisabled(t *testing.T) {
	f := newFix(t)
	f.disable("pf/cooperative")
	f.disable("pf/mechanization")
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	for _, m := range []string{"cooperative", "mechanization"} {
		want(t, xa.get("/v1/probe/"+m), http.StatusForbidden, "module_not_enabled", "đối chứng: module HTX "+m+" đã tắt")
	}
	htxBefore := f.htxRows()
	coop := f.coopRecord("31117", "HTX Bình Hòa", nil)
	r := xa.post("/v1/kn/plots", body("31117", rect(0, 0, 100, 100)))
	want(t, r, http.StatusCreated, "", "vẽ lô mới")
	ref := str(r.body, "ref")
	if str(r.body, "lot_name") != "Lô bờ kênh" || str(r.body, "shape") != "square" || str(r.body, "department_code") != dept ||
		str(r.body, "status") != "draft" || !strings.HasPrefix(str(r.body, "code"), "KT") {
		t.Fatalf("thuộc tính: %s", r.raw)
	}
	if str(r.body, "farmer", "label") == "" || strings.Contains(string(r.raw), "Nguyễn Văn An") {
		t.Fatalf("tên nông dân phải được che: %s", r.raw)
	}
	if a := decimal.RequireFromString(str(r.body, "area_ha")); a.Sub(decimal.NewFromInt(1)).Abs().GreaterThan(decimal.RequireFromString("0.01")) {
		t.Fatalf("diện tích tự tính ≈ 1 ha: %s", r.raw)
	}
	if str(r.body, "area_deviation_pct") == "" {
		t.Fatalf("đối chiếu diện tích khai báo: %s", r.raw)
	}
	// Idempotency: cùng khóa → cùng thửa.
	k := "same-" + ids.New().String()
	a1 := xa.do(http.MethodPost, "/v1/kn/plots", k, body("31117", rect(300, 0, 50, 50)))
	a2 := xa.do(http.MethodPost, "/v1/kn/plots", k, body("31117", rect(300, 0, 50, 50)))
	if a1.status != http.StatusCreated || a2.status/100 != 2 || str(a1.body, "ref") != str(a2.body, "ref") {
		t.Fatalf("POST lặp: %d %d %s", a1.status, a2.status, a2.raw)
	}
	lst := xa.get("/v1/kn/plots?commune=31117")
	want(t, lst, http.StatusOK, "", "danh sách")
	if items := list(lst.body, "items"); len(items) != 2 || items[0]["geometry"] != nil {
		t.Fatalf("danh sách (không hình): %s", lst.raw)
	}
	g := xa.get("/v1/kn/plots/" + ref)
	want(t, g, http.StatusOK, "", "chi tiết")
	if str(g.body, "geometry", "type") != "MultiPolygon" {
		t.Fatalf("chi tiết kèm hình: %s", g.raw)
	}
	// Gán HTX = hồ sơ KN loại HTX; lọc danh sách theo hồ sơ.
	u := xa.do(http.MethodPut, "/v1/kn/plots/"+ref, "", map[string]any{"note": "Lúa ST25", "cooperative_ref": coop.String(), "reason": "Đổi giống theo vụ mới"})
	want(t, u, http.StatusOK, "", "sửa thuộc tính")
	if str(u.body, "note") != "Lúa ST25" || str(u.body, "cooperative_ref") != coop.String() || str(u.body, "status") != "active" {
		t.Fatalf("sau sửa: %s", u.raw)
	}
	if items := list(xa.get("/v1/kn/plots?cooperative_ref="+coop.String()).body, "items"); len(items) != 1 || str(items[0], "ref") != ref {
		t.Fatalf("lọc theo hồ sơ HTX: %v", items)
	}
	want(t, xa.do(http.MethodPut, "/v1/kn/plots/"+ref, "", map[string]any{"cooperative_ref": ids.New().String()}), http.StatusUnprocessableEntity,
		"validation_failed", "HTX không phải hồ sơ KN")
	// Sửa ranh giới: phiên bản mới; sửa trên phiên bản cũ → 409.
	c := xa.post("/v1/kn/plots/"+ref+"/boundary", map[string]any{"supersedes_boundary_id": str(g.body, "boundary_id"),
		"geometry": rect(0, 0, 90, 100), "shape": "polygon", "reason": "Đo lại ranh giới ngoài đồng"})
	want(t, c, http.StatusCreated, "", "sửa ranh giới")
	if str(c.body, "shape") != "polygon" || str(c.body, "boundary_id") == str(g.body, "boundary_id") || str(c.body, "note") != "Lúa ST25" {
		t.Fatalf("ranh giới mới: %s", c.raw)
	}
	want(t, xa.post("/v1/kn/plots/"+ref+"/boundary", map[string]any{"supersedes_boundary_id": str(g.body, "boundary_id"),
		"geometry": rect(0, 0, 80, 100), "shape": "polygon", "reason": "Sửa trên phiên bản cũ đã bị thay"}), http.StatusConflict, "geo.version_conflict", "phiên bản cũ")
	want(t, xa.post("/v1/kn/plots/"+ref+"/boundary", map[string]any{"supersedes_boundary_id": str(c.body, "boundary_id"),
		"geometry": rect(0, 0, 80, 100), "reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed", "lý do < 10 ký tự")
	lg := xa.get("/v1/kn/plots/" + ref + "/log")
	want(t, lg, http.StatusOK, "", "log")
	events := map[string]map[string]any{}
	for _, it := range list(lg.body, "items") {
		events[str(it, "event")] = it
	}
	for _, e := range []string{"created", "attributes_changed", "boundary_corrected"} {
		if events[e] == nil {
			t.Fatalf("log thiếu %s: %s", e, lg.raw)
		}
	}
	if items := list(lg.body, "items"); str(items[0], "event") != "boundary_corrected" {
		t.Fatalf("log mới nhất trước: %s", lg.raw)
	}
	fields := map[string]map[string]any{}
	for _, ch := range list(events["attributes_changed"], "changes") {
		fields[str(ch, "field")] = ch
	}
	if len(fields) != 2 || str(fields["note"], "old") != "Lúa OM18" || str(fields["note"], "new") != "Lúa ST25" || fields["cooperative_ref"] == nil {
		t.Fatalf("log sửa thuộc tính: %v", fields)
	}
	if str(events["attributes_changed"], "reason") != "Đổi giống theo vụ mới" || str(events["boundary_corrected"], "reason") != "Đo lại ranh giới ngoài đồng" {
		t.Fatalf("log thiếu lý do: %s", lg.raw)
	}
	if strings.Contains(string(lg.raw), "Nguyễn Văn An") {
		t.Fatal("log không chứa tên nông dân")
	}
	// Xuất tệp của xã: qua điểm kiểm soát xuất (nội bộ, không bị chặn).
	cp := &denyCP{}
	f.mod.UseEgress(cp, meter.ModeEnforce, nil)
	ex := xa.get("/v1/kn/plots/export?commune=31117&format=geojson")
	if ex.status != http.StatusOK || !strings.Contains(string(ex.raw), ref) || !strings.Contains(ex.hdr.Get("Content-Disposition"), ".geojson") {
		t.Fatalf("xuất GeoJSON: %d %s", ex.status, ex.raw)
	}
	if len(cp.got) != 1 || !cp.got[0].Internal || cp.got[0].Product != "kn_plot_file" || cp.got[0].Records != 2 {
		t.Fatalf("A5: tải tệp thửa qua checkpoint nội bộ: %+v", cp.got)
	}
	km := xa.get("/v1/kn/plots/export?commune=31117&format=kml")
	if km.status != http.StatusOK || !strings.Contains(string(km.raw), "<Placemark>") || !strings.Contains(string(km.raw), "<Polygon>") {
		t.Fatalf("xuất KML: %d %s", km.status, km.raw)
	}
	if after := f.htxRows(); after != htxBefore {
		t.Fatalf("luồng KN ghi bảng HTX: %s → %s", htxBefore, after)
	}
}

func TestFN_XA_03_05_OverlapAndGeometry(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	want(t, xa.post("/v1/kn/plots", body("31117", rect(0, 0, 100, 100))), http.StatusCreated, "", "lô 1")
	ov := xa.post("/v1/kn/plots", body("31117", rect(50, 0, 100, 100)))
	want(t, ov, http.StatusConflict, "geo.plot_overlap", "chồng lấn")
	if ols, _ := ov.body["overlaps"].([]any); len(ols) != 1 || str(ols[0], "geometry", "type") == "" || str(ols[0], "area_ha") == "" {
		t.Fatalf("thông tin chồng lấn: %s", ov.raw)
	}
	// Chung cạnh không phải chồng lấn.
	want(t, xa.post("/v1/kn/plots", body("31117", rect(100, 0, 50, 50))), http.StatusCreated, "", "chung cạnh")
	two := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.6, 10.05}, {105.601, 10.05}, {105.6, 10.05}}}}
	want(t, xa.post("/v1/kn/plots", body("31117", two)), http.StatusUnprocessableEntity, "geo.too_few_points", "2 điểm")
	tri := rect(600, 0, 50, 50)
	tri["coordinates"] = [][][]float64{tri["coordinates"].([][][]float64)[0][:3]}
	want(t, xa.post("/v1/kn/plots", body("31117", tri)), http.StatusCreated, "", "tam giác 3 điểm")
	col := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.6, 10.05}, {105.601, 10.05}, {105.602, 10.05}}}}
	want(t, xa.post("/v1/kn/plots", body("31117", col)), http.StatusUnprocessableEntity, "geo.too_few_points", "3 điểm thẳng hàng")
	line := map[string]any{"type": "LineString", "coordinates": [][]float64{{105.6, 10.05}, {105.601, 10.05}}}
	want(t, xa.post("/v1/kn/plots", body("31117", line)), http.StatusUnprocessableEntity, "geo.invalid_geometry", "không phải vùng")
	bow := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{105.7, 10.1}, {105.701, 10.101}, {105.701, 10.1}, {105.7, 10.101}, {105.7, 10.1}}}}
	want(t, xa.post("/v1/kn/plots", body("31117", bow)), http.StatusUnprocessableEntity, "geo.invalid_geometry", "tự cắt")
	utm := map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{580000, 1110000}, {580100, 1110000}, {580100, 1110100}, {580000, 1110000}}}}
	want(t, xa.post("/v1/kn/plots", body("31117", utm)), http.StatusUnprocessableEntity, "geo.crs_unsupported", "toạ độ phẳng")
	for field, v := range map[string]any{"note": "Gọi 0912345678", "shape": "triangle", "declared_area_ha": "-1"} {
		b := body("31117", rect(400, 0, 50, 50))
		b[field] = v
		want(t, xa.post("/v1/kn/plots", b), http.StatusUnprocessableEntity, "validation_failed", field)
	}
	for _, field := range []string{"lot_name", "farmer_name", "commune_code"} {
		b := body("31117", rect(400, 0, 50, 50))
		delete(b, field)
		want(t, xa.post("/v1/kn/plots", b), http.StatusUnprocessableEntity, "validation_failed", "thiếu "+field)
	}
	for _, s := range []string{"1e100000", "1e-100000", strings.Repeat("9", 400)} {
		b := body("31117", rect(400, 0, 50, 50))
		b["declared_area_ha"] = json.RawMessage(s)
		if r := xa.post("/v1/kn/plots", b); r.status/100 == 2 || r.status >= 500 {
			t.Fatalf("declared_area_ha %q phải bị chặn 4xx: %d %s", s[:min(len(s), 12)], r.status, r.raw)
		}
	}
	want(t, xa.do(http.MethodPost, "/v1/kn/plots", "", nil), http.StatusBadRequest, "", "thiếu thân")
}

func TestFN_XA_03_05_RolesScopeTenantDepartment(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	r := xa.post("/v1/kn/plots", body("31117", rect(0, 0, 100, 100)))
	want(t, r, http.StatusCreated, "", "lô")
	ref := str(r.body, "ref")
	want(t, xa.post("/v1/kn/plots", body("31120", rect(500, 0, 50, 50))), http.StatusForbidden, "geo.out_of_scope", "xã ngoài phạm vi")
	want(t, xa.get("/v1/kn/plots?commune=31120"), http.StatusForbidden, "geo.out_of_scope", "lọc xã ngoài phạm vi")
	tinh := f.user(f.tenant, authz.KNTinh, dept, "31117", "31120")
	want(t, tinh.get("/v1/kn/plots/"+ref), http.StatusOK, "", "Cấp Tỉnh xem")
	want(t, tinh.post("/v1/kn/plots", body("31117", rect(500, 0, 50, 50))), http.StatusForbidden, "forbidden", "Cấp Tỉnh chỉ xem")
	tw := f.user(f.tenant, authz.KNTW, dept)
	want(t, tw.get("/v1/kn/plots/"+ref), http.StatusOK, "", "Cấp Quốc gia xem")
	want(t, tw.do(http.MethodPut, "/v1/kn/plots/"+ref, "", map[string]any{"note": "x"}), http.StatusForbidden, "geo.read_only", "Cấp Quốc gia chỉ xem")
	other := f.user(f.tenant, authz.KNXa, "chan_nuoi", "31117")
	want(t, other.get("/v1/kn/plots/"+ref), http.StatusNotFound, "geo.plot_not_found", "phòng ban khác")
	if items := list(other.get("/v1/kn/plots").body, "items"); len(items) != 0 {
		t.Fatalf("phòng ban khác không thấy thửa: %v", items)
	}
	stranger := f.user(ids.New(), authz.KNXa, dept, "31117")
	want(t, stranger.get("/v1/kn/plots/"+ref), http.StatusNotFound, "geo.plot_not_found", "tenant khác")
	adm := f.user(f.tenant, authz.KNAdmin, "")
	want(t, adm.do(http.MethodPost, "/v1/kn/plots?department=chan_nuoi", "", body("31117", rect(700, 0, 50, 50))), http.StatusCreated, "", "Admin ghi phòng ban khác")
	if items := list(adm.get("/v1/kn/plots").body, "items"); len(items) != 2 {
		t.Fatalf("Admin thấy mọi phòng ban: %v", items)
	}
}

// Đồng bộ ngoại tuyến của field-app: chiều lên kn_plot_boundary (create/correct, idempotent, chồng lấn → rejected),
// chiều xuống kn_plot theo phạm vi.
func TestFN_XA_03_05_OfflineSync(t *testing.T) {
	f := newFix(t)
	f.disable("pf/cooperative")
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	sink, src := f.mod.KNPlotSyncSink(), f.mod.KNPlotSyncSource()
	plot, dev := ids.New(), ids.New()
	payload := func(geom map[string]any) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"plot_ref": plot.String(), "commune_code": "31117", "geometry": geom, "lot_name": "Lô ngoài đồng",
			"farmer_name": "Lê Văn Bình", "shape": "polygon", "source": "kn_field_app", "confirm_overlap": true})
		return b
	}
	rec := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":1", Entity: "kn_plot_boundary", Op: syncport.OpCreate,
		EventTime: t0.Add(-time.Hour), DeviceID: dev, Payload: payload(rect(0, 0, 100, 100))}
	if res := sink.Accept(xa.ctx(), authz.Scope{}, rec); res.Status != syncport.StatusAccepted {
		t.Fatalf("tạo ngoại tuyến: %+v", res)
	}
	if res := sink.Accept(xa.ctx(), authz.Scope{}, rec); res.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lặp: %+v", res)
	}
	ch, last, err := src.Changes(xa.ctx(), authz.Scope{}, "kn_plot", 0, 100)
	if err != nil || len(ch) != 1 || ch[0].ID != plot || last == 0 {
		t.Fatalf("chiều xuống: %v %d %v", err, last, ch)
	}
	var d map[string]any
	_ = json.Unmarshal(ch[0].Data, &d)
	if str(d, "geometry", "type") != "MultiPolygon" || str(d, "lot_name") != "Lô ngoài đồng" || strings.Contains(string(ch[0].Data), "Lê Văn Bình") {
		t.Fatalf("dữ liệu xuống máy: %s", ch[0].Data)
	}
	other := ids.New()
	ov := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":2", Entity: "kn_plot_boundary", Op: syncport.OpCreate, DeviceID: dev,
		Payload: json.RawMessage(strings.Replace(string(payload(rect(50, 0, 100, 100))), plot.String(), other.String(), 1))}
	if res := sink.Accept(xa.ctx(), authz.Scope{}, ov); res.Status != syncport.StatusRejected || res.Code != "geo.plot_overlap" {
		t.Fatalf("chồng lấn ngoại tuyến bị từ chối: %+v", res)
	}
	reason := "Đo lại bằng GPS ngoài đồng"
	stale := ids.New()
	cor := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":3", Entity: "kn_plot_boundary", Op: syncport.OpCorrect, DeviceID: dev,
		SupersedesID: &stale, CorrectionReason: &reason, Payload: payload(rect(0, 0, 90, 100))}
	if res := sink.Accept(xa.ctx(), authz.Scope{}, cor); res.Status != syncport.StatusRejected || res.Code != "geo.version_conflict" {
		t.Fatalf("sửa trên phiên bản cũ: %+v", res)
	}
	cur := ch[0]
	var cd map[string]any
	_ = json.Unmarshal(cur.Data, &cd)
	bid := uuid.MustParse(str(cd, "boundary_id"))
	cor.ID, cor.IdemKey, cor.SupersedesID = ids.New(), "dev:"+dev.String()+":4", &bid
	if res := sink.Accept(xa.ctx(), authz.Scope{}, cor); res.Status != syncport.StatusAccepted {
		t.Fatalf("sửa ranh giới ngoại tuyến: %+v", res)
	}
	if ch2, _, _ := src.Changes(xa.ctx(), authz.Scope{}, "kn_plot", last, 100); len(ch2) != 1 || ch2[0].Version <= cur.Version {
		t.Fatalf("chiều xuống sau sửa: %v", ch2)
	}
	far := f.user(f.tenant, authz.KNXa, dept, "31120")
	if ch3, _, _ := src.Changes(far.ctx(), authz.Scope{}, "kn_plot", 0, 100); len(ch3) != 0 {
		t.Fatalf("xã khác không nhận thửa: %v", ch3)
	}
}

// Chép một lần từ pf_coop.plot (module HTX) sang kho KN: số thửa và tổng diện tích theo xã khớp (count.sql rỗng),
// HTX nền tảng → hồ sơ KN liên kết, thửa ngừng dùng không chép, ref giữ nguyên (đọc được qua API KN); quay lui
// (rollback.sql) gỡ đúng phần đã chép, chạy lại migration chép khôi phục; bảng HTX không đổi.
func TestKN271_CopyCountRollback(t *testing.T) {
	f := newFix(t)
	htx := ids.New()
	linked := f.coopRecord("31117", "HTX Liên Kết", &htx)
	seed := func(commune string, coop *uuid.UUID, status string, east float64) uuid.UUID {
		ref := ids.New()
		g, _ := json.Marshal(rect(east, 2000, 100, 100))
		bid := ids.New()
		// Phiên bản ranh giới đầu tiên ở sổ HTX (như mọi thửa thật) — đầu chuỗi băm cho biết tenant có thửa.
		f.admin(`INSERT INTO pf_coop.plot_boundary (id, tenant_id, stream_key, plot_ref, geom, area_ha, point_count, source, commune_code,
		    cooperative_ref, became_current, event_time, actor_ref, idem_key, rule_version)
		  SELECT $1::uuid, $2::uuid, $3::text, $4::uuid, g, (ST_Area(g::geography) / 10000)::numeric(14,4), 5, 'kn_web', $5::text, $6::uuid,
		    true, now(), $7::uuid, $8::text, 'test'
		  FROM (SELECT ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($9::text), 4326)) AS g) s`,
			bid, f.tenant, "plot:"+ref.String(), ref, commune, coop, ids.New(), "seed:"+ref.String(), string(g))
		f.admin(`INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, status, current_version, current_boundary_id,
		    geom, area_ha, source, created_by, lot_name, department_code)
		  SELECT $1::uuid, $2::uuid, $9::text, $3::text, $4::uuid, $5::text, 1, $6::uuid, g,
		    (ST_Area(g::geography) / 10000)::numeric(14,4), 'kn_web', $7::uuid, 'Lô cũ', NULL
		  FROM (SELECT ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($8::text), 4326)) AS g) s`,
			f.tenant, ref, commune, coop, status, bid, ids.New(), string(g), "TH"+strings.ReplaceAll(ref.String(), "-", "")[20:27])
		return ref
	}
	p1 := seed("31117", &htx, "active", 0)
	p2 := seed("31117", nil, "draft", 200)
	p3 := seed("31120", &htx, "active", 400)
	seed("31117", nil, "retired", 600)
	htxBefore := f.htxRows()
	mismatch := func() string {
		raw, err := os.ReadFile(repoFile(t, "scripts", "kn271-plot-copy", "count.sql"))
		if err != nil {
			t.Fatal(err)
		}
		q := strings.TrimSuffix(strings.TrimSpace(string(raw)), ";")
		return f.scalar(`SELECT string_agg(commune_code || ':' || src_plots || '/' || kn_plots || ':' || src_area_ha || '/' || kn_area_ha, ',' ORDER BY commune_code)
		  FROM (`+q+`) c WHERE tenant_id = $1`, f.tenant)
	}
	if m := mismatch(); m == "" {
		t.Fatal("trước khi chép phải lệch")
	}
	copyFile := repoFile(t, "migrations", "pf", "20261110013755_pf_extension_kn_plot_copy.up.sql") // chạy như ag_migrator (RLS)
	f.script(copyFile, dbadmin.Migrator)
	if m := mismatch(); m != "" {
		t.Fatalf("sau chép phải khớp số thửa và diện tích theo xã: %s", m)
	}
	f.script(copyFile, dbadmin.Migrator) // chạy lại không nhân đôi
	if n := f.scalar(`SELECT count(*)::text FROM pf_extension.kn_plot WHERE tenant_id = $1`, f.tenant); n != "3" {
		t.Fatalf("số thửa đã chép: %s", n)
	}
	if v := f.scalar(`SELECT cooperative_ref::text || '|' || legacy_cooperative_ref::text FROM pf_extension.kn_plot WHERE tenant_id = $1 AND ref = $2`, f.tenant, p1); v != linked.String()+"|"+htx.String() {
		t.Fatalf("ánh xạ HTX → hồ sơ KN: %s", v)
	}
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	g := xa.get("/v1/kn/plots/" + p1.String())
	want(t, g, http.StatusOK, "", "thửa đã chép đọc qua API KN")
	if str(g.body, "cooperative_ref") != linked.String() || str(g.body, "department_code") != dept || str(g.body, "lot_name") != "Lô cũ" {
		t.Fatalf("thửa đã chép: %s", g.raw)
	}
	if d := xa.get("/v1/kn/plots/" + p2.String()); str(d.body, "cooperative_ref") != "" || str(d.body, "status") != "draft" {
		t.Fatalf("thửa nháp đã chép: %s", d.raw)
	}
	if lg := list(xa.get("/v1/kn/plots/"+p1.String()+"/log").body, "items"); len(lg) != 1 || str(lg[0], "event") != "migrated" {
		t.Fatalf("Log của thửa đã chép: %v", lg)
	}
	// Vẽ mới không trùng mã TH… đã chép và chặn chồng lấn với thửa đã chép.
	want(t, xa.post("/v1/kn/plots", body("31117", rect(50, 2000, 100, 100))), http.StatusConflict, "geo.plot_overlap", "chồng lấn thửa đã chép")
	// Quay lui: gỡ phần đã chép (khi chưa ghi gì lên thửa đã chép).
	f.script(repoFile(t, "scripts", "kn271-plot-copy", "rollback.sql"), "")
	if n := f.scalar(`SELECT count(*)::text FROM pf_extension.kn_plot WHERE tenant_id = $1 AND ref IN ($2, $3, $4)`, f.tenant, p1, p2, p3); n != "0" {
		t.Fatalf("sau quay lui còn %s thửa đã chép", n)
	}
	f.script(copyFile, dbadmin.Migrator)
	if m := mismatch(); m != "" {
		t.Fatalf("chép lại sau quay lui phải khớp: %s", m)
	}
	// Đã có ghi mới trên thửa đã chép → quay lui tự động dừng.
	want(t, xa.do(http.MethodPut, "/v1/kn/plots/"+p1.String(), "", map[string]any{"note": "Sửa sau khi chép"}), http.StatusOK, "", "sửa thửa đã chép")
	raw, _ := os.ReadFile(repoFile(t, "scripts", "kn271-plot-copy", "rollback.sql"))
	if _, err := pgtest.Get(t).AdminPool(t, migrations.PF).Exec(context.Background(), "BEGIN;"+string(raw)+"COMMIT;"); err == nil || !strings.Contains(err.Error(), "kn271 rollback") {
		t.Fatalf("quay lui phải dừng khi thửa đã chép có ghi mới: %v", err)
	}
	if after := f.htxRows(); after != htxBefore {
		t.Fatalf("chép/quay lui không được đổi bảng HTX: %s → %s", htxBefore, after)
	}
}
