package departments_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/departments"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

var (
	unit  = uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	dept  = uuid.MustParse("019a0000-0000-7000-8000-000000000002")
	actor = uuid.MustParse("019a0000-0000-7000-8000-000000000003")
	at    = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
)

// fakeAdmin ghi lại lời gọi và trả dữ liệu cố định — kiểm binding/DTO, không kiểm luật (luật ở pf/party, test PG).
type fakeAdmin struct {
	calls    []string
	in       partyport.DepartmentInput
	idem     string
	replayed bool
	err      error
	logs     []partyport.ChangeLogEntry
	after    int64
	// #222: menu, cây module phòng ban, nạp phòng ban.
	query partyport.MenuQuery
	code  string
	menu  partyport.MenuView
	seed  partyport.DepartmentSeedResult
	unit  uuid.UUID
}

func (f *fakeAdmin) dept() partyport.Department {
	return partyport.Department{ID: dept, OrgRef: unit, Code: "trong_trot", Name: "Phòng Trồng trọt", SortOrder: 1, Status: partyport.DeptActive,
		MenuProfile: partyport.MenuProfileTrongTrot, UpdatedBy: actor, UpdatedAt: at}
}

func (f *fakeAdmin) ListDepartments(_ context.Context, u uuid.UUID, includeRetired, admin bool) ([]partyport.DepartmentView, uuid.UUID, error) {
	f.calls = append(f.calls, "list")
	if f.err != nil {
		return nil, uuid.Nil, f.err
	}
	v := partyport.DepartmentView{Department: f.dept()}
	if admin {
		v.Usage = &partyport.Usage{Accounts: 2, Records: 7}
	}
	if u == uuid.Nil {
		u = unit
	}
	_ = includeRetired
	return []partyport.DepartmentView{v}, u, nil
}

func (f *fakeAdmin) CreateDepartment(_ context.Context, in partyport.DepartmentInput, idem string) (partyport.Department, bool, error) {
	f.calls, f.in, f.idem = append(f.calls, "create"), in, idem
	return f.dept(), f.replayed, f.err
}

func (f *fakeAdmin) GetDepartment(_ context.Context, id uuid.UUID) (partyport.DepartmentView, error) {
	f.calls = append(f.calls, "get")
	if f.err != nil {
		return partyport.DepartmentView{}, f.err
	}
	return partyport.DepartmentView{Department: f.dept()}, nil
}

func (f *fakeAdmin) UpdateDepartment(_ context.Context, _ uuid.UUID, in partyport.DepartmentInput) (partyport.Department, error) {
	f.calls, f.in = append(f.calls, "update"), in
	return f.dept(), f.err
}

func (f *fakeAdmin) ChangeDepartmentStatus(_ context.Context, _ uuid.UUID, status, reason, idem string) (partyport.DepartmentStatusResult, error) {
	f.calls, f.idem = append(f.calls, "status:"+status+":"+reason), idem
	return partyport.DepartmentStatusResult{Department: f.dept(), Replayed: f.replayed}, f.err
}

func (f *fakeAdmin) RetireDepartment(_ context.Context, _ uuid.UUID, reason, idem string) (partyport.Department, bool, error) {
	f.calls, f.idem = append(f.calls, "retire:"+reason), idem
	return f.dept(), f.replayed, f.err
}

func (f *fakeAdmin) DepartmentLog(_ context.Context, _ uuid.UUID, after int64, _ int) ([]partyport.ChangeLogEntry, error) {
	f.calls, f.after = append(f.calls, "log"), after
	return f.logs, f.err
}

func (f *fakeAdmin) SeedDepartments(_ context.Context, u uuid.UUID, idem string) (partyport.DepartmentSeedResult, error) {
	f.calls, f.unit, f.idem = append(f.calls, "seed"), u, idem
	return f.seed, f.err
}

func (f *fakeAdmin) Menu(_ context.Context, q partyport.MenuQuery) (partyport.MenuView, error) {
	f.calls, f.query = append(f.calls, "menu"), q
	return f.menu, f.err
}

func (f *fakeAdmin) DepartmentModules(_ context.Context, code string, q partyport.MenuQuery) (partyport.MenuView, error) {
	f.calls, f.code, f.query = append(f.calls, "modules"), code, q
	return f.menu, f.err
}

type surface struct {
	srv *httptest.Server
	e   *gin.Engine
}

func mount(t *testing.T, svc partyport.DepartmentAdmin, admin bool) surface {
	t.Helper()
	e := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	h := departments.Handler{Svc: svc}
	if admin {
		h.RegisterAdmin(e.Group("/v1"))
	} else {
		h.RegisterApp(e.Group("/v1"))
	}
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return surface{srv: srv, e: e}
}

type resp struct {
	status int
	hdr    http.Header
	raw    string
	body   map[string]any
}

func (s surface) do(t *testing.T, method, path, body string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, s.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, raw: string(raw), body: m}
}

func routes(e *gin.Engine) []string {
	var out []string
	for _, r := range e.Routes() {
		out = append(out, r.Method+" "+r.Path)
	}
	slices.Sort(out)
	return out
}

// Bảng đăng ký là hợp đồng: 7 thao tác Admin của danh mục phòng ban (#118) + nạp phòng ban mặc định (#222); Application:
// danh sách phòng ban (#118), cây module phòng ban và menu (#222). Khu vực, cây tổ chức ở pf/party.
func TestRouteMap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if got, want := routes(mount(t, nil, true).e), []string{
		"GET /v1/departments", "GET /v1/departments/:id", "GET /v1/departments/:id/log", "POST /v1/departments",
		"POST /v1/departments/:id/retire", "POST /v1/departments/:id/status", "POST /v1/orgs/:ref/departments/seed", "PUT /v1/departments/:id",
	}; !slices.Equal(got, want) {
		t.Fatalf("Admin routes:\n got %v\nwant %v", got, want)
	}
	if got, want := routes(mount(t, nil, false).e), []string{"GET /v1/departments", "GET /v1/departments/:code/modules", "GET /v1/menu"}; !slices.Equal(got, want) {
		t.Fatalf("Application routes: %v", got)
	}
}

// Chưa nối cổng của pf/party → mọi route đóng 503, không trả dữ liệu.
func TestUnwiredFailsClosed(t *testing.T) {
	adm, app := mount(t, nil, true), mount(t, nil, false)
	for _, c := range []struct {
		s            surface
		method, path string
	}{
		{adm, http.MethodGet, "/v1/departments"}, {adm, http.MethodPost, "/v1/departments"}, {adm, http.MethodGet, "/v1/departments/" + dept.String()},
		{adm, http.MethodPut, "/v1/departments/" + dept.String()}, {adm, http.MethodPost, "/v1/departments/" + dept.String() + "/status"},
		{adm, http.MethodPost, "/v1/departments/" + dept.String() + "/retire"}, {adm, http.MethodGet, "/v1/departments/" + dept.String() + "/log"},
		{app, http.MethodGet, "/v1/departments"},
		{adm, http.MethodPost, "/v1/orgs/" + unit.String() + "/departments/seed"}, {app, http.MethodGet, "/v1/menu"},
		{app, http.MethodGet, "/v1/departments/trong_trot/modules"},
	} {
		r := c.s.do(t, c.method, c.path, `{}`)
		if r.status != http.StatusServiceUnavailable || r.body["code"] != "extension.dependency_unavailable" {
			t.Fatalf("%s %s: %d %s", c.method, c.path, r.status, r.raw)
		}
	}
}

// DTO giữ nguyên byte-cho-byte thân JSON của hợp đồng KNDepartment/KNDepartmentList trước khi chuyển (#118).
func TestDTOAndStatusCodes(t *testing.T) {
	f := &fakeAdmin{}
	adm, app := mount(t, f, true), mount(t, f, false)
	want := `{"id":"` + dept.String() + `","org_ref":"` + unit.String() + `","code":"trong_trot","name":"Phòng Trồng trọt","description":null,` +
		`"sort_order":1,"status":"active","status_label":"Đang áp dụng","menu_profile":"trong_trot","retired":false,"selectable":true,` +
		`"account_count":%s,"record_count":%s,"updated_by":"` + actor.String() + `","updated_at":"2026-11-03T08:00:00Z"}`
	dto := func(a, r string) string { return strings.Replace(strings.Replace(want, "%s", a, 1), "%s", r, 1) }

	if r := app.do(t, http.MethodGet, "/v1/departments", ""); r.status != http.StatusOK ||
		r.raw != `{"items":[`+dto("null", "null")+`],"org_ref":"`+unit.String()+`"}` {
		t.Fatalf("app list: %d %s", r.status, r.raw)
	}
	if r := adm.do(t, http.MethodGet, "/v1/departments?org_ref="+unit.String(), ""); r.status != http.StatusOK ||
		r.raw != `{"items":[`+dto("2", "7")+`],"org_ref":"`+unit.String()+`"}` {
		t.Fatalf("admin list: %d %s", r.status, r.raw)
	}
	if r := adm.do(t, http.MethodGet, "/v1/departments?org_ref=khong-phai-uuid", ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("org_ref sai: %d %s", r.status, r.raw)
	}
	r := adm.do(t, http.MethodPost, "/v1/departments", `{"org_ref":"`+unit.String()+`","code":"trong_trot","name":"Phòng Trồng trọt","sort_order":1,"status":"active","menu_profile":"trong_trot"}`)
	if r.status != http.StatusCreated || r.hdr.Get("Location") != "/v1/departments/"+dept.String() || r.raw != dto("null", "null") {
		t.Fatalf("create: %d %v %s", r.status, r.hdr, r.raw)
	}
	if f.in.OrgRef != unit || f.in.Code != "trong_trot" || f.in.Status != partyport.DeptActive || f.in.MenuProfile != "trong_trot" {
		t.Fatalf("create input: %+v", f.in)
	}
	f.replayed = true
	if r := adm.do(t, http.MethodPost, "/v1/departments/"+dept.String()+"/status", `{"status":"suspended","reason":"Tạm ngưng chờ sắp xếp"}`); r.status != http.StatusOK ||
		r.raw != `{"department":`+dto("null", "null")+`,"warnings":[]}` {
		t.Fatalf("status replay: %d %s", r.status, r.raw)
	}
	if r := adm.do(t, http.MethodPost, "/v1/departments/"+dept.String()+"/retire", `{"reason":"Khai báo nhầm phòng ban"}`); r.status != http.StatusOK {
		t.Fatalf("retire replay: %d %s", r.status, r.raw)
	}
	f.replayed = false
	if r := adm.do(t, http.MethodPut, "/v1/departments/"+dept.String(), `{"name":"Phòng Trồng trọt","sort_order":5}`); r.status != http.StatusOK || f.in.SortOrder != 5 {
		t.Fatalf("update: %d %s %+v", r.status, r.raw, f.in)
	}
	if r := adm.do(t, http.MethodPut, "/v1/departments/"+dept.String(), `không phải json`); r.status != http.StatusBadRequest {
		t.Fatalf("json sai: %d %s", r.status, r.raw)
	}
	// id không phải UUID → như không có (404 party.department_not_found), không gọi use case.
	n := len(f.calls)
	for _, p := range []string{"/v1/departments/abc", "/v1/departments/abc/log"} {
		if r := adm.do(t, http.MethodGet, p, ""); r.status != http.StatusNotFound || r.body["code"] != "party.department_not_found" {
			t.Fatalf("%s: %d %s", p, r.status, r.raw)
		}
	}
	if len(f.calls) != n {
		t.Fatalf("id sai vẫn gọi use case: %v", f.calls[n:])
	}
	// Lỗi của use case chuyển nguyên mã HTTP + mã lỗi.
	f.err = problem.New(http.StatusConflict, "party.department_in_use", "x")
	if r := adm.do(t, http.MethodPost, "/v1/departments/"+dept.String()+"/retire", `{"reason":"Khai báo nhầm phòng ban"}`); r.status != http.StatusConflict ||
		r.body["code"] != "party.department_in_use" {
		t.Fatalf("lỗi use case: %d %s", r.status, r.raw)
	}
	f.err = problem.Forbidden
	if r := app.do(t, http.MethodGet, "/v1/departments", ""); r.status != http.StatusForbidden {
		t.Fatalf("lỗi quyền: %d %s", r.status, r.raw)
	}
}

func TestLogPage(t *testing.T) {
	old, nw, reason := "Phòng trong_trot", "Phòng Trồng trọt", "Đổi tên theo quyết định"
	f := &fakeAdmin{logs: []partyport.ChangeLogEntry{{Seq: 4, Event: "updated", ActorRef: actor, ActorDisplay: "Quản trị Mekong", At: at, RecordedAt: at,
		Changes: []partyport.FieldChange{{Field: "name", Old: &old, New: &nw}}, Reason: &reason}}}
	adm := mount(t, f, true)
	if r := adm.do(t, http.MethodGet, "/v1/departments/"+dept.String()+"/log?cursor=-1", ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("cursor âm: %d %s", r.status, r.raw)
	}
	r := adm.do(t, http.MethodGet, "/v1/departments/"+dept.String()+"/log?cursor=3&limit=1", "")
	want := `{"items":[{"seq":4,"event":"updated","actor_ref":"` + actor.String() + `","actor_display":"Quản trị Mekong","at":"2026-11-03T08:00:00Z",` +
		`"recorded_at":"2026-11-03T08:00:00Z","changes":[{"field":"name","old":"Phòng trong_trot","new":"Phòng Trồng trọt"}],"reason":"Đổi tên theo quyết định"}],"next_cursor":4}`
	if r.status != http.StatusOK || r.raw != want || f.after != 3 {
		t.Fatalf("log: %d %s after=%d", r.status, r.raw, f.after)
	}
	if r := adm.do(t, http.MethodGet, "/v1/departments/"+dept.String()+"/log", ""); !strings.HasSuffix(r.raw, `"next_cursor":null}`) {
		t.Fatalf("trang cuối: %s", r.raw)
	}
}
