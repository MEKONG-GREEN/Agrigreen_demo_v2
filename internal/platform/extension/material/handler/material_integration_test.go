package handler_test

// KN v7.7 FN-XA-03.06 "Nhập thông tin vật tư": sổ pf_extension.material_record trên PostgreSQL thật qua module
// pf/extension (vai extension_app). Ngữ cảnh phiên KN giả lập bằng middleware X-AG-Context (như tests/kn77).

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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const dept = "trong_trot"

// 10:00 thứ Năm 01/10/2026 giờ Việt Nam.
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

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (mdport.AdminUnit, error) {
	parent := "89"
	if strings.HasPrefix(code, "31") {
		parent = "92"
	}
	return mdport.AdminUnit{Code: code, Name: "Xã " + code, Level: mdport.LevelCommune, ParentCode: parent}, nil
}
func (units) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}
func (units) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	item := func(code, name, unit string, active bool) mdport.CatalogItem {
		return mdport.CatalogItem{Catalog: mdport.CatalogMaterial, Code: code, Name: name, Attrs: map[string]string{"unit": unit}, Active: active}
	}
	cat := catalogs{m: map[string]mdport.CatalogItem{
		"material/phan_ure":  item("phan_ure", "Phân urê", "kg", true),
		"material/giong_bao": item("giong_bao", "Giống lúa (bao)", "bao", true),
		"material/cu":        item("cu", "Thuốc cũ", "chai", false),
	}}
	mod := extension.New(env.Pool(t, migrations.PF, extension.DBRole), clock.NewFixed(t0), extension.Deps{Catalogs: cat, Units: units{}})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
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
		kc.Communes, kc.Scope = units, authz.Scope{Units: units}
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

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path, idem string, body any) resp {
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
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
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

func key() string                        { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp { return u.do(http.MethodPost, p, key(), b) }
func (u user) get(p string) resp         { return u.do(http.MethodGet, p, "", nil) }
func str(m any, k string) string         { mm, _ := m.(map[string]any); s, _ := mm[k].(string); return s }
func items(r resp) []map[string]any      { return objs(r.body["items"]) }
func objs(v any) []map[string]any {
	l, _ := v.([]any)
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

func lines(ls ...map[string]any) map[string]any {
	return map[string]any{"commune_code": "30001", "recorded_on": "2026-09-30", "lines": ls}
}

func line(code, qty string) map[string]any {
	return map[string]any{"material_code": code, "quantity": qty}
}

func TestFN_XA_03_06_RecordCorrectLogIdempotent(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "30001")
	k := key()
	body := lines(line("phan_ure", "120.5"), map[string]any{"material_code": "giong_bao", "quantity": "10", "note": "Hỗ trợ vụ Đông Xuân"})
	r := xa.do(http.MethodPost, "/v1/kn/materials", k, body)
	want(t, r, http.StatusCreated, "", "ghi nhận")
	recs := items(r)
	if len(recs) != 2 || str(recs[0], "unit") != "kg" || str(recs[1], "unit") != "bao" || str(recs[0], "quantity") != "120.5" ||
		str(recs[0], "material_name") != "Phân urê" || str(recs[0], "recorded_on") != "2026-09-30" {
		t.Fatalf("bản ghi: %s", r.raw)
	}
	// POST lặp cùng khóa → cùng kết quả, không ghi thêm.
	r2 := xa.do(http.MethodPost, "/v1/kn/materials", k, body)
	if r2.status/100 != 2 || len(items(r2)) != 2 || str(items(r2)[0], "id") != str(recs[0], "id") {
		t.Fatalf("POST lặp: %d %s", r2.status, r2.raw)
	}
	if l := xa.get("/v1/kn/materials?commune=30001"); len(items(l)) != 2 {
		t.Fatalf("danh sách sau POST lặp: %s", l.raw)
	}
	id := str(recs[0], "id")
	c := xa.post("/v1/kn/materials/"+id+"/correct", map[string]any{"quantity": "100", "reason": "Nhập nhầm số lượng phân"})
	want(t, c, http.StatusCreated, "", "sửa")
	if str(c.body, "supersedes_id") != id || str(c.body, "quantity") != "100" {
		t.Fatalf("bản ghi bù: %s", c.raw)
	}
	// Bản cũ hết hiện hành; sửa lại bản cũ → 409.
	want(t, xa.post("/v1/kn/materials/"+id+"/correct", map[string]any{"quantity": "90", "reason": "Sửa bản đã bị thay thế"}),
		http.StatusConflict, "extension.material_superseded", "sửa bản không hiện hành")
	l := xa.get("/v1/kn/materials?commune=30001&material_code=phan_ure")
	if its := items(l); len(its) != 1 || str(its[0], "id") != str(c.body, "id") {
		t.Fatalf("chỉ bản hiện hành: %s", l.raw)
	}
	lg := xa.get("/v1/kn/materials/" + id + "/log")
	want(t, lg, http.StatusOK, "", "log")
	its := items(lg)
	if len(its) != 2 || str(its[0], "event") != "corrected" || str(its[0], "reason") != "Nhập nhầm số lượng phân" || str(its[1], "event") != "recorded" {
		t.Fatalf("log: %s", lg.raw)
	}
	ch := objs(its[0]["changes"])
	if len(ch) == 0 || str(ch[0], "field") != "quantity" || str(ch[0], "old") != "120.5" || str(ch[0], "new") != "100" {
		t.Fatalf("log thay đổi: %s", lg.raw)
	}
	sm := xa.get("/v1/kn/materials/summary?commune=30001")
	want(t, sm, http.StatusOK, "", "tổng hợp")
	got := map[string]string{}
	for _, it := range items(sm) {
		got[str(it, "material_code")] = str(it, "quantity")
	}
	if got["phan_ure"] != "100" || got["giong_bao"] != "10" {
		t.Fatalf("tổng hợp bản hiện hành: %s", sm.raw)
	}
}

func TestFN_XA_03_06_QuantityUnitAndValidation(t *testing.T) {
	f := newFix(t)
	xa := f.user(f.tenant, authz.KNXa, dept, "30001")
	for _, tc := range []struct{ code, qty, what string }{
		{"giong_bao", "2.5", "ĐVT đếm được không nguyên"},
		{"phan_ure", "-1", "số âm"},
		{"phan_ure", "1.12345", "quá 4 chữ số lẻ"},
		{"phan_ure", "1000000001", "vượt 1e9"},
	} {
		want(t, xa.post("/v1/kn/materials", lines(line(tc.code, tc.qty))), http.StatusUnprocessableEntity, "extension.material_quantity_invalid", tc.what)
	}
	want(t, xa.post("/v1/kn/materials", lines(line("phan_ure", "1.1234"))), http.StatusCreated, "", "4 chữ số lẻ")
	want(t, xa.post("/v1/kn/materials", lines(line("cu", "1"))), http.StatusUnprocessableEntity, "extension.material_unknown", "vật tư ngừng dùng")
	want(t, xa.post("/v1/kn/materials", lines(line("khong_co", "1"))), http.StatusUnprocessableEntity, "extension.material_unknown", "vật tư lạ")
	b := lines(map[string]any{"material_code": "phan_ure", "quantity": "1", "note": "Liên hệ 0912345678"})
	want(t, xa.post("/v1/kn/materials", b), http.StatusUnprocessableEntity, "validation_failed", "ghi chú có SĐT")
	b = lines(line("phan_ure", "1"))
	b["recorded_on"] = "2026-10-02"
	want(t, xa.post("/v1/kn/materials", b), http.StatusUnprocessableEntity, "validation_failed", "ngày tương lai")
	b["lines"] = []any{}
	b["recorded_on"] = "2026-10-01"
	want(t, xa.post("/v1/kn/materials", b), http.StatusUnprocessableEntity, "validation_failed", "không có dòng")
	r := xa.post("/v1/kn/materials", lines(line("giong_bao", "3")))
	want(t, r, http.StatusCreated, "", "bao nguyên")
	want(t, xa.post("/v1/kn/materials/"+str(items(r)[0], "id")+"/correct", map[string]any{"quantity": "3.5", "reason": "Sửa số bao giống"}),
		http.StatusUnprocessableEntity, "extension.material_quantity_invalid", "sửa sai ĐVT")
	want(t, xa.post("/v1/kn/materials/"+str(items(r)[0], "id")+"/correct", map[string]any{"quantity": "4", "reason": "ngắn"}),
		http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
}

func TestFN_XA_03_06_ScopeRolesDepartmentTenantSummary(t *testing.T) {
	f := newFix(t)
	xa1 := f.user(f.tenant, authz.KNXa, dept, "30001")
	xa2 := f.user(f.tenant, authz.KNXa, dept, "30002")
	r1 := xa1.post("/v1/kn/materials", lines(line("phan_ure", "50")))
	want(t, r1, http.StatusCreated, "", "xã 1")
	b := lines(line("phan_ure", "30"))
	b["commune_code"] = "30002"
	want(t, xa2.post("/v1/kn/materials", b), http.StatusCreated, "", "xã 2")
	b2 := lines(line("phan_ure", "7"))
	b2["commune_code"] = "31117" // tỉnh 92
	admin := f.user(f.tenant, authz.KNAdmin, "")
	want(t, admin.post("/v1/kn/materials", b2), http.StatusCreated, "", "Admin ghi tỉnh khác")

	// Xã khác: 403 khi ghi/lọc, 404 khi đọc bản ghi.
	want(t, xa2.post("/v1/kn/materials", lines(line("phan_ure", "1"))), http.StatusForbidden, "extension.out_of_scope", "ghi xã khác")
	want(t, xa2.get("/v1/kn/materials?commune=30001"), http.StatusForbidden, "extension.out_of_scope", "lọc xã khác")
	id1 := str(items(r1)[0], "id")
	want(t, xa2.get("/v1/kn/materials/"+id1+"/log"), http.StatusNotFound, "extension.material_record_not_found", "đọc bản ghi xã khác")
	want(t, xa2.post("/v1/kn/materials/"+id1+"/correct", map[string]any{"quantity": "1", "reason": "Sửa bản ghi xã khác"}),
		http.StatusNotFound, "extension.material_record_not_found", "sửa bản ghi xã khác")
	if l := xa2.get("/v1/kn/materials"); len(items(l)) != 1 {
		t.Fatalf("xã 2 chỉ thấy xã mình: %s", l.raw)
	}

	// AC: Cấp Tỉnh xem tổng hợp theo xã trong tỉnh (chỉ xem).
	tinh := f.user(f.tenant, authz.KNTinh, dept, "89", "30001", "30002")
	sm := tinh.get("/v1/kn/materials/summary")
	want(t, sm, http.StatusOK, "", "tổng hợp tỉnh")
	byCommune := map[string]string{}
	for _, it := range items(sm) {
		byCommune[str(it, "commune_code")] = str(it, "quantity")
	}
	if len(byCommune) != 2 || byCommune["30001"] != "50" || byCommune["30002"] != "30" {
		t.Fatalf("tổng hợp theo xã của tỉnh: %s", sm.raw)
	}
	want(t, tinh.post("/v1/kn/materials", lines(line("phan_ure", "1"))), http.StatusForbidden, "forbidden", "Cấp Tỉnh ghi")
	tw := f.user(f.tenant, authz.KNTW, dept)
	if l := tw.get("/v1/kn/materials"); l.status != http.StatusOK || len(items(l)) != 3 {
		t.Fatalf("TW xem toàn quốc: %s", l.raw)
	}
	want(t, tw.post("/v1/kn/materials", lines(line("phan_ure", "1"))), http.StatusForbidden, "extension.read_only", "TW ghi")
	want(t, tw.post("/v1/kn/materials/"+id1+"/correct", map[string]any{"quantity": "1", "reason": "TW thử sửa bản ghi"}),
		http.StatusForbidden, "extension.read_only", "TW sửa")

	// Phòng ban khác không thấy; tenant khác không thấy.
	bvtv := f.user(f.tenant, authz.KNXa, "bvtv", "30001")
	if l := bvtv.get("/v1/kn/materials"); len(items(l)) != 0 {
		t.Fatalf("phòng ban khác thấy vật tư: %s", l.raw)
	}
	want(t, bvtv.get("/v1/kn/materials/"+id1+"/log"), http.StatusNotFound, "extension.material_record_not_found", "phòng ban khác đọc")
	stranger := f.user(ids.New(), authz.KNXa, dept, "30001")
	if l := stranger.get("/v1/kn/materials"); len(items(l)) != 0 {
		t.Fatalf("tenant khác thấy vật tư: %s", l.raw)
	}
	want(t, stranger.get("/v1/kn/materials/"+id1+"/log"), http.StatusNotFound, "extension.material_record_not_found", "tenant khác đọc")
}
