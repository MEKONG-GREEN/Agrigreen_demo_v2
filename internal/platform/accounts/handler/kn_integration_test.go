package handler_test

// KN v7.7 gói P1: gán vai trò nhiều vai × Nút × phòng ban × xã, ngữ cảnh phiên X-AG-Context, chế độ hỗ
// trợ của Admin (FN-CH-00.01, FN-AD-04.04, FN-AD-04.07).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fakeDepartments thực hiện party/port.Departments: đơn vị = Nút cấp 1/2 gần nhất (fakeParty không có
// cây nên khai trực tiếp), phòng ban theo đơn vị (P2 thật có kiểm thử riêng).
type fakeDepartments struct {
	mu    sync.Mutex
	unit  map[uuid.UUID]uuid.UUID
	depts map[uuid.UUID][]partyport.Department
}

func (f *fakeDepartments) DepartmentUnit(_ context.Context, org uuid.UUID) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.unit[org]
	if !ok {
		return uuid.Nil, partyport.ErrOrgNotFound
	}
	return u, nil
}

func (f *fakeDepartments) UnitDepartments(_ context.Context, unit uuid.UUID) ([]partyport.Department, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.depts[unit]), nil
}

func (f *fakeDepartments) DepartmentByCode(_ context.Context, unit uuid.UUID, code string) (partyport.Department, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.depts[unit] {
		if d.Code == code && !d.Retired {
			return d, nil
		}
	}
	return partyport.Department{}, partyport.ErrDepartmentNotFound
}

func (f *fakeDepartments) setStatus(unit uuid.UUID, code, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, d := range f.depts[unit] {
		if d.Code == code {
			f.depts[unit][i].Status = status
		}
	}
}

type knFix struct {
	*fixture
	app, admin2 *httptest.Server
	depts       *fakeDepartments
	kmod        *accounts.Module
	// Admin thật (có trong Keycloak giả để khung Log tra tên).
	rootID, accAdminID uuid.UUID
	root, accAdmin     caller
	mu                 sync.Mutex
	probe              []authz.Principal
}

const (
	deptTT   = "trong_trot"
	deptCN   = "chan_nuoi_thuy_san" // Chưa mở
	deptBVTV = "bao_ve_thuc_vat"    // Đang áp dụng — cho kiểm "hai Cán bộ xã khác phòng ban cùng xã"
)

func newKNFixture(t *testing.T, registries ...*authz.SessionRegistry) *knFix {
	sessions := fixtureSessionRegistry(t)
	if len(registries) > 1 {
		t.Fatal("one registry override expected")
	}
	if len(registries) == 1 {
		sessions = registries[0]
	}
	base := newFixture(t)
	f := &knFix{fixture: base, depts: &fakeDepartments{unit: map[uuid.UUID]uuid.UUID{}, depts: map[uuid.UUID][]partyport.Department{}}}
	f.depts.unit[f.p91], f.depts.unit[f.team30292], f.depts.unit[f.national] = f.p91, f.p91, f.national
	f.depts.unit[f.p92] = f.p92 // tỉnh 92 chưa khai phòng ban → tiên quyết UR-AD-04
	mk := func(unit uuid.UUID, code, name, status string) partyport.Department {
		return partyport.Department{ID: ids.New(), OrgRef: unit, Code: code, Name: name, Status: status}
	}
	f.depts.depts[f.p91] = []partyport.Department{mk(f.p91, deptTT, "Phòng Trồng trọt", partyport.DeptActive),
		mk(f.p91, deptCN, "Phòng Chăn nuôi - Thủy sản", partyport.DeptNotOpen), mk(f.p91, deptBVTV, "Phòng Bảo vệ thực vật", partyport.DeptActive)}
	f.depts.depts[f.national] = []partyport.Department{mk(f.national, deptTT, "Phòng Trồng trọt", partyport.DeptActive)}

	ops := f.env.Pool(t, migrations.PF, "platform_api")
	f.kmod = accounts.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{Directory: f.dir, Scope: f.party, Orgs: f.party, Departments: f.depts, Sessions: sessions})
	log := telemetry.NewLogger("test", io.Discard)
	app := httpx.NewEngine(log, 1<<20)
	ag := app.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.kmod.Middleware(), idempotency.Middleware(ops))
	f.kmod.RegisterApp(ag)
	// Route thử của "module khác": đọc Principal (KNContext) mà middleware dựng.
	record := func(c *gin.Context) {
		p, _ := authz.FromContext(c.Request.Context())
		f.mu.Lock()
		f.probe = append(f.probe, p)
		f.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
	ag.GET("/probe", record)
	ag.POST("/probe/:id/write", record)
	f.app = httptest.NewServer(app)
	t.Cleanup(f.app.Close)
	adm := httpx.NewEngine(log, 1<<20)
	f.kmod.RegisterAdmin(adm.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), f.kmod.Middleware(), idempotency.Middleware(ops)))
	f.admin2 = httptest.NewServer(adm)
	t.Cleanup(f.admin2.Close)

	f.rootID = f.dir.Seed(port.NewUser{Username: "admin.mekong", Email: "admin.mekong@vi-du.vn", FirstName: "Mekong", LastName: "Quản trị",
		TenantID: f.tenant}, true, "platform_admin")
	f.root = f.tok(f.rootID, f.tenant, true, "platform_admin")
	f.accAdminID = f.dir.Seed(port.NewUser{Username: "qt.taikhoan", Email: "qt.taikhoan@vi-du.vn", FirstName: "Hà", LastName: "Lê Thu",
		TenantID: f.tenant, OrgRef: f.national}, true, port.RoleAccountAdmin)
	f.accAdmin = caller{f: f.fixture, actor: f.accAdminID, token: f.iss.Sign(t, authztest.Token{Subject: f.accAdminID, TenantID: f.tenant,
		Roles: []string{port.RoleAccountAdmin}, MFA: true, OrgRef: f.national})}
	return f
}

func (f *knFix) tok(sub, tid uuid.UUID, mfa bool, roles ...string) caller {
	return caller{f: f.fixture, actor: sub, token: f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: tid, Roles: roles, MFA: mfa})}
}

// req gọi một bề mặt (srv) với header tùy chọn.
func (f *knFix) req(srv *httptest.Server, c caller, method, path string, body any, hdr map[string]string) resp {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		r.Header.Set("Idempotency-Key", key())
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp{status: res.StatusCode, hdr: res.Header, body: m}
}

func (f *knFix) adminDo(c caller, method, path string, body any) resp {
	return f.req(f.admin2, c, method, path, body, nil)
}

func asg(role string, org uuid.UUID, depts []string, communes ...string) map[string]any {
	m := map[string]any{"role": role, "department_codes": depts, "commune_codes": communes, "mgmt_level": "chuyen_vien"}
	if org != uuid.Nil {
		m["org_ref"] = org.String()
	}
	return m
}

var phoneSeq = 100

// createKN tạo tài khoản KN (POST /v1/accounts/kn-users) và trả id + token của chính người đó.
func (f *knFix) createKN(name string, assignments ...map[string]any) (uuid.UUID, caller, resp) {
	f.t.Helper()
	phoneSeq++
	n := ids.New().String()[24:]
	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": name, "email": "kn-" + n + "@vi-du.vn",
		"phone": "091234" + strings.Repeat("0", 4-len(itoa(phoneSeq))) + itoa(phoneSeq), "assignments": assignments})
	if r.status != http.StatusCreated {
		f.t.Fatalf("tạo tài khoản KN: %d %v", r.status, r.body)
	}
	id := uuid.MustParse(str(r.body, "user", "id"))
	return id, f.tok(id, f.tenant, false, port.RoleKNUser), r
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func asgIDs(r resp) map[string]string {
	out := map[string]string{}
	list, _ := r.body["assignments"].([]any)
	for _, x := range list {
		m := x.(map[string]any)
		out[m["role"].(string)] = m["assignment_id"].(string)
	}
	return out
}

func (f *knFix) selectCtx(c caller, assignment string, dept any) resp {
	return f.req(f.app, c, http.MethodPost, "/v1/me/context", map[string]any{"assignment_id": assignment, "department_code": dept}, nil)
}

func (f *knFix) ctxID(c caller, assignment string, dept any) string {
	f.t.Helper()
	r := f.selectCtx(c, assignment, dept)
	if r.status != http.StatusCreated {
		f.t.Fatalf("chọn ngữ cảnh: %d %v", r.status, r.body)
	}
	return str(r.body, "context_id")
}

func (f *knFix) withCtx(c caller, method, path, ctx string, body any) resp {
	return f.req(f.app, c, method, path, body, map[string]string{authz.ContextHeader: ctx})
}

func (f *knFix) lastProbe() authz.Principal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probe[len(f.probe)-1]
}

func wantCode(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || (code != "" && r.body["code"] != code) {
		t.Fatalf("%s: muốn %d %s, có %d %v", what, status, code, r.status, r.body)
	}
}

// FN-CH-00.01 (Hình 3b, 25b): một tài khoản nhiều vai trò → nhiều ngữ cảnh; chọn vai trò + phòng ban,
// đổi không đăng nhập lại; phòng ban chưa mở hiện mờ, không chọn được; gọi lặp cùng khóa.
func TestFN_CH_00_01_MultiRoleContextsAndSwitch(t *testing.T) {
	f := newKNFixture(t)
	_, an, cr := f.createKN("Nguyễn Văn An",
		asg(port.KNXa, f.p91, []string{deptTT, deptCN}, "30292", "30301"),
		asg(port.KNTinh, f.p91, []string{deptTT}),
		asg(port.KNTW, f.national, []string{deptTT}))
	ids3 := asgIDs(cr)
	r := f.req(f.app, an, http.MethodGet, "/v1/me/contexts", nil, nil)
	list, _ := r.body["assignments"].([]any)
	if r.status != http.StatusOK || len(list) != 3 {
		t.Fatalf("contexts: %d %v", r.status, r.body)
	}
	for _, x := range list {
		m := x.(map[string]any)
		if m["role"] == port.KNXa {
			deps := m["departments"].([]any)
			communes := m["communes"].([]any)
			if len(deps) != 2 || len(communes) != 2 || m["mgmt_level"] != "Chuyên viên - Cán bộ" || m["org_name"] != "Tổ chức thử" {
				t.Fatalf("thẻ Cấp Xã: %v", m)
			}
			for _, d := range deps {
				dm := d.(map[string]any)
				if dm["code"] == deptCN && dm["status"] != partyport.DeptNotOpen {
					t.Fatalf("phòng ban chưa mở phải mang status not_open: %v", dm)
				}
			}
		}
	}
	// Chọn Cấp Xã · Trồng trọt.
	sel := f.selectCtx(an, ids3[port.KNXa], deptTT)
	if sel.status != http.StatusCreated || sel.body["role"] != port.KNXa || sel.body["department_code"] != deptTT ||
		sel.body["default_route"] != "/kn/nhiem-vu/cong-viec" || len(sel.body["communes"].([]any)) != 2 || sel.body["expires_at"] == nil {
		t.Fatalf("chọn Cấp Xã: %d %v", sel.status, sel.body)
	}
	xaCtx := str(sel.body, "context_id")
	// Phòng ban chưa mở → 409; phòng ban không thuộc vai trò → 422; Cấp Xã không gửi phòng ban → 422.
	wantCode(t, f.selectCtx(an, ids3[port.KNXa], deptCN), http.StatusConflict, "accounts.department_not_open", "chọn phòng ban chưa mở")
	wantCode(t, f.selectCtx(an, ids3[port.KNTinh], deptBVTV), http.StatusUnprocessableEntity, "accounts.department_required", "phòng ban ngoài vai trò")
	wantCode(t, f.selectCtx(an, ids3[port.KNXa], nil), http.StatusUnprocessableEntity, "accounts.department_required", "thiếu phòng ban")
	// Đổi vai trò không đăng nhập lại: cùng token, ngữ cảnh mới; ngữ cảnh cũ vẫn dùng được (tab khác).
	tinhCtx := f.ctxID(an, ids3[port.KNTinh], deptTT)
	if r := f.withCtx(an, http.MethodGet, "/v1/me/context", tinhCtx, nil); r.status != http.StatusOK || r.body["role"] != port.KNTinh ||
		r.body["default_route"] != "/kn/dashboard" {
		t.Fatalf("ngữ cảnh Tỉnh: %d %v", r.status, r.body)
	}
	if r := f.withCtx(an, http.MethodGet, "/v1/me/context", xaCtx, nil); r.status != http.StatusOK || r.body["role"] != port.KNXa {
		t.Fatalf("ngữ cảnh Xã vẫn hợp lệ: %d %v", r.status, r.body)
	}
	// Không có header → không có ngữ cảnh (đường cũ giữ nguyên cho module khác).
	wantCode(t, f.req(f.app, an, http.MethodGet, "/v1/me/context", nil, nil), 428, "accounts.context_required", "không header")
	if r := f.req(f.app, an, http.MethodGet, "/v1/probe", nil, nil); r.status != http.StatusOK || f.lastProbe().KN != nil {
		t.Fatalf("không header: Principal.KN phải nil")
	}
	// Gọi lặp POST /v1/me/context cùng Idempotency-Key → cùng context_id.
	k := key()
	body := map[string]any{"assignment_id": ids3[port.KNTW], "department_code": deptTT}
	r1 := f.req(f.app, an, http.MethodPost, "/v1/me/context", body, map[string]string{"Idempotency-Key": k})
	r2 := f.req(f.app, an, http.MethodPost, "/v1/me/context", body, map[string]string{"Idempotency-Key": k})
	if r1.status != http.StatusCreated || str(r1.body, "context_id") == "" || str(r1.body, "context_id") != str(r2.body, "context_id") {
		t.Fatalf("gọi lặp: %d %v / %d %v", r1.status, r1.body, r2.status, r2.body)
	}
}

// Header X-AG-Context giả hoặc của người khác bị từ chối (vai, phòng ban, phạm vi không bao giờ lấy từ
// client); tenant khác không thấy ngữ cảnh (RLS).
func TestFN_CH_00_01_ContextHeaderSpoofingRejected(t *testing.T) {
	f := newKNFixture(t)
	_, an, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	_, binh, _ := f.createKN("Trần Thị Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	anCtx := f.ctxID(an, asgIDs(cr)[port.KNXa], deptTT)
	wantCode(t, f.withCtx(binh, http.MethodGet, "/v1/probe", anCtx, nil), http.StatusForbidden, "accounts.context_invalid", "dùng ngữ cảnh của người khác")
	wantCode(t, f.withCtx(an, http.MethodGet, "/v1/probe", ids.New().String(), nil), http.StatusForbidden, "accounts.context_invalid", "ngữ cảnh không tồn tại")
	wantCode(t, f.withCtx(an, http.MethodGet, "/v1/probe", "kn_tinh;trong_trot", nil), http.StatusForbidden, "accounts.context_invalid", "header không phải UUID")
	other := f.tok(an.actor, ids.New(), false, port.RoleKNUser) // cùng sub, tenant khác
	wantCode(t, f.withCtx(other, http.MethodGet, "/v1/probe", anCtx, nil), http.StatusForbidden, "accounts.context_invalid", "tenant khác")
	// Chọn ngữ cảnh bằng lần gán của người khác → 404 (không lộ sự tồn tại).
	wantCode(t, f.selectCtx(binh, asgIDs(cr)[port.KNXa], deptTT), http.StatusNotFound, "accounts.assignment_not_found", "lần gán của người khác")
	// Ngữ cảnh hợp lệ: Principal giữ actor thật và vai realm của token, vai KN nằm ở KN.
	if r := f.withCtx(an, http.MethodGet, "/v1/probe", anCtx, nil); r.status != http.StatusOK {
		t.Fatalf("ngữ cảnh hợp lệ: %d %v", r.status, r.body)
	}
	p := f.lastProbe()
	if p.SelectedSession == nil || p.SelectedSession.Profile() != "fixture" || p.SelectedSession.ID().String() != anCtx {
		t.Fatal("selected server context was not bound")
	}
	if p.ActorRef != an.actor || p.KN == nil || p.KN.Role != port.KNXa || p.Has("kn_officer") || !p.Has(port.RoleKNUser) {
		t.Fatalf("Principal: %+v", p)
	}
}

// FN-AD-04.04: phạm vi theo ngữ cảnh — Cấp Xã 2 xã chỉ thấy 2 xã; Cấp Tỉnh thấy tỉnh và mọi xã; hai Cán
// bộ xã khác phòng ban trên cùng xã chỉ thấy dữ liệu phòng ban mình; Cấp Quốc gia toàn quốc, chỉ đọc.
func TestFN_AD_04_04_ScopeIsolationPerContext(t *testing.T) {
	f := newKNFixture(t)
	_, an, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292", "30301"), asg(port.KNTinh, f.p91, []string{deptTT}),
		asg(port.KNTW, f.national, []string{deptTT}))
	_, cuong, cr2 := f.createKN("Lê Văn Cường", asg(port.KNXa, f.p91, []string{deptBVTV}, "30292"))
	a := asgIDs(cr)
	probe := func(c caller, ctx string) authz.KNContext {
		t.Helper()
		if r := f.withCtx(c, http.MethodGet, "/v1/probe", ctx, nil); r.status != http.StatusOK {
			t.Fatalf("probe: %d %v", r.status, r.body)
		}
		return *f.lastProbe().KN
	}
	xa := probe(an, f.ctxID(an, a[port.KNXa], deptTT))
	if xa.Scope.All || !slices.Equal(xa.Scope.Units, []string{"30292", "30301"}) || !xa.Scope.AllowsCommune("30301") || xa.Scope.Allows("31120") || xa.Scope.Allows("91") {
		t.Fatalf("Cấp Xã 2 xã: %+v", xa.Scope)
	}
	tinh := probe(an, f.ctxID(an, a[port.KNTinh], deptTT))
	if !tinh.Scope.Allows("91") || !tinh.Scope.Allows("30292") || tinh.Scope.Allows("31120") || len(tinh.Communes) > 0 {
		t.Fatalf("Cấp Tỉnh: %+v", tinh)
	}
	tw := probe(an, f.ctxID(an, a[port.KNTW], deptTT))
	if !tw.Scope.All || !tw.ReadOnly() || tw.DefaultRoute != "/kn/dashboard" {
		t.Fatalf("Cấp Quốc gia: %+v", tw)
	}
	c2 := probe(cuong, f.ctxID(cuong, asgIDs(cr2)[port.KNXa], deptBVTV))
	// Cùng xã 30292, khác phòng ban: mỗi người chỉ dữ liệu phòng ban mình.
	if !xa.Allows(deptTT, "30292") || xa.Allows(deptBVTV, "30292") || !c2.Allows(deptBVTV, "30292") || c2.Allows(deptTT, "30292") {
		t.Fatalf("hai Cán bộ xã khác phòng ban: an=%+v cuong=%+v", xa, c2)
	}
	// P2 hook: menu theo ngữ cảnh phiên.
	pctx := authz.WithPrincipal(context.Background(), authz.Principal{KN: &c2})
	if aud, dept, unit, ok := f.kmod.SessionContext().Current(pctx); !ok || aud != port.KNXa || dept != deptBVTV || unit != f.p91 {
		t.Fatalf("SessionContext: %s %s %s %v", aud, dept, unit, ok)
	}
	// RequireKN: vai trong ngữ cảnh, không trong token.
	if _, _, err := authz.RequireKN(pctx, port.KNTinh); err == nil {
		t.Fatal("RequireKN phải chặn Cấp Xã ở thao tác Cấp Tỉnh")
	}
	if _, k, err := authz.RequireKN(pctx, port.KNXa); err != nil || k.Department != deptBVTV {
		t.Fatalf("RequireKN Cấp Xã: %v", err)
	}
}

// FN-AD-04.04 + FN-AD-04.03 AC: thu hồi vai trò, đổi xã, ngưng phòng ban, khóa tài khoản có hiệu lực ở
// request kế tiếp; phòng ban chuyển "Đang áp dụng" chọn được ngay; số tài khoản theo phòng ban (P2).
func TestFN_AD_04_04_RevocationAndDepartmentStatusTakeEffect(t *testing.T) {
	f := newKNFixture(t)
	anID, an, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT, deptCN}, "30292", "30301"), asg(port.KNTinh, f.p91, []string{deptTT}))
	a := asgIDs(cr)
	ctx := f.ctxID(an, a[port.KNXa], deptTT)
	// Đổi xã phụ trách → phạm vi mới ngay.
	ch := asg(port.KNXa, f.p91, []string{deptTT, deptCN}, "30292")
	ch["reason"] = "Điều chuyển địa bàn theo quyết định 15/QĐ"
	if r := f.req(f.admin2, f.root, http.MethodPut, "/v1/accounts/users/"+anID.String()+"/assignments/"+a[port.KNXa], ch, nil); r.status != http.StatusOK {
		t.Fatalf("sửa vai trò: %d %v", r.status, r.body)
	}
	f.withCtx(an, http.MethodGet, "/v1/probe", ctx, nil)
	if u := f.lastProbe().KN.Scope.Units; !slices.Equal(u, []string{"30292"}) {
		t.Fatalf("xã sau khi sửa: %v", u)
	}
	// Phòng ban Chăn nuôi chuyển Đang áp dụng → chọn được ngay; ngưng lại → ngữ cảnh hết hiệu lực.
	f.depts.setStatus(f.p91, deptCN, partyport.DeptActive)
	cnCtx := f.ctxID(an, a[port.KNXa], deptCN)
	f.depts.setStatus(f.p91, deptCN, partyport.DeptSuspended)
	wantCode(t, f.withCtx(an, http.MethodGet, "/v1/probe", cnCtx, nil), http.StatusForbidden, "accounts.context_revoked", "phòng ban ngưng")
	// Số tài khoản theo phòng ban (P2 UseDepartmentUsage → chặn "xóa" phòng ban còn tài khoản).
	tctx := tenant.WithID(context.Background(), f.tenant)
	if u, err := f.kmod.DepartmentUsage().DepartmentUsage(tctx, f.p91, deptTT); err != nil || u.Accounts != 1 {
		t.Fatalf("số tài khoản Trồng trọt: %+v %v", u, err)
	}
	if n, err := f.kmod.OrgImpact().OrgImpact(tctx, f.team30292, []string{"30292"}); err != nil || n != 1 {
		t.Fatalf("ảnh hưởng Nút xã: %d %v", n, err)
	}
	// Thu hồi vai trò Cấp Xã (lý do bắt buộc) → ngữ cảnh hết hiệu lực; vai Tỉnh còn.
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/assignments/"+a[port.KNXa]+"/revoke",
		map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed", "thu hồi thiếu lý do")
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/assignments/"+a[port.KNXa]+"/revoke",
		map[string]any{"reason": "Cán bộ chuyển công tác về tỉnh"}); r.status != http.StatusOK || r.body["assignment"].(map[string]any)["active"] != false {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	wantCode(t, f.withCtx(an, http.MethodGet, "/v1/probe", ctx, nil), http.StatusForbidden, "accounts.context_revoked", "vai trò đã thu hồi")
	if u, _ := f.kmod.DepartmentUsage().DepartmentUsage(tctx, f.p91, deptCN); u.Accounts != 0 {
		t.Fatalf("Chăn nuôi sau thu hồi: %+v", u)
	}
	// Khóa tài khoản → mọi ngữ cảnh hết hiệu lực ngay (khóa thu hồi mọi phiên).
	tinhCtx := f.ctxID(an, a[port.KNTinh], deptTT)
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("khóa: %d %v", r.status, r.body)
	}
	wantCode(t, f.withCtx(an, http.MethodGet, "/v1/probe", tinhCtx, nil), http.StatusForbidden, "accounts.context_revoked", "tài khoản bị khóa")
	wantCode(t, f.req(f.app, an, http.MethodGet, "/v1/me/contexts", nil, nil), http.StatusForbidden, "accounts.account_locked", "contexts khi bị khóa")
	if f.dir.Logouts(anID) == 0 {
		t.Fatal("khóa phải thu hồi phiên Keycloak")
	}
}

// FN-CH-00.01: Admin không chọn phòng ban, vào "Tổng quan hệ thống"; bắt buộc MFA. Vai trò Admin chỉ
// platform_admin gán, cho tài khoản đã có platform_admin (SoD).
func TestFN_CH_00_01_AdminNoDepartment(t *testing.T) {
	f := newKNFixture(t)
	admID := f.dir.Seed(port.NewUser{Username: "admin.hotro", Email: "admin.hotro@vi-du.vn", FirstName: "Hỗ trợ", LastName: "Admin", TenantID: f.tenant},
		true, "platform_admin")
	// account_admin không gán được vai Admin; platform_admin không gán Admin cho người thiếu platform_admin.
	wantCode(t, f.adminDo(f.accAdmin, http.MethodPost, "/v1/accounts/users/"+admID.String()+"/assignments", asg(port.KNAdmin, uuid.Nil, nil)),
		http.StatusForbidden, "", "account_admin gán Admin")
	_, plain, _ := f.createKN("Phạm Văn Lộc", asg(port.KNTinh, f.p91, []string{deptTT}))
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+plain.actor.String()+"/assignments", asg(port.KNAdmin, uuid.Nil, nil)),
		http.StatusForbidden, "accounts.kn_admin_grant", "Admin cho người không có platform_admin")
	// Admin có phòng ban → từ chối.
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+admID.String()+"/assignments", asg(port.KNAdmin, uuid.Nil, []string{deptTT})),
		http.StatusUnprocessableEntity, "validation_failed", "Admin có phòng ban")
	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+admID.String()+"/assignments", asg(port.KNAdmin, uuid.Nil, nil))
	if r.status != http.StatusCreated {
		t.Fatalf("gán Admin: %d %v", r.status, r.body)
	}
	aid := str(r.body, "assignment", "assignment_id")
	adm := f.tok(admID, f.tenant, true, "platform_admin", port.RoleKNUser)
	r = f.req(f.app, adm, http.MethodGet, "/v1/me/contexts", nil, nil)
	m := r.body["assignments"].([]any)[0].(map[string]any)
	if m["role"] != port.KNAdmin || len(m["departments"].([]any)) != 0 || m["org_ref"] != nil || m["org_name"] != "Toàn hệ thống" {
		t.Fatalf("thẻ Admin: %v", m)
	}
	sel := f.selectCtx(adm, aid, nil)
	if sel.status != http.StatusCreated || sel.body["department_code"] != nil || sel.body["default_route"] != "/kn/tong-quan" {
		t.Fatalf("chọn Admin: %d %v", sel.status, sel.body)
	}
	f.withCtx(adm, http.MethodGet, "/v1/probe", str(sel.body, "context_id"), nil)
	k := f.lastProbe().KN
	if !k.Scope.All || k.Department != "" || !k.AllowsDepartment(deptTT) || !k.AllowsDepartment(deptBVTV) {
		t.Fatalf("ngữ cảnh Admin: %+v", k)
	}
	noMFA := f.tok(admID, f.tenant, false, "platform_admin", port.RoleKNUser)
	wantCode(t, f.selectCtx(noMFA, aid, nil), http.StatusForbidden, "mfa_required", "Admin không MFA chọn ngữ cảnh")
	wantCode(t, f.withCtx(noMFA, http.MethodGet, "/v1/probe", str(sel.body, "context_id"), nil), http.StatusForbidden, "mfa_required", "Admin không MFA dùng ngữ cảnh")
}

// FN-AD-04.07: Admin hỗ trợ trong menu Cấp Xã — dải hỗ trợ (support_session_id), mọi thao tác ghi bắt buộc
// lý do và có trong nhật ký với Admin thật + thay cho ai + chức năng + lý do; thoát hỗ trợ → hết hiệu lực.
func TestFN_AD_04_07_SupportModeAuditTrail(t *testing.T) {
	f := newKNFixture(t)
	anID, _, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292", "30301"))
	start := map[string]any{"target_assignment_id": asgIDs(cr)[port.KNXa], "department_code": deptTT, "reason": "Hỗ trợ xã nhập biểu T01 tuần 36"}
	wantCode(t, f.adminDo(f.accAdmin, http.MethodPost, "/v1/accounts/support-sessions", start), http.StatusForbidden, "", "account_admin mở hỗ trợ")
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"target_assignment_id": asgIDs(cr)[port.KNXa],
		"department_code": deptTT}), http.StatusUnprocessableEntity, "validation_failed", "mở hỗ trợ thiếu lý do")
	r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/support-sessions", start)
	if r.status != http.StatusCreated || r.body["role"] != port.KNXa || r.body["on_behalf_of"] != anID.String() || r.body["support_session_id"] == nil {
		t.Fatalf("mở hỗ trợ: %d %v", r.status, r.body)
	}
	sctx, sid := str(r.body, "context_id"), str(r.body, "support_session_id")
	// Đọc trong chế độ hỗ trợ: không cần lý do, Principal giữ Admin thật.
	if r := f.withCtx(f.root, http.MethodGet, "/v1/probe", sctx, nil); r.status != http.StatusOK {
		t.Fatalf("đọc khi hỗ trợ: %d %v", r.status, r.body)
	}
	p := f.lastProbe()
	if p.ActorRef != f.rootID || p.KN.Support == nil || p.KN.Support.OnBehalfOf != anID || p.KN.Role != port.KNXa || !slices.Equal(p.KN.Communes, []string{"30292", "30301"}) {
		t.Fatalf("Principal hỗ trợ: %+v %+v", p, p.KN)
	}
	// Ghi không lý do → 422; có lý do (UTF-8 mã hóa phần trăm) → chạy + 2 dòng nhật ký.
	wantCode(t, f.req(f.app, f.root, http.MethodPost, "/v1/probe/30292/write", map[string]any{}, map[string]string{authz.ContextHeader: sctx}),
		http.StatusUnprocessableEntity, "accounts.support_reason_required", "ghi không lý do")
	reason := "Xã báo mất mạng, nhập thay theo điện thoại"
	if r := f.req(f.app, f.root, http.MethodPost, "/v1/probe/30292/write", map[string]any{}, map[string]string{authz.ContextHeader: sctx,
		authz.ReasonHeader: url.PathEscape(reason)}); r.status != http.StatusOK {
		t.Fatalf("ghi có lý do: %d %v", r.status, r.body)
	}
	if p := f.lastProbe(); p.KN.Support.Reason != reason || p.ActorRef != f.rootID {
		t.Fatalf("lý do trong ngữ cảnh: %+v", p.KN.Support)
	}
	lg := f.adminDo(f.root, http.MethodGet, "/v1/accounts/support-log?support_session_id="+sid, nil)
	items, _ := lg.body["items"].([]any)
	if lg.status != http.StatusOK || len(items) != 2 {
		t.Fatalf("nhật ký hỗ trợ: %d %v", lg.status, lg.body)
	}
	done, att := items[0].(map[string]any), items[1].(map[string]any)
	if done["phase"] != "completed" || done["status_code"] != float64(200) || att["phase"] != "attempted" || att["actor_ref"] != f.rootID.String() ||
		att["actor_display"] != "Quản trị Mekong" || att["on_behalf_of"] != anID.String() || att["on_behalf_of_display"] != "Nguyễn Văn An" ||
		att["function"] != "POST /v1/probe/:id/write" || att["reason"] != reason || att["acted_role"] != port.KNXa || att["department_code"] != deptTT {
		t.Fatalf("dòng nhật ký: %v / %v", att, done)
	}
	if pp := att["content"].(map[string]any)["path_params"].(map[string]any); pp["id"] != "30292" {
		t.Fatalf("nội dung: %v", att["content"])
	}
	// Admin khác (hoặc người được hỗ trợ) không dùng được ngữ cảnh hỗ trợ.
	wantCode(t, f.withCtx(f.tok(anID, f.tenant, false, port.RoleKNUser), http.MethodGet, "/v1/probe", sctx, nil), http.StatusForbidden,
		"accounts.context_invalid", "người được hỗ trợ dùng ngữ cảnh hỗ trợ")
	// Thoát hỗ trợ → ngữ cảnh hết hiệu lực; thoát lần hai → 409.
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/support-sessions/"+sid+"/end", nil); r.status != http.StatusOK || r.body["ended_at"] == nil {
		t.Fatalf("thoát hỗ trợ: %d %v", r.status, r.body)
	}
	wantCode(t, f.withCtx(f.root, http.MethodGet, "/v1/probe", sctx, nil), http.StatusForbidden, "accounts.context_revoked", "sau khi thoát")
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/support-sessions/"+sid+"/end", nil), http.StatusConflict, "accounts.support_session_ended", "thoát lần hai")
	// Hỗ trợ không gắn tài khoản: chọn vai + Nút + xã.
	r = f.adminDo(f.root, http.MethodPost, "/v1/accounts/support-sessions", map[string]any{"role": port.KNXa, "org_ref": f.p91.String(),
		"commune_codes": []string{"30301"}, "department_code": deptTT, "reason": "Kiểm tra menu Cấp Xã của xã 30301"})
	if r.status != http.StatusCreated || r.body["on_behalf_of"] != nil || r.body["assignment_id"] != nil {
		t.Fatalf("hỗ trợ theo xã: %d %v", r.status, r.body)
	}
}

// FN-AD-04.04: Log chỉnh sửa — mọi lần tạo, thêm/sửa/xóa vai trò, sửa thông tin, khóa, gửi lại liên kết
// hiện với người, thời điểm, cũ → mới (định dạng Log chung của P2). PII không vào sổ (RB-3).
func TestFN_AD_04_04_AssignmentLog(t *testing.T) {
	f := newKNFixture(t)
	anID, _, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	aid := asgIDs(cr)[port.KNXa]
	base := "/v1/accounts/users/" + anID.String()
	ch := asg(port.KNXa, f.p91, []string{deptTT}, "30292", "30301")
	ch["mgmt_level"] = "truong_phong"
	if r := f.req(f.admin2, f.root, http.MethodPut, base+"/assignments/"+aid, ch, nil); r.status != http.StatusOK {
		t.Fatalf("sửa: %d %v", r.status, r.body)
	}
	if r := f.req(f.admin2, f.root, http.MethodPut, base+"/assignments/"+aid, ch, nil); r.status != http.StatusConflict {
		t.Fatalf("sửa không đổi gì phải 409: %d %v", r.status, r.body)
	}
	g := f.adminDo(f.root, http.MethodPost, base+"/assignments", asg(port.KNTinh, f.p91, []string{deptTT}))
	if g.status != http.StatusCreated {
		t.Fatalf("thêm vai trò: %d %v", g.status, g.body)
	}
	newPhone := "0987654321"
	if r := f.req(f.admin2, f.root, http.MethodPatch, base+"/profile", map[string]any{"phone": newPhone, "full_name": "Nguyễn Văn Ân"}, nil); r.status != http.StatusOK ||
		!slices.Equal(toStrings(r.body["changed_fields"]), []string{"full_name", "phone", "username"}) || str(r.body, "user", "username") != newPhone {
		t.Fatalf("sửa thông tin: %d %v", r.status, r.body)
	}
	if r := f.adminDo(f.root, http.MethodPost, base+"/assignments/"+str(g.body, "assignment", "assignment_id")+"/revoke",
		map[string]any{"reason": "Gán nhầm vai trò Cấp Tỉnh"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	lg := f.adminDo(f.root, http.MethodGet, base+"/log", nil)
	items, _ := lg.body["items"].([]any)
	var events []string
	for _, x := range items {
		events = append(events, x.(map[string]any)["event"].(string))
	}
	want := []string{"account.created", "account.assignment_changed", "assignment.granted", "account.activation_sent", "assignment.changed", "assignment.granted",
		"account.profile_updated", "assignment.revoked"}
	if lg.status != http.StatusOK || !slices.Equal(events, want) {
		t.Fatalf("Log: %d %v", lg.status, events)
	}
	changed := items[4].(map[string]any)
	if changed["actor_display"] != "Quản trị Mekong" || changed["entity_ref"] != aid || changed["seq"] != float64(5) {
		t.Fatalf("dòng sửa: %v", changed)
	}
	got := map[string][2]any{}
	for _, c := range changed["changes"].([]any) {
		m := c.(map[string]any)
		got[m["field"].(string)] = [2]any{m["old"], m["new"]}
	}
	if got["commune_codes"] != [2]any{"30292", "30292,30301"} || got["mgmt_level"] != [2]any{"chuyen_vien", "truong_phong"} || len(got) != 2 {
		t.Fatalf("cũ → mới: %v", got)
	}
	prof := items[6].(map[string]any)
	raw, _ := json.Marshal(prof)
	if strings.Contains(string(raw), newPhone) || strings.Contains(string(raw), "Ân") {
		t.Fatalf("Log chỉnh sửa không được chứa PII: %s", raw)
	}
	if items[7].(map[string]any)["reason"] != "Gán nhầm vai trò Cấp Tỉnh" {
		t.Fatalf("lý do thu hồi: %v", items[7])
	}
	// Phân trang cursor.
	p1 := f.adminDo(f.root, http.MethodGet, base+"/log?limit=3", nil)
	p2 := f.adminDo(f.root, http.MethodGet, base+"/log?limit=3&cursor="+itoa(int(p1.body["next_cursor"].(float64))), nil)
	if len(p1.body["items"].([]any)) != 3 || p2.body["items"].([]any)[0].(map[string]any)["seq"] != float64(4) {
		t.Fatalf("phân trang: %v / %v", p1.body, p2.body)
	}
	// Đăng nhập bằng SĐT mới: tên đăng nhập Keycloak = SĐT chuẩn hóa.
	if u, _ := f.dir.GetUser(context.Background(), anID); u.Username != newPhone || u.Phone != newPhone {
		t.Fatalf("Keycloak: %s %s", u.Username, u.Phone)
	}
}

func toStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		out = append(out, x.(string))
	}
	return out
}

// UR-AD-04 / FN-AD-04.04: tiên quyết (Nút, phòng ban), ràng buộc vai ↔ Nút ↔ xã, trùng SĐT/email, chỉ
// Admin quản lý tài khoản, không tự sửa mình, cảnh báo Cấp Xã chưa gán xã; tạo tài khoản: tên đăng nhập
// là SĐT, lần đầu đặt mật khẩu qua liên kết (không mật khẩu tạm); gọi lặp cùng khóa.
func TestFN_AD_04_04_CreateAccountRules(t *testing.T) {
	f := newKNFixture(t)
	create := func(c caller, phone string, as ...map[string]any) resp {
		return f.adminDo(c, http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Võ Thị Mai", "email": "mai-" + ids.New().String()[24:] + "@vi-du.vn",
			"phone": phone, "assignments": as})
	}
	wantCode(t, create(f.root, "", asg(port.KNTinh, ids.New(), []string{deptTT})), http.StatusConflict, "accounts.prerequisite_missing", "Nút không tồn tại")
	wantCode(t, create(f.root, "", asg(port.KNTinh, f.p92, []string{deptTT})), http.StatusConflict, "accounts.prerequisite_missing", "đơn vị chưa có phòng ban")
	wantCode(t, create(f.root, "", asg(port.KNTinh, f.p91, []string{"phong_khong_co"})), http.StatusUnprocessableEntity, "accounts.department_invalid", "phòng ban lạ")
	wantCode(t, create(f.root, "", asg(port.KNTW, f.p91, []string{deptTT})), http.StatusUnprocessableEntity, "validation_failed", "Cấp Quốc gia gắn Nút tỉnh")
	wantCode(t, create(f.root, "", asg(port.KNXa, f.p91, []string{deptTT}, "31120")), http.StatusUnprocessableEntity, "accounts.commune_invalid", "xã ngoài tỉnh")
	wantCode(t, create(f.root, "", asg(port.KNTinh, f.p91, []string{deptTT}, "30292")), http.StatusUnprocessableEntity, "validation_failed", "Cấp Tỉnh có xã")
	wantCode(t, create(f.root, "", asg(port.KNTinh, f.p91, []string{deptTT}), asg(port.KNTinh, f.p91, []string{deptBVTV})), http.StatusConflict,
		"accounts.assignment_duplicate", "hai vai Tỉnh cùng Nút")
	wantCode(t, create(f.root, "", asg(port.KNAdmin, uuid.Nil, nil)), http.StatusForbidden, "accounts.kn_admin_grant", "tạo mới với vai Admin")
	wantCode(t, create(f.provinceAdmin(), "", asg(port.KNTinh, f.p91, []string{deptTT})), http.StatusForbidden, "", "quản trị tỉnh không quản lý tài khoản KN")
	wantCode(t, create(f.tok(ids.New(), f.tenant, true, "kn_province_admin"), "", asg(port.KNTinh, f.p91, []string{deptTT})), http.StatusForbidden, "", "vai khác Admin")
	// Cấp Xã chưa gán xã → lưu được, kèm cảnh báo.
	k := key()
	body := map[string]any{"full_name": "Võ Thị Mai", "email": "mai.vt@vi-du.vn", "phone": "+84 912 000 111",
		"assignments": []any{asg(port.KNXa, f.p91, []string{deptTT})}}
	r := f.req(f.admin2, f.root, http.MethodPost, "/v1/accounts/kn-users", body, map[string]string{"Idempotency-Key": k})
	if r.status != http.StatusCreated || !strings.Contains(string(mustJSON(r.body["warnings"])), "commune_unassigned") || r.body["activation_email_sent"] != true {
		t.Fatalf("Cấp Xã chưa gán xã: %d %v", r.status, r.body)
	}
	id := uuid.MustParse(str(r.body, "user", "id"))
	u, _ := f.dir.GetUser(context.Background(), id)
	roles, _ := f.dir.RealmRoles(context.Background(), id)
	if u.Username != "0912000111" || u.Phone != "0912000111" || !slices.Equal(u.RequiredActions, []string{port.ActionVerifyEmail, port.ActionUpdatePassword, port.ActionConfigureTOTP}) || // D24
		!slices.Equal(roles, []string{port.RoleKNUser, "vault_registrar"}) || u.OrgRef != uuid.Nil { // A1: Cán bộ xã có vai kho định danh
		t.Fatalf("Keycloak: user=%s phone=%s actions=%v roles=%v org=%s", u.Username, u.Phone, u.RequiredActions, roles, u.OrgRef)
	}
	// Gọi lặp cùng Idempotency-Key → cùng tài khoản, không tạo thêm.
	r2 := f.req(f.admin2, f.root, http.MethodPost, "/v1/accounts/kn-users", body, map[string]string{"Idempotency-Key": k})
	if r2.status != http.StatusCreated || str(r2.body, "user", "id") != id.String() {
		t.Fatalf("gọi lặp: %d %v", r2.status, r2.body)
	}
	// Trùng SĐT / email → accounts.duplicate_identity.
	wantCode(t, create(f.root, "0912000111", asg(port.KNTinh, f.p91, []string{deptTT})), http.StatusConflict, "accounts.duplicate_identity", "trùng SĐT")
	dup := map[string]any{"full_name": "Võ Thị Mai", "email": "MAI.VT@vi-du.vn", "assignments": []any{asg(port.KNTinh, f.p91, []string{deptTT})}}
	wantCode(t, f.adminDo(f.root, http.MethodPost, "/v1/accounts/kn-users", dup), http.StatusConflict, "accounts.duplicate_identity", "trùng email")
	// Không tự thêm vai trò cho mình.
	self := f.tok(f.rootID, f.tenant, true, "platform_admin")
	wantCode(t, f.adminDo(self, http.MethodPost, "/v1/accounts/users/"+f.rootID.String()+"/assignments", asg(port.KNTinh, f.p91, []string{deptTT})),
		http.StatusForbidden, "accounts.self_action", "tự gán vai trò")
	// Danh sách vai trò của tài khoản (màn Detail).
	if r := f.adminDo(f.accAdmin, http.MethodGet, "/v1/accounts/users/"+id.String()+"/assignments", nil); r.status != http.StatusOK || len(r.body["items"].([]any)) != 1 {
		t.Fatalf("danh sách vai trò: %d %v", r.status, r.body)
	}
}

// Cách ly tenant (g1-conventions §5): quản trị tenant khác không thấy vai trò, Log; người dùng tenant
// khác không có ngữ cảnh.
func TestFN_AD_04_04_TenantIsolation(t *testing.T) {
	f := newKNFixture(t)
	anID, an, cr := f.createKN("Nguyễn Văn An", asg(port.KNTinh, f.p91, []string{deptTT}))
	otherTenant := ids.New()
	other := f.tok(ids.New(), otherTenant, true, "platform_admin")
	wantCode(t, f.adminDo(other, http.MethodGet, "/v1/accounts/users/"+anID.String()+"/assignments", nil), http.StatusNotFound, "accounts.user_not_found", "vai trò tenant khác")
	wantCode(t, f.adminDo(other, http.MethodGet, "/v1/accounts/users/"+anID.String()+"/log", nil), http.StatusNotFound, "accounts.user_not_found", "Log tenant khác")
	same := f.tok(an.actor, otherTenant, false, port.RoleKNUser)
	if r := f.req(f.app, same, http.MethodGet, "/v1/me/contexts", nil, nil); r.status != http.StatusOK || len(r.body["assignments"].([]any)) != 0 {
		t.Fatalf("contexts tenant khác: %d %v", r.status, r.body)
	}
	wantCode(t, f.selectCtx(same, asgIDs(cr)[port.KNTinh], deptTT), http.StatusNotFound, "accounts.assignment_not_found", "chọn lần gán của tenant khác")
}

// A1 (chốt 03/10/2026, FN-XA-03.03/03.05): gán vai trò Cán bộ xã → module tự cấp vai realm vault_registrar (ghi sổ dấu tự
// cấp); thu lần gán kn_xa cuối cùng → thu lại vai; tài khoản giữ vai vì lý do khác (Admin cấp) không bị thu.
func TestA1_KNXaDefaultVaultRegistrar(t *testing.T) {
	f := newKNFixture(t)
	ctx := context.Background()
	has := func(id uuid.UUID) bool {
		t.Helper()
		roles, err := f.dir.RealmRoles(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(roles, "vault_registrar")
	}
	// Tạo tài khoản có kn_xa → có vai kho định danh; dòng sổ ghi dấu tự cấp.
	anID, _, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"), asg(port.KNTinh, f.p91, []string{deptTT}))
	if !has(anID) {
		t.Fatal("Cán bộ xã phải có vault_registrar ngay khi tạo")
	}
	ev := f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+anID.String()+"/events", nil)
	if !strings.Contains(func() string { b, _ := json.Marshal(ev.body); return string(b) }(), "kn_xa_default") {
		t.Fatalf("sổ account_event thiếu dấu tự cấp: %v", ev.body)
	}
	// Chỉ vai Tỉnh → không cấp.
	binhID, _, _ := f.createKN("Trần Thị Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	if has(binhID) {
		t.Fatal("Cấp Tỉnh không được cấp mặc định vai kho định danh")
	}
	// Thêm vai trò kn_xa sau → cấp; thu hồi → thu lại (vai Tỉnh còn, kn_user còn).
	g := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+binhID.String()+"/assignments", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	if g.status != http.StatusCreated || !has(binhID) {
		t.Fatalf("thêm vai trò Cán bộ xã: %d %v", g.status, g.body)
	}
	aid := str(g.body, "assignment", "assignment_id")
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+binhID.String()+"/assignments/"+aid+"/revoke",
		map[string]any{"reason": "Hết nhiệm vụ phụ trách xã"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	if roles, _ := f.dir.RealmRoles(ctx, binhID); slices.Contains(roles, "vault_registrar") || !slices.Contains(roles, port.RoleKNUser) {
		t.Fatalf("sau thu hồi kn_xa cuối: %v", roles)
	}
	// Giữ vì lý do khác: Admin cấp vault_registrar trước khi gán kn_xa → thu kn_xa không thu vai.
	cuongID, _, _ := f.createKN("Lê Văn Cường", asg(port.KNTinh, f.p91, []string{deptTT}))
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+cuongID.String()+"/assignment",
		map[string]any{"org_ref": f.p91, "roles": []string{port.RoleKNUser, "vault_registrar"}}); r.status != http.StatusOK {
		t.Fatalf("Admin cấp vai kho: %d %v", r.status, r.body)
	}
	g = f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+cuongID.String()+"/assignments", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	if g.status != http.StatusCreated {
		t.Fatalf("thêm kn_xa: %d %v", g.status, g.body)
	}
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+cuongID.String()+"/assignments/"+str(g.body, "assignment", "assignment_id")+"/revoke",
		map[string]any{"reason": "Hết nhiệm vụ phụ trách xã"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	if !has(cuongID) {
		t.Fatal("vai do Admin cấp không được thu theo kn_xa")
	}
	// An: thu vai Tỉnh không đụng vai kho; thu kn_xa → thu vai kho.
	a := asgIDs(cr)
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/assignments/"+a[port.KNTinh]+"/revoke",
		map[string]any{"reason": "Hết nhiệm vụ cấp tỉnh"}); r.status != http.StatusOK || !has(anID) {
		t.Fatalf("thu vai Tỉnh: %d %v", r.status, r.body)
	}
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/assignments/"+a[port.KNXa]+"/revoke",
		map[string]any{"reason": "Cán bộ chuyển công tác"}); r.status != http.StatusOK || has(anID) {
		t.Fatalf("thu kn_xa cuối: %d %v", r.status, r.body)
	}
}

// IDEM-SCOPE-002: dùng middleware accounts thật; cache không vượt qua lần đổi vai/phòng trong cùng tài khoản.
func TestIdempotencyContextBinding_W1146(t *testing.T) {
	f := newKNFixture(t)
	_, an, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"), asg(port.KNTinh, f.p91, []string{deptTT}))
	a := asgIDs(cr)
	xa := f.ctxID(an, a[port.KNXa], deptTT)
	tinh := f.ctxID(an, a[port.KNTinh], deptTT)
	k := key()
	call := func(path, ctx string) resp {
		return f.req(f.app, an, http.MethodPost, path, map[string]any{"note": "Nội dung"}, map[string]string{authz.ContextHeader: ctx, "Idempotency-Key": k})
	}
	if r := call("/v1/probe/a/write", xa); r.status != 200 {
		t.Fatalf("lần đầu: %+v", r)
	}
	if r := call("/v1/probe/a/write", xa); r.status != 200 || r.hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại hợp lệ: %+v", r)
	}
	for _, r := range []resp{call("/v1/probe/b/write", xa), call("/v1/probe/a/write", tinh), call("/v1/probe/a/write?mode=other", xa)} {
		if r.status != 422 || r.body["code"] != "idempotency_key_reused" {
			t.Fatalf("khác tài nguyên/ngữ cảnh/truy vấn phải xung đột: %+v", r)
		}
	}
	if len(f.probe) != 1 {
		t.Fatalf("đã chạy handler ngoài lần đầu: %d", len(f.probe))
	}
}

func fixtureSessionRegistry(t *testing.T) *authz.SessionRegistry {
	t.Helper()
	r, err := authz.NewSessionRegistry(authz.SessionRegistration{Profile: "fixture", Project: func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
		if p.KN == nil {
			return authz.Session{}, false, nil
		}
		scope := authz.SessionScope{NodeID: p.KN.OrgRef, Department: p.KN.Department, UnitCodes: p.KN.Communes, RequiresNode: p.KN.Role == authz.KNXa}
		if p.KN.Support != nil {
			scope.OnBehalfOfRef = p.KN.Support.OnBehalfOf
		}
		binding, err := authz.NewSession("fixture", p.KN.ID, scope)
		return binding, true, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSelectedMiddlewareRequiresRegisteredProjection(t *testing.T) {
	provider := func(profile string) authz.SessionProjector {
		return func(_ context.Context, p authz.Principal) (authz.Session, bool, error) {
			s, err := authz.NewSession(profile, p.KN.ID, authz.SessionScope{})
			return s, true, err
		}
	}
	cases := []struct {
		name        string
		entries     []authz.SessionRegistration
		nilRegistry bool
	}{
		{name: "nil", nilRegistry: true},
		{name: "empty"},
		{name: "provider error", entries: []authz.SessionRegistration{{Profile: "fixture", Project: func(context.Context, authz.Principal) (authz.Session, bool, error) {
			return authz.Session{}, true, errors.New("synthetic projection failure")
		}}}},
		{name: "mismatch", entries: []authz.SessionRegistration{{Profile: "fixture", Project: provider("other")}}},
		{name: "ambiguous", entries: []authz.SessionRegistration{{Profile: "one", Project: provider("one")}, {Profile: "two", Project: provider("two")}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var registry *authz.SessionRegistry
			if !c.nilRegistry {
				var err error
				registry, err = authz.NewSessionRegistry(c.entries...)
				if err != nil {
					t.Fatal(err)
				}
			}
			f := newKNFixture(t, registry)
			_, actor, created := f.createKN("Cán bộ thử registry", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
			selected := f.ctxID(actor, asgIDs(created)[port.KNXa], deptTT)
			denied := f.withCtx(actor, http.MethodGet, "/v1/probe", selected, nil)
			wantCode(t, denied, http.StatusForbidden, "accounts.context_invalid", "unregistered projection must not use token permissions")
			if len(f.probe) != 0 {
				t.Fatal("rejected selected request reached handler")
			}
			if normal := f.req(f.app, actor, http.MethodGet, "/v1/probe", nil, nil); normal.status != http.StatusOK || f.lastProbe().SelectedSession != nil {
				t.Fatal("unselected native request changed")
			}
		})
	}
}
