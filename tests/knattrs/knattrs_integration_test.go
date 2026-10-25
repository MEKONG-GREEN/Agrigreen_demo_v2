package knattrs_test

// BF-SRV-KN-ATTRS (#119): hồ sơ KN riêng trên PostgreSQL thật qua module pf/extension (vai r_extension). Kiểm thử đặt
// ở tests/ (ngoài module KN) vì phải tạo dữ liệu nguồn HTX bằng SQL — mã KN không được nhắc bảng HTX. HTX và thửa
// được tạo ở bảng nguồn bằng vai quản trị (như dữ liệu HTX có sẵn) và chỉ được KN đọc qua publication
// pf_party_pub.v1_org_tree, pf_cooperative_pub.v1_plots. Ngữ cảnh phiên KN đi qua middleware X-AG-Context + projection
// phiên của KN (như tests/kn77, sổ vật tư) — không đặt Principal thẳng vào handler.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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

const dept = "trong_trot"

// 10:00 thứ Năm 01/10/2026 giờ Việt Nam: năm hiện tại 2026.
var t0 = time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)

type catalogs struct{ m map[string]mdport.CatalogItem }

func (c catalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	it, ok := c.m[catalog+"/"+code]
	if !ok {
		return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
	}
	return it, nil
}

func (c catalogs) List(_ context.Context, catalog string) ([]mdport.CatalogItem, error) {
	var out []mdport.CatalogItem
	for _, it := range c.m {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	// HTX và thửa nguồn của tenant chính.
	coop, coopOther, coopInactive, team uuid.UUID
	plot, plotDraft, plotOther, plotRet uuid.UUID
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	item := func(catalog, code, name string, active bool) mdport.CatalogItem {
		return mdport.CatalogItem{Catalog: catalog, Code: code, Name: name, Active: active}
	}
	cat := catalogs{m: map[string]mdport.CatalogItem{
		"machine_type/MLD": item(mdport.CatalogMachineType, "MLD", "Máy làm đất", true),
		"machine_type/MGD": item(mdport.CatalogMachineType, "MGD", "Máy gặt đập liên hợp", true),
		"machine_type/OLD": item(mdport.CatalogMachineType, "OLD", "Máy cũ", false),
		"crop/lua":         item(mdport.CatalogCrop, "lua", "Lúa", true),
		"crop/ngo":         item(mdport.CatalogCrop, "ngo", "Ngô", true),
		"crop/cu":          item(mdport.CatalogCrop, "cu", "Cây ngừng dùng", false),
	}}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := extension.New(pool, clock.NewFixed(t0), extension.Deps{Catalogs: cat})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	f.seed()
	return f
}

func (f *fix) admin(sql string, args ...any) {
	f.t.Helper()
	if _, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("seed: %v\n%s", err, sql)
	}
}

func (f *fix) org(tid, ref uuid.UUID, kind partyport.OrgKind, unit string, active bool) {
	f.admin(`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $4, $5, $6, $7, 1)`, tid, ref, string(kind), "O"+ref.String()[24:], unit, active, ids.New())
}

func (f *fix) plotRow(tid, ref uuid.UUID, commune string, coop uuid.UUID, status string) {
	var c any
	if coop != uuid.Nil {
		c = coop
	}
	f.admin(`INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, status, current_version,
	    current_boundary_id, geom, area_ha, source, created_by)
	  VALUES ($1, $2, $3, $4, $5::uuid, $6, 1, gen_random_uuid(),
	    ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326)), 1.2::numeric, 'kn_web', gen_random_uuid())`,
		tid, ref, "T"+ref.String()[24:], commune, c, status)
}

// seed: HTX ở xã 31117 (trong phạm vi), 31120 (ngoài phạm vi), HTX ngừng hoạt động, tổ khuyến nông (Nút của Cấp Xã);
// thửa có HTX, thửa nháp, thửa xã khác, thửa ngừng dùng.
func (f *fix) seed() {
	f.coop, f.coopOther, f.coopInactive, f.team = ids.New(), ids.New(), ids.New(), ids.New()
	f.org(f.tenant, f.coop, partyport.OrgCooperative, "31117", true)
	f.org(f.tenant, f.coopOther, partyport.OrgCooperative, "31120", true)
	f.org(f.tenant, f.coopInactive, partyport.OrgCooperative, "31117", false)
	f.org(f.tenant, f.team, partyport.OrgExtensionTeam, "31117", true)
	f.admin(`INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2), ($1, $3)`, f.tenant, f.coop, f.coopOther)
	f.plot, f.plotDraft, f.plotOther, f.plotRet = ids.New(), ids.New(), ids.New(), ids.New()
	f.plotRow(f.tenant, f.plot, "31117", f.coop, "active")
	f.plotRow(f.tenant, f.plotDraft, "31117", uuid.Nil, "draft")
	f.plotRow(f.tenant, f.plotOther, "31120", f.coopOther, "active")
	f.plotRow(f.tenant, f.plotRet, "31117", uuid.Nil, "retired")
}

func (f *fix) count(table string) int {
	f.t.Helper()
	var n int
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		"SELECT count(*) FROM pf_extension."+table+" WHERE tenant_id = $1", f.tenant).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
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
	noCtx bool
}

func (f *fix) user(tid uuid.UUID, role, department string, units ...string) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: department}
	switch role {
	case authz.KNXa:
		kc.Communes, kc.Scope = units, authz.Scope{Units: units}
		kc.OrgRef = f.team // Nút của lần gán Cấp Xã (tổ khuyến nông xã 31117)
	case authz.KNTW, authz.KNAdmin:
		kc.Scope = authz.Scope{All: true}
	default:
		kc.Scope = authz.Scope{Units: units}
	}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
}

// revoke thu hồi ngữ cảnh phiên (middleware P1 trả 403 cho phiên không còn hiệu lực).
func (u user) revoke() {
	u.f.mu.Lock()
	delete(u.f.ctxs, u.ctxID)
	u.f.mu.Unlock()
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	if !u.noCtx {
		r.Header.Set(authz.ContextHeader, u.ctxID.String())
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

func (u user) get(p string) resp        { return u.do(http.MethodGet, p, nil) }
func (u user) put(p string, b any) resp { return u.do(http.MethodPut, p, b) }

func str(m any, k string) string { mm, _ := m.(map[string]any); s, _ := mm[k].(string); return s }
func items(r resp) []map[string]any {
	l, _ := r.body["items"].([]any)
	out := []map[string]any{}
	for _, x := range l {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

func want(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || (c != "" && str(r.body, "code") != c) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, c, r.status, r.raw)
	}
}

func fieldErr(t *testing.T, r resp, field string) {
	t.Helper()
	errs, _ := r.body["errors"].([]any)
	for _, e := range errs {
		if str(e, "field") == field {
			return
		}
	}
	t.Fatalf("thiếu lỗi trường %s: %s", field, r.raw)
}

func ml(t string, year any) map[string]any {
	return map[string]any{"machine_type": t, "year_made": year}
}

func lines(ls ...map[string]any) map[string]any {
	if ls == nil {
		ls = []map[string]any{}
	}
	return map[string]any{"lines": ls}
}

func years(r resp) map[string]float64 {
	out := map[string]float64{}
	for _, it := range items(r) {
		y, _ := it["year_made"].(float64)
		out[str(it, "machine_type")] = y
	}
	return out
}

// TT-10: năm sản xuất được lưu và trả lại thật; thay hồ sơ = set/removed; không đổi → không ghi sổ.
func TestTT10_MachineYearSavedAndReadBack(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	path := "/v1/kn/machine-profiles/" + f.coop.String()
	g := xa.get(path)
	want(t, g, http.StatusOK, "", "hồ sơ rỗng")
	if len(items(g)) != 0 || str(g.body, "commune_code") != "31117" || str(g.body, "department_code") != dept {
		t.Fatalf("hồ sơ rỗng: %s", g.raw)
	}
	r := xa.put(path, lines(ml("MLD", 2015), ml("MGD", 2026)))
	want(t, r, http.StatusOK, "", "lưu năm máy")
	if y := years(r); len(y) != 2 || y["MLD"] != 2015 || y["MGD"] != 2026 {
		t.Fatalf("năm máy sau lưu: %s", r.raw)
	}
	if it := items(r)[0]; str(it, "machine_type") != "MGD" || str(it, "machine_type_name") != "Máy gặt đập liên hợp" || str(it, "updated_at") == "" {
		t.Fatalf("dòng hồ sơ: %s", r.raw)
	}
	if y := years(xa.get(path)); len(y) != 2 || y["MLD"] != 2015 || y["MGD"] != 2026 {
		t.Fatal("đọc lại không khớp")
	}
	if n := f.count("kn_machine_year"); n != 2 {
		t.Fatalf("sổ sau lần đầu: %d", n)
	}
	// Gửi lại y nguyên → không ghi.
	want(t, xa.put(path, lines(ml("MGD", 2026), ml("MLD", 2015))), http.StatusOK, "", "gửi lại")
	if n := f.count("kn_machine_year"); n != 2 {
		t.Fatalf("gửi lại ghi sổ: %d", n)
	}
	// Sửa năm một loại, bỏ loại kia.
	r = xa.put(path, lines(ml("MLD", 2016)))
	want(t, r, http.StatusOK, "", "sửa năm")
	if y := years(r); len(y) != 1 || y["MLD"] != 2016 {
		t.Fatalf("sau sửa: %s", r.raw)
	}
	if y := years(xa.get(path)); len(y) != 1 || y["MLD"] != 2016 {
		t.Fatal("đọc lại sau sửa không khớp")
	}
	if n := f.count("kn_machine_year"); n != 4 {
		t.Fatalf("sổ sau sửa (set + removed): %d", n)
	}
	// Mảng rỗng = bỏ toàn bộ.
	want(t, xa.put(path, lines()), http.StatusOK, "", "bỏ hết")
	if len(items(xa.get(path))) != 0 {
		t.Fatal("bỏ hết vẫn còn dòng")
	}
}

// TT-10 ca âm: năm tương lai/âm/không nguyên/chuỗi/thiếu, loại máy trùng/lạ/ngừng dùng, thiếu lines — 422 theo trường,
// không ghi dòng nào, hồ sơ giữ nguyên.
func TestTT10_MachineYearValidationWritesNothing(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	path := "/v1/kn/machine-profiles/" + f.coop.String()
	want(t, xa.put(path, lines(ml("MLD", 2010))), http.StatusOK, "", "hồ sơ ban đầu")
	before := f.count("kn_machine_year")
	for _, tc := range []struct {
		body      map[string]any
		code, fld string
		what      string
	}{
		{lines(ml("MLD", 2027)), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "năm tương lai"},
		{lines(ml("MLD", -2020)), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "năm âm"},
		{lines(ml("MLD", 0)), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "năm 0"},
		{lines(ml("MLD", 2015.5)), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "năm không nguyên"},
		{lines(ml("MLD", "2015")), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "năm dạng chuỗi"},
		{lines(ml("MLD", nil)), "validation_failed", "lines.MLD.year_made", "thiếu năm"},
		{lines(ml("", 2015)), "validation_failed", "lines.machine_type", "thiếu loại máy"},
		{lines(ml("MLD", 2015), ml("MLD", 2016)), "validation_failed", "lines.MLD.machine_type", "loại trùng"},
		{lines(ml("XYZ", 2015)), "extension.kn_attrs_machine_type_unknown", "lines.XYZ.machine_type", "loại lạ"},
		{lines(ml("OLD", 2015)), "extension.kn_attrs_machine_type_unknown", "lines.OLD.machine_type", "loại ngừng dùng"},
		{map[string]any{}, "validation_failed", "lines", "thiếu lines"},
		// Một dòng hợp lệ đi kèm dòng sai: cả lần gửi bị từ chối.
		{lines(ml("MGD", 2020), ml("MLD", 2030)), "extension.kn_attrs_year_invalid", "lines.MLD.year_made", "một dòng sai"},
	} {
		r := xa.put(path, tc.body)
		want(t, r, http.StatusUnprocessableEntity, tc.code, tc.what)
		fieldErr(t, r, tc.fld)
	}
	if n := f.count("kn_machine_year"); n != before {
		t.Fatalf("lỗi đã ghi sổ: %d → %d", before, n)
	}
	if y := years(xa.get(path)); len(y) != 1 || y["MLD"] != 2010 {
		t.Fatal("hồ sơ đổi sau lỗi")
	}
	r := xa.put("/v1/kn/machine-profiles/"+f.coopInactive.String(), lines(ml("MLD", 2015)))
	want(t, r, http.StatusUnprocessableEntity, "validation_failed", "HTX ngừng hoạt động")
	fieldErr(t, r, "cooperative_ref")
	if n := f.count("kn_machine_year"); n != before {
		t.Fatalf("HTX ngừng hoạt động đã ghi sổ: %d", n)
	}
}

// TT-10 phạm vi, vai, phòng ban, tenant, phiên.
func TestTT10_MachineYearScopeRolesSession(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	path := "/v1/kn/machine-profiles/" + f.coop.String()
	want(t, xa.put(path, lines(ml("MLD", 2015))), http.StatusOK, "", "Cấp Xã ghi")
	before := f.count("kn_machine_year")

	// HTX ngoài xã phụ trách, tổ chức không phải HTX, mã lạ → 404 (không lộ tồn tại).
	for _, ref := range []string{f.coopOther.String(), f.team.String(), ids.New().String(), "khong-phai-uuid"} {
		want(t, xa.get("/v1/kn/machine-profiles/"+ref), http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "đọc "+ref)
		want(t, xa.put("/v1/kn/machine-profiles/"+ref, lines(ml("MLD", 2015))), http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "ghi "+ref)
	}
	// Cấp Tỉnh xem được, không ghi; Cấp Quốc gia chỉ xem.
	tinh := f.user(f.tenant, authz.KNTinh, dept, "31117", "31120")
	if y := years(tinh.get(path)); y["MLD"] != 2015 {
		t.Fatal("Cấp Tỉnh không đọc được hồ sơ")
	}
	want(t, tinh.put(path, lines(ml("MLD", 2016))), http.StatusForbidden, "forbidden", "Cấp Tỉnh ghi")
	tw := f.user(f.tenant, authz.KNTW, dept)
	want(t, tw.get(path), http.StatusOK, "", "TW đọc")
	want(t, tw.put(path, lines(ml("MLD", 2016))), http.StatusForbidden, "extension.read_only", "TW ghi")
	// Admin ghi theo phòng ban chọn; mặc định trong_trot.
	admin := f.user(f.tenant, authz.KNAdmin, "")
	if y := years(admin.get(path)); y["MLD"] != 2015 {
		t.Fatal("Admin đọc phòng ban mặc định")
	}
	want(t, admin.put(path+"?department=chan_nuoi", lines(ml("MGD", 2020))), http.StatusOK, "", "Admin ghi phòng khác")
	want(t, admin.get(path+"?department=Sai-Ma"), http.StatusUnprocessableEntity, "validation_failed", "phòng ban sai mã")
	// Phòng ban khác của xã không thấy hồ sơ trồng trọt.
	cn := f.user(f.tenant, authz.KNXa, "chan_nuoi", "31117")
	if y := years(cn.get(path)); len(y) != 1 || y["MGD"] != 2020 {
		t.Fatal("phòng ban tách hồ sơ")
	}
	if y := years(xa.get(path)); len(y) != 1 || y["MLD"] != 2015 {
		t.Fatal("hồ sơ trồng trọt bị phòng khác đổi")
	}
	// Tenant khác cùng mã xã: HTX không tồn tại.
	stranger := f.user(ids.New(), authz.KNXa, dept, "31117")
	want(t, stranger.get(path), http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "tenant khác đọc")
	want(t, stranger.put(path, lines(ml("MLD", 2001))), http.StatusNotFound, "extension.kn_attrs_cooperative_not_found", "tenant khác ghi")
	// Thiếu ngữ cảnh phiên → 428; phiên bị thu hồi → 403.
	noCtx := xa
	noCtx.noCtx = true
	want(t, noCtx.put(path, lines(ml("MLD", 2002))), http.StatusPreconditionRequired, "", "thiếu X-AG-Context")
	gone := f.user(f.tenant, authz.KNXa, dept, "31117")
	gone.revoke()
	if r := gone.put(path, lines(ml("MLD", 2003))); r.status != http.StatusForbidden {
		t.Fatalf("phiên thu hồi: %d %s", r.status, r.raw)
	}
	if n := f.count("kn_machine_year"); n != before+1 {
		t.Fatalf("chỉ Admin ghi thêm 1 dòng: %d → %d", before, n)
	}
}

// TT-11: cây trồng bắt buộc, danh mục đang dùng, lưu và trả lại thật; không đổi → không ghi sổ.
func TestTT11_PlotCropSavedAndReadBack(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	path := "/v1/kn/plot-profiles/" + f.plot.String()
	g := xa.get(path)
	want(t, g, http.StatusOK, "", "chưa có cây trồng")
	if g.body["crop_code"] != nil || str(g.body, "commune_code") != "31117" || str(g.body, "status") != "active" {
		t.Fatalf("thửa chưa có cây trồng: %s", g.raw)
	}
	for _, tc := range []struct {
		body      map[string]any
		code, fld string
		what      string
	}{
		{map[string]any{}, "validation_failed", "crop_code", "thiếu cây trồng"},
		{map[string]any{"crop_code": "  "}, "validation_failed", "crop_code", "cây trồng rỗng"},
		{map[string]any{"crop_code": "sau_rieng"}, "extension.kn_attrs_crop_unknown", "crop_code", "cây lạ"},
		{map[string]any{"crop_code": "cu"}, "extension.kn_attrs_crop_unknown", "crop_code", "cây ngừng dùng"},
		{map[string]any{"crop_code": "lua; drop"}, "extension.kn_attrs_crop_unknown", "crop_code", "mã sai dạng"},
	} {
		r := xa.put(path, tc.body)
		want(t, r, http.StatusUnprocessableEntity, tc.code, tc.what)
		fieldErr(t, r, tc.fld)
	}
	if n := f.count("kn_plot_crop"); n != 0 {
		t.Fatalf("lỗi đã ghi sổ: %d", n)
	}
	r := xa.put(path, map[string]any{"crop_code": "lua"})
	want(t, r, http.StatusOK, "", "đặt cây trồng")
	if str(r.body, "crop_code") != "lua" || str(r.body, "crop_name") != "Lúa" {
		t.Fatalf("sau đặt: %s", r.raw)
	}
	if g := xa.get(path); str(g.body, "crop_code") != "lua" || str(g.body, "updated_at") == "" {
		t.Fatalf("đọc lại: %s", g.raw)
	}
	want(t, xa.put(path, map[string]any{"crop_code": "lua"}), http.StatusOK, "", "gửi lại")
	if n := f.count("kn_plot_crop"); n != 1 {
		t.Fatalf("gửi lại ghi sổ: %d", n)
	}
	want(t, xa.put(path, map[string]any{"crop_code": "ngo"}), http.StatusOK, "", "đổi cây")
	if g := xa.get(path); str(g.body, "crop_code") != "ngo" {
		t.Fatalf("đọc lại sau đổi: %s", g.raw)
	}
	// Thửa nháp (chưa gán HTX) cũng có cây trồng.
	want(t, xa.put("/v1/kn/plot-profiles/"+f.plotDraft.String(), map[string]any{"crop_code": "lua"}), http.StatusOK, "", "thửa nháp")
	l := xa.get("/v1/kn/plot-profiles?commune=31117")
	want(t, l, http.StatusOK, "", "danh sách")
	got := map[string]string{}
	for _, it := range items(l) {
		got[str(it, "plot_ref")] = str(it, "crop_code")
	}
	if len(got) != 2 || got[f.plot.String()] != "ngo" || got[f.plotDraft.String()] != "lua" {
		t.Fatalf("danh sách cây trồng hiện hành: %s", l.raw)
	}
	p1 := xa.get("/v1/kn/plot-profiles?limit=1")
	if len(items(p1)) != 1 || p1.body["next_cursor"] == nil {
		t.Fatalf("trang 1: %s", p1.raw)
	}
	p2 := xa.get("/v1/kn/plot-profiles?limit=1&cursor=" + str(p1.body, "next_cursor"))
	if len(items(p2)) != 1 || p2.body["next_cursor"] != nil || str(items(p2)[0], "plot_ref") == str(items(p1)[0], "plot_ref") {
		t.Fatalf("trang 2: %s", p2.raw)
	}
	if n := f.count("kn_plot_crop"); n != 3 {
		t.Fatalf("sổ cây trồng: %d", n)
	}
}

// TT-11 phạm vi, trạng thái, vai, phòng ban, tenant, phiên — lỗi không ghi sổ.
func TestTT11_PlotCropScopeRolesSession(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	path := "/v1/kn/plot-profiles/" + f.plot.String()
	want(t, xa.put(path, map[string]any{"crop_code": "lua"}), http.StatusOK, "", "Cấp Xã ghi")
	for _, ref := range []string{f.plotOther.String(), ids.New().String(), "x"} {
		want(t, xa.get("/v1/kn/plot-profiles/"+ref), http.StatusNotFound, "extension.kn_attrs_plot_not_found", "đọc "+ref)
		want(t, xa.put("/v1/kn/plot-profiles/"+ref, map[string]any{"crop_code": "lua"}), http.StatusNotFound, "extension.kn_attrs_plot_not_found", "ghi "+ref)
	}
	want(t, xa.get("/v1/kn/plot-profiles?commune=31120"), http.StatusForbidden, "extension.out_of_scope", "lọc xã khác")
	want(t, xa.put("/v1/kn/plot-profiles/"+f.plotRet.String(), map[string]any{"crop_code": "lua"}), http.StatusConflict, "extension.kn_attrs_plot_retired", "thửa ngừng dùng")
	want(t, xa.get("/v1/kn/plot-profiles/"+f.plotRet.String()), http.StatusOK, "", "thửa ngừng dùng vẫn đọc")

	tinh := f.user(f.tenant, authz.KNTinh, dept, "31117", "31120")
	if g := tinh.get(path); str(g.body, "crop_code") != "lua" {
		t.Fatalf("Cấp Tỉnh đọc: %s", g.raw)
	}
	want(t, tinh.put(path, map[string]any{"crop_code": "ngo"}), http.StatusForbidden, "forbidden", "Cấp Tỉnh ghi")
	tw := f.user(f.tenant, authz.KNTW, dept)
	want(t, tw.put(path, map[string]any{"crop_code": "ngo"}), http.StatusForbidden, "extension.read_only", "TW ghi")
	cn := f.user(f.tenant, authz.KNXa, "chan_nuoi", "31117")
	if g := cn.get(path); g.status != http.StatusOK || g.body["crop_code"] != nil {
		t.Fatalf("phòng ban khác thấy cây trồng: %s", g.raw)
	}
	if l := cn.get("/v1/kn/plot-profiles"); len(items(l)) != 0 {
		t.Fatalf("phòng ban khác thấy danh sách: %s", l.raw)
	}
	stranger := f.user(ids.New(), authz.KNXa, dept, "31117")
	want(t, stranger.get(path), http.StatusNotFound, "extension.kn_attrs_plot_not_found", "tenant khác đọc")
	want(t, stranger.put(path, map[string]any{"crop_code": "ngo"}), http.StatusNotFound, "extension.kn_attrs_plot_not_found", "tenant khác ghi")
	if l := stranger.get("/v1/kn/plot-profiles"); len(items(l)) != 0 {
		t.Fatalf("tenant khác thấy danh sách: %s", l.raw)
	}
	noCtx := xa
	noCtx.noCtx = true
	want(t, noCtx.put(path, map[string]any{"crop_code": "ngo"}), http.StatusPreconditionRequired, "", "thiếu X-AG-Context")
	gone := f.user(f.tenant, authz.KNXa, dept, "31117")
	gone.revoke()
	if r := gone.put(path, map[string]any{"crop_code": "ngo"}); r.status != http.StatusForbidden {
		t.Fatalf("phiên thu hồi: %d %s", r.status, r.raw)
	}
	if n := f.count("kn_plot_crop"); n != 1 {
		t.Fatalf("lỗi đã ghi sổ: %d", n)
	}
	if g := xa.get(path); str(g.body, "crop_code") != "lua" {
		t.Fatalf("cây trồng đổi sau lỗi: %s", g.raw)
	}
}

// Hồ sơ KN không ghi bảng HTX: số dòng nguồn HTX/thửa của tenant giữ nguyên sau mọi thao tác ghi KN.
func TestKNAttrsNeverWritesHTX(t *testing.T) {
	f := newFix(t)
	snap := func() string {
		var s string
		if err := pgtest.Get(t).AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT
		    (SELECT count(*) FROM pf_party.org WHERE tenant_id = $1)::text || '/' ||
		    (SELECT coalesce(md5(string_agg(to_jsonb(p)::text, '|' ORDER BY ref)), '') FROM pf_coop.plot p WHERE tenant_id = $1) || '/' ||
		    (SELECT count(*) FROM pf_coop.cooperative WHERE tenant_id = $1)::text`, f.tenant).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snap()
	xa := f.user(f.tenant, authz.KNXa, dept, "31117")
	want(t, xa.put("/v1/kn/machine-profiles/"+f.coop.String(), lines(ml("MLD", 2015))), http.StatusOK, "", "năm máy")
	want(t, xa.put("/v1/kn/plot-profiles/"+f.plot.String(), map[string]any{"crop_code": "lua"}), http.StatusOK, "", "cây trồng")
	if after := snap(); after != before {
		t.Fatalf("dữ liệu HTX đổi: %s → %s", before, after)
	}
}
