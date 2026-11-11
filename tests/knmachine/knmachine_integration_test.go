package knmachine_test

// KN-STANDALONE-5-TT-DATA-MAY (#306): máy móc HTX (Trồng trọt FN-XA-03.04) ở kho KN pf_extension trên PostgreSQL thật qua
// module pf/extension (vai r_extension). Đặt ở tests/ vì phải tạo số máy nguồn ở bảng CGH (pf_mechanization) bằng SQL quản
// trị để kiểm lần chép một lần + đếm trước/sau + quay lui, và đếm bảng CGH — mã KN không được nhắc bảng CGH. Ngữ cảnh phiên
// KN đi qua middleware X-AG-Context + projection phiên của KN (như tests/knplot). Cổng bật module của pf/party gắn như gốc
// lắp ráp để kiểm FN chạy khi tenant TẮT pf/mechanization và pf/cooperative.

import (
	"context"
	"encoding/json"
	"io"
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const (
	dept = "trong_trot"
	base = "/v1/kn/machine-counts"
)

// 10:00 thứ Tư 11/11/2026 giờ Việt Nam: năm hiện tại 2026.
var t0 = time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)

type catalogs struct{ items []mdport.CatalogItem }

func (c catalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	for _, it := range c.items {
		if it.Catalog == catalog && it.Code == code {
			return it, nil
		}
	}
	return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
}

func (c catalogs) List(_ context.Context, catalog string) ([]mdport.CatalogItem, error) {
	var out []mdport.CatalogItem
	for _, it := range c.items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

func machineTypes() catalogs {
	mt := func(code, name string, active bool) mdport.CatalogItem {
		return mdport.CatalogItem{Catalog: mdport.CatalogMachineType, Code: code, Name: name, Active: active}
	}
	return catalogs{items: []mdport.CatalogItem{mt("MLD", "Máy làm đất", true), mt("MGD", "Máy gặt đập liên hợp", true),
		mt("MC", "Máy cấy", true), mt("MCU", "Máy cũ ngừng dùng", false)}}
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	team   uuid.UUID
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := extension.New(pool, clock.NewFixed(t0), extension.Deps{Catalogs: machineTypes()})
	pty := party.New(pool, clock.NewFixed(t0))
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	g := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops))
	// Như gốc lắp ráp (assembly.Modules.List): route của module per_tenant đi qua cổng bật module của pf/party.
	mod.RegisterApp(g.Group("", pty.ModuleGate("pf/extension")))
	// Mốc đối chứng: route của module CGH/HTX bị chặn khi tenant tắt module (403 module_not_enabled).
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

func repoFile(parts ...string) string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(self), "..", ".."}, parts...)...)
}

// disable tắt module cho tenant (bảng chiếu cấu hình module của pf/party).
func (f *fix) disable(module string) {
	f.admin(`INSERT INTO pf_party.module_enablement_current (tenant_id, module, status, reason, last_event_id, last_seq, updated_by, updated_at)
	  VALUES ($1, $2, 'disabled', 'Tenant KN chạy độc lập (#306)', $3, 1, $4, now())`, f.tenant, module, ids.New(), ids.New())
}

// record ghi một hồ sơ KN (sổ kn_producer) trực tiếp — như dữ liệu KN đã tự nhập. kind household → không tên.
func (f *fix) record(tid uuid.UUID, kind, event, commune, name string, htx *uuid.UUID) uuid.UUID {
	ref := ids.New()
	var nm *string
	if kind != "household" {
		nm = &name
	}
	f.admin(`INSERT INTO pf_extension.kn_producer (id, tenant_id, stream_key, producer_ref, kind, event, commune_code, name, person_ref,
	    htx_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $8, $3, $10, 'created', $4, $5, $6, $7, now(), $6, $9, 'test')`,
		ids.New(), tid, ref, commune, nm, ids.New(), htx, "kn_producer:"+ref.String(), "seed:"+ref.String(), kind)
	if event != "created" {
		f.admin(`INSERT INTO pf_extension.kn_producer (id, tenant_id, stream_key, producer_ref, kind, event, commune_code, name, person_ref,
		    htx_ref, status_reason, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $8, $3, $10, $11, $4, $5, $6, $7, 'HTX giải thể theo quyết định', now(), $6, $9, 'test')`,
			ids.New(), tid, ref, commune, nm, ids.New(), htx, "kn_producer:"+ref.String(), "seed2:"+ref.String(), kind, event)
	}
	return ref
}

func (f *fix) coop(commune, name string, htx *uuid.UUID) uuid.UUID {
	return f.record(f.tenant, "cooperative", "created", commune, name, htx)
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
}

func (f *fix) user(tid uuid.UUID, role, department string, units ...string) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
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
	return user{f: f, token: tok, ctxID: kc.ID}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

// do gửi request; body kiểu string là JSON thô (để gửi số như 1e100000 đúng nguyên văn).
func (u user) do(method, path, key string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = strings.NewReader(string(raw))
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	if u.ctxID != uuid.Nil {
		r.Header.Set(authz.ContextHeader, u.ctxID.String())
	}
	if key == "" && method != http.MethodGet {
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
	return resp{status: res.StatusCode, raw: raw, body: m}
}

func (u user) get(p string) resp { return u.do(http.MethodGet, p, "", nil) }

type line struct {
	t string
	q int
	n string
}

func lines(ls ...line) map[string]any {
	out := []any{}
	for _, l := range ls {
		m := map[string]any{"machine_type": l.t, "quantity": l.q}
		if l.n != "" {
			m["note"] = l.n
		}
		out = append(out, m)
	}
	return map[string]any{"lines": out}
}

func (u user) put(coop uuid.UUID, body any) resp {
	return u.do(http.MethodPut, base+"/"+coop.String(), "", body)
}

func str(m any, path ...string) string {
	for _, k := range path {
		mm, _ := m.(map[string]any)
		m = mm[k]
	}
	s, _ := m.(string)
	return s
}

func num(m map[string]any, k string) int {
	n, _ := m[k].(float64)
	return int(n)
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

// communeTotal đọc tổng số máy một loại của một xã từ màn tổng quan (-1 nếu không có dòng).
func communeTotal(r resp, commune, mtype string) int {
	for _, t := range list(r.body, "commune_totals") {
		if str(t, "commune_code") == commune && str(t, "machine_type") == mtype {
			return num(t, "quantity")
		}
	}
	return -1
}

// cghRows đếm dòng của tenant ở các bảng số máy của module CGH — luồng KN không được ghi.
func (f *fix) cghRows() string {
	return f.scalar(`SELECT (SELECT count(*) FROM pf_mechanization.coop_machine_count WHERE tenant_id = $1)::text || '/' ||
	  (SELECT count(*) FROM pf_mechanization.coop_machine_count_event WHERE tenant_id = $1)::text || '/' ||
	  (SELECT coalesce(sum(quantity), 0) FROM pf_mechanization.coop_machine_count WHERE tenant_id = $1)::text`, f.tenant)
}

// FN-XA-03.04 trọn luồng khi tenant tắt pf/mechanization và pf/cooperative: khai báo theo hồ sơ KN loại HTX, tổng theo loại
// của xã = Σ các HTX, lịch sử thay đổi số lượng, năm sản xuất; bảng CGH không đổi.
func TestFN_XA_03_04_StandaloneWithCGHModulesDisabled(t *testing.T) {
	f := newFix(t)
	f.disable("pf/mechanization")
	f.disable("pf/cooperative")
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	for _, m := range []string{"mechanization", "cooperative"} {
		want(t, xa.get("/v1/probe/"+m), http.StatusForbidden, "module_not_enabled", "đối chứng: module "+m+" đã tắt")
	}
	before := f.cghRows()
	c1, c2 := f.coop("31117", "HTX Bình Hòa", nil), f.coop("31117", "HTX Thuận Phát", nil)

	r := xa.put(c1, lines(line{"MLD", 2, "Máy cày 4 bánh"}, line{"MGD", 1, ""}))
	want(t, r, http.StatusOK, "", "khai báo máy HTX 1")
	if num(r.body, "total") != 3 || str(r.body, "name") != "HTX Bình Hòa" || str(r.body, "commune_code") != "31117" || str(r.body, "cooperative_ref") != c1.String() {
		t.Fatalf("khai báo sau lưu: %s", r.raw)
	}
	ls := list(r.body, "lines")
	if len(ls) != 2 || str(ls[0], "machine_type") != "MGD" || str(ls[1], "machine_type_name") != "Máy làm đất" || str(ls[1], "note") != "Máy cày 4 bánh" || ls[0]["note"] != nil {
		t.Fatalf("dòng khai báo: %s", r.raw)
	}
	want(t, xa.put(c2, lines(line{"MLD", 5, ""})), http.StatusOK, "", "khai báo máy HTX 2")

	// AC: tổng số máy theo loại của xã bằng tổng các HTX.
	ov := xa.get(base + "?commune=31117")
	want(t, ov, http.StatusOK, "", "tổng quan xã")
	if len(list(ov.body, "cooperatives")) != 2 || communeTotal(ov, "31117", "MLD") != 7 || communeTotal(ov, "31117", "MGD") != 1 {
		t.Fatalf("tổng theo loại của xã phải = Σ các HTX: %s", ov.raw)
	}

	// Sửa số lượng, bỏ một loại, thêm một loại — mỗi loại đổi là một dòng lịch sử mang số cũ → mới.
	key := "k-" + ids.New().String()
	r = xa.do(http.MethodPut, base+"/"+c1.String(), key, lines(line{"MLD", 3, "Máy cày 4 bánh"}, line{"MC", 4, ""}))
	want(t, r, http.StatusOK, "", "sửa khai báo")
	if num(r.body, "total") != 7 {
		t.Fatalf("tổng sau sửa: %s", r.raw)
	}
	want(t, xa.do(http.MethodPut, base+"/"+c1.String(), key, lines(line{"MLD", 3, "Máy cày 4 bánh"}, line{"MC", 4, ""})), http.StatusOK, "", "gửi lại cùng khóa")
	lg := xa.get(base + "/" + c1.String() + "/log")
	want(t, lg, http.StatusOK, "", "lịch sử số lượng")
	items := list(lg.body, "items")
	if len(items) != 5 { // 2 dòng đầu + MC set, MGD removed, MLD set (gửi lại cùng khóa không ghi thêm)
		t.Fatalf("lịch sử: %s", lg.raw)
	}
	got := map[string]string{}
	for _, it := range items[2:] {
		ch := list(it, "changes")[0]
		got[str(it, "machine_type")+":"+str(it, "event")] = str(ch, "old") + "→" + str(ch, "new")
		if str(it, "department_code") != dept || str(it, "actor_ref") == "" {
			t.Fatalf("dòng lịch sử thiếu truy vết: %v", it)
		}
	}
	if got["MLD:set"] != "2→3" || got["MGD:removed"] != "1→" || got["MC:set"] != "→4" {
		t.Fatalf("lịch sử thay đổi số lượng: %v", got)
	}
	if ov = xa.get(base); communeTotal(ov, "31117", "MLD") != 8 || communeTotal(ov, "31117", "MGD") != -1 || communeTotal(ov, "31117", "MC") != 4 {
		t.Fatalf("tổng xã sau sửa: %s", ov.raw)
	}

	// Năm sản xuất theo loại máy (hành vi đã có) khóa theo cùng hồ sơ KN.
	y := xa.do(http.MethodPut, "/v1/kn/machine-profiles/"+c1.String(), "", map[string]any{"lines": []any{map[string]any{"machine_type": "MLD", "year_made": 2019}}})
	want(t, y, http.StatusOK, "", "năm sản xuất")
	if it := list(xa.get("/v1/kn/machine-profiles/"+c1.String()).body, "items"); len(it) != 1 || num(it[0], "year_made") != 2019 {
		t.Fatalf("năm sản xuất theo hồ sơ KN: %v", it)
	}
	want(t, xa.do(http.MethodPut, "/v1/kn/machine-profiles/"+c1.String(), "", map[string]any{"lines": []any{map[string]any{"machine_type": "MLD", "year_made": 2027}}}),
		http.StatusUnprocessableEntity, "extension.kn_attrs_year_invalid", "năm sản xuất sau năm hiện tại")

	// Mã không phải hồ sơ KN loại HTX (kể cả mã HTX nền tảng) → 404, không lộ sự tồn tại.
	htx := ids.New()
	f.admin(`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
	  VALUES ($1, $2, 'cooperative', $3, 'HTX nền tảng', '31117', true, $4, 1)`, f.tenant, htx, "H"+htx.String()[24:], ids.New())
	want(t, xa.put(htx, lines(line{"MLD", 1, ""})), http.StatusNotFound, "mechanization.cooperative_not_found", "khai máy theo mã HTX nền tảng")
	want(t, xa.get("/v1/kn/machine-profiles/"+htx.String()), http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "năm máy theo mã HTX nền tảng")
	hh := f.record(f.tenant, "household", "created", "31117", "", nil)
	want(t, xa.get(base+"/"+hh.String()), http.StatusNotFound, "mechanization.cooperative_not_found", "hồ sơ Nông hộ không phải HTX")
	want(t, xa.get(base+"/khong-phai-uuid"), http.StatusNotFound, "mechanization.cooperative_not_found", "mã sai dạng")

	if after := f.cghRows(); after != before {
		t.Fatalf("KN không được ghi bảng CGH: %s → %s", before, after)
	}
}

// Luồng phụ BRD: số lượng âm hoặc không nguyên → chặn; trùng loại máy → gợi ý cập nhật dòng đã có; loại máy từ danh mục.
// Cú pháp số kiểm trước khi parse (1e100000, 1e-100000, chuỗi rất dài).
func TestFN_XA_03_04_Validation(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	c := f.coop("31117", "HTX Kiểm Số", nil)
	path := base + "/" + c.String()
	for name, q := range map[string]string{"không": "0", "âm": "-1", "thập phân": "1.5", "2.0": "2.0", "chuỗi": `"2"`, "null": "null",
		"số mũ": "1e3", "1e100000": "1e100000", "1e-100000": "1e-100000", "vượt 1.000.000": "1000001",
		"rất dài": strings.Repeat("9", 100_000), "boolean": "true"} {
		r := xa.do(http.MethodPut, path, "", `{"lines":[{"machine_type":"MLD","quantity":`+q+`}]}`)
		want(t, r, http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "số lượng "+name)
		if !strings.Contains(string(r.raw), "lines[0].quantity") {
			t.Fatalf("số lượng %s: thiếu trường lỗi: %.300s", name, r.raw)
		}
	}
	want(t, xa.do(http.MethodPut, path, "", `{"lines":[{"machine_type":"MLD"}]}`), http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "thiếu số lượng")
	want(t, xa.do(http.MethodPut, path, "", `{"reason":"x"}`), http.StatusUnprocessableEntity, "validation_failed", "thiếu lines")
	want(t, xa.do(http.MethodPut, path, "", `{"lines":`), http.StatusBadRequest, "", "JSON hỏng")
	want(t, xa.put(c, lines(line{"MLD", 1, ""}, line{"MLD", 2, ""})), http.StatusUnprocessableEntity, "mechanization.machine_type_duplicate", "trùng loại máy")
	want(t, xa.put(c, lines(line{"KHONG_CO", 1, ""})), http.StatusUnprocessableEntity, "mechanization.machine_type_unknown", "loại ngoài danh mục")
	want(t, xa.put(c, lines(line{"MCU", 1, ""})), http.StatusUnprocessableEntity, "mechanization.machine_type_unknown", "loại đã ngừng dùng")
	want(t, xa.put(c, lines(line{"", 1, ""})), http.StatusUnprocessableEntity, "validation_failed", "thiếu loại máy")
	want(t, xa.put(c, lines(line{"MLD", 1, strings.Repeat("ă", 501)})), http.StatusUnprocessableEntity, "validation_failed", "ghi chú quá 500 ký tự")
	many := make([]line, 101)
	for i := range many {
		many[i] = line{"MLD", 1, ""}
	}
	want(t, xa.put(c, lines(many...)), http.StatusUnprocessableEntity, "validation_failed", "quá 100 dòng")
	if n := f.scalar(`SELECT count(*)::text FROM pf_extension.kn_machine_count_event WHERE tenant_id = $1`, f.tenant); n != "0" {
		t.Fatalf("request sai không được ghi sổ: %s dòng", n)
	}
	// Số lượng biên hợp lệ; bỏ mọi dòng bằng mảng rỗng.
	want(t, xa.put(c, lines(line{"MLD", 1, ""}, line{"MGD", 1_000_000, ""})), http.StatusOK, "", "biên 1 và 1.000.000")
	if r := xa.put(c, map[string]any{"lines": []any{}}); r.status != http.StatusOK || num(r.body, "total") != 0 || len(list(r.body, "lines")) != 0 {
		t.Fatalf("bỏ mọi dòng: %s", r.raw)
	}
	// HTX đã ngừng hoạt động: xem được, không ghi được.
	off := f.record(f.tenant, "cooperative", "deactivated", "31117", "HTX Đã Ngừng", nil)
	want(t, xa.get(base+"/"+off.String()), http.StatusOK, "", "xem HTX đã ngừng")
	want(t, xa.put(off, lines(line{"MLD", 1, ""})), http.StatusUnprocessableEntity, "mechanization.cooperative_invalid", "ghi HTX đã ngừng")
}

// Quyền BRD: Cấp Xã CRUD trong xã; Cấp Tỉnh, Cấp Quốc gia chỉ xem; Admin CRUD; ngoài phạm vi, tenant khác không thấy gì.
func TestFN_XA_03_04_RolesScopeTenant(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	c1, c2 := f.coop("31117", "HTX Trong Xã", nil), f.coop("31120", "HTX Xã Khác", nil)
	want(t, xa.put(c1, lines(line{"MLD", 2, ""})), http.StatusOK, "", "Cấp Xã khai trong xã")
	want(t, xa.put(c2, lines(line{"MLD", 2, ""})), http.StatusNotFound, "mechanization.cooperative_not_found", "Cấp Xã khai HTX xã khác")
	want(t, xa.get(base+"/"+c2.String()), http.StatusNotFound, "mechanization.cooperative_not_found", "Cấp Xã đọc HTX xã khác")
	want(t, xa.get(base+"/"+c2.String()+"/log"), http.StatusNotFound, "mechanization.cooperative_not_found", "Cấp Xã đọc Log HTX xã khác")
	want(t, xa.get(base+"?commune=31120"), http.StatusForbidden, "mechanization.out_of_scope", "lọc xã ngoài phạm vi")

	tinh := f.user(f.tenant, authz.KNTinh, dept, "31117", "31120")
	want(t, tinh.put(c1, lines(line{"MLD", 9, ""})), http.StatusForbidden, "mechanization.read_only", "Cấp Tỉnh ghi")
	if communeTotal(tinh.get(base), "31117", "MLD") != 2 {
		t.Fatal("Cấp Tỉnh xem tổng xã")
	}
	tw := f.user(f.tenant, authz.KNTW, dept)
	want(t, tw.put(c1, lines(line{"MLD", 9, ""})), http.StatusForbidden, "mechanization.read_only", "Cấp Quốc gia ghi")
	if r := tw.get(base + "/" + c1.String()); num(r.body, "total") != 2 {
		t.Fatalf("Cấp Quốc gia xem: %s", r.raw)
	}
	// Cấp Xã phòng khác đọc được (máy của HTX là dữ liệu chung, không lọc phòng ban).
	if r := f.user(f.tenant, authz.KNXa, "chan_nuoi_thuy_san", "31117").get(base + "/" + c1.String()); num(r.body, "total") != 2 {
		t.Fatalf("phòng khác xem: %s", r.raw)
	}
	adm := f.user(f.tenant, authz.KNAdmin, "")
	want(t, adm.put(c2, lines(line{"MGD", 4, ""})), http.StatusOK, "", "Admin khai mọi xã")
	if d := f.scalar(`SELECT department_code FROM pf_extension.kn_machine_count WHERE tenant_id = $1 AND cooperative_ref = $2`, f.tenant, c2); d != dept {
		t.Fatalf("Admin không chọn phòng ban → trong_trot, nhận %q", d)
	}
	want(t, adm.do(http.MethodPut, base+"/"+c2.String()+"?department=SAI", "", lines(line{"MGD", 4, ""})), http.StatusUnprocessableEntity, "validation_failed", "Admin phòng ban sai")

	// Tenant khác: hồ sơ và khai báo của tenant này không tồn tại.
	otherTenant := ids.New()
	other := f.user(otherTenant, authz.KNXa, dept, "31117")
	if r := other.get(base); r.status != http.StatusOK || len(list(r.body, "cooperatives")) != 0 || len(list(r.body, "commune_totals")) != 0 {
		t.Fatalf("tenant khác thấy dữ liệu: %s", r.raw)
	}
	want(t, other.get(base+"/"+c1.String()), http.StatusNotFound, "mechanization.cooperative_not_found", "tenant khác đọc")
	want(t, other.put(c1, lines(line{"MLD", 1, ""})), http.StatusNotFound, "mechanization.cooperative_not_found", "tenant khác ghi")
	// Không có ngữ cảnh phiên KN → từ chối.
	noCtx := user{f: f, token: xa.token}
	if r := noCtx.get(base); r.status < 400 || r.status >= 500 {
		t.Fatalf("thiếu X-AG-Context phải bị từ chối: %d %s", r.status, r.raw)
	}
}

// Bề mặt KN đã gỡ ở #306: ứng viên HTX đã công bố (route 404), liên kết htx_ref không còn ghi được.
func TestKN306_PublishedHTXLinkHidden(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	want(t, xa.get("/v1/kn/producer-htx-candidates"), http.StatusNotFound, "", "ứng viên HTX đã công bố")
	want(t, xa.get("/v1/kn/producer-htx-candidates?commune=31117"), http.StatusNotFound, "", "ứng viên HTX đã công bố theo xã")
	r := xa.do(http.MethodPost, "/v1/kn/producer-records", "", map[string]any{"kind": "cooperative", "commune_code": "31117", "name": "HTX Liên Kết",
		"representative": map[string]any{"full_name": "Nguyễn Văn An", "phone": "0912345678"}, "htx_ref": ids.New().String()})
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "tạo hồ sơ kèm htx_ref")
	if !strings.Contains(string(r.raw), "htx_ref") {
		t.Fatalf("lỗi phải chỉ trường htx_ref: %s", r.raw)
	}
	// Liên kết đã có từ trước: đọc được (chỉ đối chiếu), không tra tên HTX đã công bố.
	htx := ids.New()
	linked := f.coop("31117", "HTX Có Liên Kết Cũ", &htx)
	g := xa.get("/v1/kn/producer-records/" + linked.String())
	want(t, g, http.StatusOK, "", "đọc hồ sơ có liên kết cũ")
	if str(g.body, "htx_ref") != htx.String() || g.body["htx_name"] != nil {
		t.Fatalf("liên kết cũ chỉ đọc, không tên HTX đã công bố: %s", g.raw)
	}
}

// Chép một lần từ module CGH: chạy như ag_migrator, count.sql khớp số dòng và tổng số máy theo xã × loại, ánh xạ HTX nền
// tảng → hồ sơ KN, HTX chưa có hồ sơ KN giữ khóa cũ (tổng xã không đổi, vẫn sửa được), năm sản xuất theo sang hồ sơ KN,
// chạy lại không nhân đôi, rollback.sql gỡ đúng phần chép và dừng khi đã có ghi mới; bảng CGH không đổi.
func TestKN306_CopyCountRollback(t *testing.T) {
	f := newFix(t)
	htx, orphan, htxFar := ids.New(), ids.New(), ids.New()
	linked := f.coop("31117", "HTX Liên Kết", &htx)
	linkedFar := f.coop("31120", "HTX Xã Bên", &htxFar)
	seed := func(coop uuid.UUID, commune, mtype string, qty int, active bool, note *string) {
		f.admin(`INSERT INTO pf_mechanization.coop_machine_count (tenant_id, cooperative_ref, machine_type, quantity, note, commune_code,
		    department_code, active, updated_at, updated_by, last_event_id, last_seq)
		  VALUES ($1, $2, $3, $4, $5, $6, 'trong_trot', $7, '2026-10-20T02:00:00Z', $8, $9, 1)`,
			f.tenant, coop, mtype, qty, note, commune, active, ids.New(), ids.New())
	}
	year := func(coop uuid.UUID, commune, mtype string, y int) {
		f.admin(`INSERT INTO pf_extension.kn_machine_year (id, tenant_id, stream_key, cooperative_ref, commune_code, department_code,
		    machine_type, machine_type_name, event, year_made, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, 'trong_trot', $6, 'Máy', 'set', $7, now(), $8, $9, 'kn-attrs-1')`,
			ids.New(), f.tenant, "kn_machine_year:trong_trot:"+coop.String(), coop, commune, mtype, y, ids.New(), "seed:"+coop.String()+mtype)
	}
	note := "Máy cũ của HTX"
	seed(htx, "31117", "MLD", 2, true, &note)
	seed(htx, "31117", "MGD", 1, true, nil)
	seed(htx, "31117", "MC", 0, false, nil) // dòng đã bỏ: không chép
	seed(orphan, "31117", "MLD", 5, true, nil)
	seed(htxFar, "31120", "MLD", 4, true, nil)
	year(htx, "31117", "MLD", 2015)
	year(orphan, "31117", "MLD", 2010)
	cghBefore := f.cghRows()
	mismatch := func() string {
		raw, err := os.ReadFile(repoFile("scripts", "kn306-machine-copy", "count.sql"))
		if err != nil {
			t.Fatal(err)
		}
		q := strings.TrimSuffix(strings.TrimSpace(string(raw)), ";")
		return f.scalar(`SELECT string_agg(commune_code || ':' || machine_type || ':' || src_lines || '/' || kn_lines || ':' || src_quantity || '/' || kn_quantity, ',' ORDER BY commune_code, machine_type)
		  FROM (`+q+`) c WHERE tenant_id = $1`, f.tenant)
	}
	if m := mismatch(); m != "31117:MGD:1/0:1/0,31117:MLD:2/0:7/0,31120:MLD:1/0:4/0" {
		t.Fatalf("trước khi chép phải lệch đúng phần nguồn: %s", m)
	}
	copyFile := repoFile("migrations", "pf", "20261111002854_pf_extension_kn_machine_count_copy.up.sql") // chạy như ag_migrator (RLS)
	f.script(copyFile, dbadmin.Migrator)
	if m := mismatch(); m != "" {
		t.Fatalf("sau chép phải khớp số dòng và tổng số máy theo xã × loại: %s", m)
	}
	f.script(copyFile, dbadmin.Migrator) // chạy lại không nhân đôi
	if n := f.scalar(`SELECT count(*)::text || '/' || sum(quantity)::text FROM pf_extension.kn_machine_count WHERE tenant_id = $1 AND active`, f.tenant); n != "4/12" {
		t.Fatalf("số dòng/tổng máy đã chép: %s", n)
	}
	if n := f.scalar(`SELECT count(*)::text FROM pf_extension.kn_machine_count_event WHERE tenant_id = $1`, f.tenant); n != "4" {
		t.Fatalf("chạy lại không được ghi thêm sổ: %s dòng", n)
	}
	if v := f.scalar(`SELECT string_agg(cooperative_ref::text || '|' || legacy_htx_ref::text, ',' ORDER BY machine_type)
	  FROM pf_extension.kn_machine_count WHERE tenant_id = $1 AND legacy_htx_ref = $2`, f.tenant, htx); v != linked.String()+"|"+htx.String()+","+linked.String()+"|"+htx.String() {
		t.Fatalf("ánh xạ HTX nền tảng → hồ sơ KN: %s", v)
	}
	if v := f.scalar(`SELECT cooperative_ref::text FROM pf_extension.kn_machine_count WHERE tenant_id = $1 AND legacy_htx_ref = $2`, f.tenant, orphan); v != orphan.String() {
		t.Fatalf("HTX chưa có hồ sơ KN phải giữ khóa cũ: %s", v)
	}
	if v := f.scalar(`SELECT substr(id::text, 15, 1) FROM pf_extension.kn_machine_count_event WHERE tenant_id = $1 LIMIT 1`, f.tenant); v != "7" {
		t.Fatalf("mã dòng sổ đã chép phải là UUIDv7: phiên bản %s", v)
	}

	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	g := xa.get(base + "/" + linked.String())
	want(t, g, http.StatusOK, "", "khai báo đã chép đọc qua hồ sơ KN")
	if ls := list(g.body, "lines"); num(g.body, "total") != 3 || len(ls) != 2 || str(ls[1], "note") != note || str(ls[1], "updated_at") != "2026-10-20T02:00:00Z" {
		t.Fatalf("khai báo đã chép: %s", g.raw)
	}
	if lg := list(xa.get(base+"/"+linked.String()+"/log").body, "items"); len(lg) != 2 || str(lg[0], "event") != "migrated" {
		t.Fatalf("Log của khai báo đã chép: %v", lg)
	}
	if it := list(xa.get("/v1/kn/machine-profiles/"+linked.String()).body, "items"); len(it) != 1 || num(it[0], "year_made") != 2015 {
		t.Fatalf("năm sản xuất theo sang hồ sơ KN: %v", it)
	}
	// Tổng theo xã không đổi: gồm cả HTX cũ chưa có hồ sơ KN (tên rỗng), vẫn đọc và sửa được.
	ov := xa.get(base + "?commune=31117")
	if communeTotal(ov, "31117", "MLD") != 7 || communeTotal(ov, "31117", "MGD") != 1 || len(list(ov.body, "cooperatives")) != 2 {
		t.Fatalf("tổng xã sau chép: %s", ov.raw)
	}
	og := xa.get(base + "/" + orphan.String())
	want(t, og, http.StatusOK, "", "đọc HTX cũ chưa có hồ sơ KN")
	if num(og.body, "total") != 5 || str(og.body, "name") != "" {
		t.Fatalf("HTX cũ chưa có hồ sơ KN: %s", og.raw)
	}
	if it := list(xa.get("/v1/kn/machine-profiles/"+orphan.String()).body, "items"); len(it) != 1 || num(it[0], "year_made") != 2010 {
		t.Fatalf("năm sản xuất của HTX cũ giữ nguyên khóa: %v", it)
	}
	want(t, f.user(f.tenant, authz.KNXa, dept, "31120").get(base+"/"+orphan.String()), http.StatusNotFound, "mechanization.cooperative_not_found", "HTX cũ ngoài phạm vi")
	if r := f.user(f.tenant, authz.KNXa, dept, "31120").get(base + "/" + linkedFar.String()); num(r.body, "total") != 4 {
		t.Fatalf("xã bên đọc khai báo đã chép: %s", r.raw)
	}

	// Quay lui: gỡ phần đã chép (khi chưa ghi gì lên khai báo đã chép).
	rollback := repoFile("scripts", "kn306-machine-copy", "rollback.sql")
	f.script(rollback, "")
	if n := f.scalar(`SELECT ((SELECT count(*) FROM pf_extension.kn_machine_count WHERE tenant_id = $1) +
	    (SELECT count(*) FROM pf_extension.kn_machine_count_event WHERE tenant_id = $1) +
	    (SELECT count(*) FROM pf_extension.kn_machine_year WHERE tenant_id = $1 AND cooperative_ref = $2))::text`, f.tenant, linked); n != "0" {
		t.Fatalf("sau quay lui còn %s dòng đã chép", n)
	}
	if n := f.scalar(`SELECT count(*)::text FROM pf_extension.kn_machine_year WHERE tenant_id = $1`, f.tenant); n != "2" {
		t.Fatalf("quay lui không được đụng dòng năm sản xuất cũ: %s", n)
	}
	f.script(copyFile, dbadmin.Migrator)
	if m := mismatch(); m != "" {
		t.Fatalf("chép lại sau quay lui phải khớp: %s", m)
	}
	// Sửa khai báo đã chép (kể cả của HTX cũ chưa có hồ sơ KN): ảnh chụp lúc chép không đổi nên count.sql vẫn khớp.
	want(t, xa.put(linked, lines(line{"MLD", 3, note})), http.StatusOK, "", "sửa khai báo đã chép")
	want(t, xa.put(orphan, map[string]any{"lines": []any{}}), http.StatusOK, "", "bỏ khai báo của HTX cũ")
	if ov = xa.get(base + "?commune=31117"); communeTotal(ov, "31117", "MLD") != 3 || communeTotal(ov, "31117", "MGD") != -1 {
		t.Fatalf("tổng xã sau khi KN sửa: %s", ov.raw)
	}
	if m := mismatch(); m != "" {
		t.Fatalf("đối chiếu lần chép không đổi khi KN sửa tiếp: %s", m)
	}
	// Đã có ghi mới trên khai báo đã chép → quay lui tự động dừng.
	raw, _ := os.ReadFile(rollback)
	if _, err := pgtest.Get(t).AdminPool(t, migrations.PF).Exec(context.Background(), "BEGIN;"+string(raw)+"COMMIT;"); err == nil || !strings.Contains(err.Error(), "kn306 rollback") {
		t.Fatalf("quay lui phải dừng khi khai báo đã chép có ghi mới: %v", err)
	}
	if after := f.cghRows(); after != cghBefore {
		t.Fatalf("chép/quay lui/sửa ở KN không được đổi bảng CGH: %s → %s", cghBefore, after)
	}
}
