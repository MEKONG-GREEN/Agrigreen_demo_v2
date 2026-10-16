package handler_test

// W11.61 (D4 KH1, BRD KHTC v2.2 FN-AD-04.01, FN-TI-02.02): Danh mục KHTC, danh sách CT/DA chủ (D20), Kế hoạch năm có
// phiên bản theo QĐ trên PostgreSQL thật qua module pf/extension (vai extension_app). Ngữ cảnh phiên KN giả lập bằng
// middleware X-AG-Context (như extension/material).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const khtc = "ke_hoach_tai_chinh"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	mod    *extension.Module
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	f.mod = extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
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

func (f *fix) user(role, department string, viewOnly bool) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: department, Scope: authz.Scope{All: true}, OrgRef: ids.New(), ViewOnly: viewOnly}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
	header http.Header
}

func (u user) send(method, path, idem, ctype string, body io.Reader) resp {
	u.f.t.Helper()
	r, _ := http.NewRequest(method, u.f.srv.URL+path, body)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", ctype)
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // số giữ dạng chuỗi (R-10: không float trong vùng KHTC, cả kiểm thử)
	_ = dec.Decode(&m)
	return resp{status: res.StatusCode, raw: raw, body: m, header: res.Header}
}

func (u user) do(method, path, idem string, body any) resp {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	return u.send(method, path, idem, "application/json", rd)
}

func key() string                        { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp { return u.do(http.MethodPost, p, key(), b) }
func (u user) get(p string) resp         { return u.do(http.MethodGet, p, "", nil) }
func str(m any, k string) string         { mm, _ := m.(map[string]any); s, _ := mm[k].(string); return s }
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

// FN-AD-04.01: Admin quản lý Danh mục KHTC, KHTC xem; trùng mã chặn; một mẫu một người ký mặc định đang dùng; ngừng dùng
// = phiên bản mới (lịch sử giữ).
func TestFN_AD_04_01_Catalog(t *testing.T) {
	f := newFix(t)
	admin := f.user(authz.KNAdmin, "", false)
	tinh := f.user(authz.KNTinh, khtc, false)
	xa := f.user(authz.KNXa, khtc, false)

	want(t, admin.post("/v1/kn/khtc/catalogs/unit", map[string]any{"code": "ha", "name": "Héc ta"}), http.StatusCreated, "", "thêm ĐVT")
	want(t, admin.post("/v1/kn/khtc/catalogs/unit", map[string]any{"code": "HA", "name": "Ha"}), http.StatusConflict, "extension.khtc_catalog_exists", "trùng mã")
	want(t, tinh.post("/v1/kn/khtc/catalogs/unit", map[string]any{"code": "con", "name": "Con"}), http.StatusForbidden, "", "KHTC không sửa danh mục")
	want(t, xa.get("/v1/kn/khtc/catalogs"), http.StatusForbidden, "", "Cấp Xã không có chức năng KHTC")

	signer := map[string]any{"code": "gd", "name": "Giám đốc A", "default_for": []string{"mau_1", "mau_2"}, "attrs": map[string]string{"title": "Giám đốc"}}
	k := key()
	r := admin.do(http.MethodPost, "/v1/kn/khtc/catalogs/signer", k, signer)
	want(t, r, http.StatusCreated, "", "người ký")
	if r2 := admin.do(http.MethodPost, "/v1/kn/khtc/catalogs/signer", k, signer); r2.status >= 300 || str(r2.body, "version_id") != str(r.body, "version_id") {
		t.Fatalf("gửi lặp cùng khóa: %s", r2.raw)
	}
	other := map[string]any{"code": "pgd", "name": "Phó giám đốc B", "default_for": []string{"mau_2"}, "attrs": map[string]string{"title": "Phó giám đốc"}}
	want(t, admin.post("/v1/kn/khtc/catalogs/signer", other), http.StatusConflict, "extension.khtc_default_conflict", "hai người ký mặc định")
	other["default_for"] = []string{"mau_3"}
	want(t, admin.post("/v1/kn/khtc/catalogs/signer", other), http.StatusCreated, "", "người ký mẫu 3")

	off := false
	r = admin.post("/v1/kn/khtc/catalogs/unit/ha/revise", map[string]any{"name": "Héc ta", "active": off})
	want(t, r, http.StatusCreated, "", "ngừng dùng")
	if r.body["version"].(json.Number).String() != "2" || r.body["active"] != false {
		t.Fatalf("phiên bản ngừng: %s", r.raw)
	}
	r = tinh.get("/v1/kn/khtc/catalogs?kind=unit")
	want(t, r, http.StatusOK, "", "KHTC xem danh mục")
	if len(objs(r.body["items"])) != 0 {
		t.Fatalf("mục đã ngừng vẫn hiện: %s", r.raw)
	}
	counts := objs(r.body["counts"])
	if len(counts) != 7 || str(counts[1], "kind") != "unit" || counts[1]["total"].(json.Number).String() != "1" || counts[4]["active"].(json.Number).String() != "2" {
		t.Fatalf("số lượng: %s", r.raw)
	}
	r = tinh.get("/v1/kn/khtc/catalogs/unit/ha/history")
	if len(objs(r.body["items"])) != 2 {
		t.Fatalf("lịch sử: %s", r.raw)
	}
}

func seedCatalog(t *testing.T, admin user) {
	for _, c := range []struct{ kind, code, name string }{{"unit", "ha", "Héc ta"}, {"unit", "mo_hinh", "Mô hình"},
		{"field", "trong_trot", "Trồng trọt"}, {"funding_source", "nstinh", "Ngân sách tỉnh"}} {
		want(t, admin.post("/v1/kn/khtc/catalogs/"+c.kind, map[string]any{"code": c.code, "name": c.name}), http.StatusCreated, "", "danh mục "+c.code)
	}
}

func program(t *testing.T, u user, code, name, kind string) string {
	r := u.post("/v1/kn/khtc/programs", map[string]any{"code": code, "name": name, "kind": kind, "field_code": "trong_trot"})
	want(t, r, http.StatusCreated, "", "thêm CT/DA "+code)
	return str(r.body, "ref")
}

// D20 + FN-TI-02.02 B7: KHTC là chủ danh sách CT/DA; trùng mã chặn; phòng khác chỉ xem; cổng KHTCPrograms cho module khác.
func TestD20_ProgramMaster(t *testing.T) {
	f := newFix(t)
	admin := f.user(authz.KNAdmin, "", false)
	seedCatalog(t, admin)
	tinh := f.user(authz.KNTinh, khtc, false)
	cnts := f.user(authz.KNTinh, "chan_nuoi_thuy_san", false)
	lead := f.user(authz.KNTinh, khtc, true)

	ref := program(t, tinh, "CT-01", "Chương trình KN trồng trọt", "program")
	want(t, tinh.post("/v1/kn/khtc/programs", map[string]any{"code": "ct-01", "name": "Trùng", "kind": "program"}), http.StatusConflict,
		"extension.khtc_program_exists", "trùng mã")
	want(t, cnts.post("/v1/kn/khtc/programs", map[string]any{"code": "CT-02", "name": "X", "kind": "program"}), http.StatusForbidden, "", "phòng khác")
	want(t, lead.post("/v1/kn/khtc/programs", map[string]any{"code": "CT-02", "name": "X", "kind": "program"}), http.StatusForbidden,
		"extension.read_only", "lãnh đạo chỉ xem")
	want(t, tinh.post("/v1/kn/khtc/programs", map[string]any{"code": "CT-03", "name": "X", "kind": "program", "field_code": "khong_co"}),
		http.StatusUnprocessableEntity, "", "lĩnh vực không có")
	r := cnts.get("/v1/kn/khtc/programs")
	want(t, r, http.StatusOK, "", "CNTS xem danh sách")
	if len(objs(r.body["items"])) != 1 {
		t.Fatalf("danh sách: %s", r.raw)
	}
	want(t, tinh.post("/v1/kn/khtc/programs/"+ref+"/revise", map[string]any{"code": "CT-01", "name": "CT KN trồng trọt", "kind": "program",
		"active": false}), http.StatusCreated, "", "ngừng CT/DA")

	ctx := tenant.WithID(context.Background(), f.tenant)
	ps := f.mod.KHTCPrograms()
	p, err := ps.Program(ctx, uuid.MustParse(ref))
	if err != nil || p.Active || p.Version != 2 || p.Code != "CT-01" {
		t.Fatalf("cổng Program: %+v %v", p, err)
	}
	if _, err := ps.Program(ctx, ids.New()); !errors.Is(err, port.ErrKHTCProgramNotFound) {
		t.Fatalf("cổng Program không có: %v", err)
	}
	if l, _ := ps.Programs(ctx, port.KHTCProgramFilter{}); len(l) != 0 {
		t.Fatalf("cổng Programs (bỏ CT đã ngừng): %+v", l)
	}
	if l, _ := ps.Programs(ctx, port.KHTCProgramFilter{IncludeRetired: true}); len(l) != 1 {
		t.Fatalf("cổng Programs (kể cả đã ngừng): %+v", l)
	}
}

func planBody(base int, prog1, prog2 string, extra map[string]any) map[string]any {
	p1, g1, i1, p2, i2 := ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()
	b := map[string]any{"base_version": base, "decision_no": "123/QĐ-SNN", "decision_date": "2027-01-05", "entries": []map[string]any{
		{"ref": p1, "tab": "programs", "kind": "program", "program_ref": prog1, "sort_order": 1},
		{"ref": g1, "tab": "programs", "kind": "group", "parent_ref": p1, "name": "Xây dựng mô hình"},
		{"ref": i1, "tab": "programs", "kind": "item", "parent_ref": g1, "name": "Mô hình lúa chất lượng cao", "unit_code": "ha",
			"planned_qty": "120.5", "budget": "350000000", "funding_code": "nstinh", "department_code": "trong_trot", "communes": []string{"30001"}},
		{"ref": p2, "tab": "central_project", "kind": "program", "program_ref": prog2, "name": "Dự án KN TW nuôi tôm"},
		{"ref": i2, "tab": "central_project", "kind": "item", "parent_ref": p2, "name": "Điểm trình diễn", "unit_code": "mo_hinh",
			"planned_qty": "3", "department_code": "chan_nuoi_thuy_san", "data_source": "module", "source_key": "cnts.demo_points"},
	}}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// FN-TI-02.02, BR-DL-13: lập kế hoạch (phiên bản 1, hiệu lực mặc định 01/01), "Điều chỉnh theo QĐ" (bắt buộc số, ngày QĐ,
// ngày hiệu lực) → phiên bản mới, phiên bản cũ xem được; khóa lạc quan; chọn phiên bản theo ngày; cổng KHTCPlans; sao
// chép từ năm trước; xuất Excel và nhập lại thành bản nháp.
func TestFN_TI_02_02_AnnualPlan(t *testing.T) {
	f := newFix(t)
	admin := f.user(authz.KNAdmin, "", false)
	seedCatalog(t, admin)
	tinh := f.user(authz.KNTinh, khtc, false)
	tt := f.user(authz.KNTinh, "trong_trot", false)
	prog1 := program(t, tinh, "CT-I", "Chương trình KN trồng trọt", "program")
	prog2 := program(t, tinh, "DA-TW-01", "Dự án KN TW", "central_project")

	want(t, tinh.get("/v1/kn/khtc/plans/2027"), http.StatusNotFound, "extension.khtc_plan_not_found", "chưa có kế hoạch")
	want(t, tt.post("/v1/kn/khtc/plans/2027/versions", planBody(0, prog1, prog2, nil)), http.StatusForbidden, "", "phòng chuyên môn không lập")
	bad := planBody(0, prog1, prog2, nil)
	bad["entries"].([]map[string]any)[2]["department_code"] = ""
	r := tinh.post("/v1/kn/khtc/plans/2027/versions", bad)
	want(t, r, http.StatusUnprocessableEntity, "extension.khtc_plan_invalid", "hạng mục thiếu phòng phụ trách")

	k, first := key(), planBody(0, prog1, prog2, nil)
	r = tinh.do(http.MethodPost, "/v1/kn/khtc/plans/2027/versions", k, first)
	want(t, r, http.StatusCreated, "", "lập kế hoạch")
	v1 := r.body["version"].(map[string]any)
	es := objs(r.body["entries"])
	if v1["version"].(json.Number).String() != "1" || str(v1, "basis") != "initial" || str(v1, "effective_from") != "2027-01-01" || len(es) != 5 ||
		str(es[0], "name") != "Chương trình KN trồng trọt" || str(es[0], "roman") != "I" || str(es[2], "planned_qty") != "120.5" ||
		str(es[2], "program_ref") != prog1 || str(es[2], "program_code") != "CT-I" {
		t.Fatalf("phiên bản 1: %s", r.raw)
	}
	if r2 := tinh.do(http.MethodPost, "/v1/kn/khtc/plans/2027/versions", k, first); r2.status >= 300 || r2.body["latest_version"].(json.Number).String() != "1" {
		t.Fatalf("gửi lặp: %s", r2.raw)
	}

	// Điều chỉnh theo QĐ.
	adj := planBody(1, prog1, prog2, map[string]any{"decision_no": "", "effective_from": "2027-07-01"})
	want(t, tinh.post("/v1/kn/khtc/plans/2027/versions", adj), http.StatusUnprocessableEntity, "extension.khtc_effective_date_invalid", "thiếu số QĐ")
	adj = planBody(1, prog1, prog2, map[string]any{"decision_no": "456/QĐ-SNN", "decision_date": "2027-06-20", "effective_from": "2027-07-01"})
	adj["entries"].([]map[string]any)[2]["planned_qty"] = "150"
	r = tinh.post("/v1/kn/khtc/plans/2027/versions", adj)
	want(t, r, http.StatusCreated, "", "điều chỉnh")
	if r.body["latest_version"].(json.Number).String() != "2" || str(objs(r.body["entries"])[2], "planned_qty") != "150" {
		t.Fatalf("phiên bản 2: %s", r.raw)
	}
	want(t, tinh.post("/v1/kn/khtc/plans/2027/versions", planBody(1, prog1, prog2, map[string]any{"effective_from": "2027-08-01"})),
		http.StatusConflict, "extension.khtc_plan_version_conflict", "khóa lạc quan")

	r = tt.get("/v1/kn/khtc/plans/2027?at=2027-03-01")
	want(t, r, http.StatusOK, "", "phòng chuyên môn xem theo ngày")
	if r.body["version"].(map[string]any)["version"].(json.Number).String() != "1" || str(objs(r.body["entries"])[2], "planned_qty") != "120.5" {
		t.Fatalf("phiên bản theo ngày: %s", r.raw)
	}
	r = tinh.get("/v1/kn/khtc/plans/2027/versions")
	if len(objs(r.body["items"])) != 2 {
		t.Fatalf("lịch sử phiên bản: %s", r.raw)
	}

	ctx := tenant.WithID(context.Background(), f.tenant)
	pl, err := f.mod.KHTCPlans().PlanAt(ctx, 2027, time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || pl.Version != 2 || pl.DecisionNo != "456/QĐ-SNN" || len(pl.Entries) != 5 || pl.Entries[0].Roman != "I" ||
		pl.Entries[2].PlannedQty.String() != "150" || pl.Entries[2].Department != "trong_trot" {
		t.Fatalf("cổng PlanAt: %+v %v", pl, err)
	}
	if _, err := f.mod.KHTCPlans().PlanAt(ctx, 2026, t0); !errors.Is(err, port.ErrKHTCPlanNotFound) {
		t.Fatalf("cổng PlanAt năm chưa có: %v", err)
	}

	// Sao chép từ năm trước → bản nháp (định danh dòng mới).
	r = tinh.get("/v1/kn/khtc/plans/2028/copy-draft")
	want(t, r, http.StatusOK, "", "sao chép")
	drafts := objs(r.body["entries"])
	if len(drafts) != 5 || str(drafts[0], "ref") == str(es[0], "ref") || r.body["from_version"].(json.Number).String() != "2" {
		t.Fatalf("bản nháp sao chép: %s", r.raw)
	}

	// Xuất Excel rồi nhập lại thành bản nháp.
	r = tinh.get("/v1/kn/khtc/plans/2027/export?version=1")
	if r.status != http.StatusOK || !strings.Contains(r.header.Get("Content-Type"), "spreadsheetml") || len(r.raw) < 100 {
		t.Fatalf("xuất Excel: %d %s", r.status, r.header)
	}
	r = tinh.send(http.MethodPost, "/v1/kn/khtc/plans/2027/import-draft", key(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		bytes.NewReader(r.raw))
	want(t, r, http.StatusOK, "", "nhập Excel")
	imp := objs(r.body["entries"])
	if len(imp) != 5 || str(imp[0], "ref") != str(es[0], "ref") || str(imp[2], "parent_ref") != str(es[1], "ref") ||
		str(imp[2], "planned_qty") != "120.5" || str(imp[0], "program_ref") != prog1 || len(objs(r.body["problems"])) != 0 {
		t.Fatalf("nhập lại: %s", r.raw)
	}
}
