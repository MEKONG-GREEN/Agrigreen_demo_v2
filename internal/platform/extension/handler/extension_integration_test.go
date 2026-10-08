package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
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

// t0 là "bây giờ" của kiểm thử: 10:00 giờ Việt Nam ngày 15/09/2026 (kỳ 2026-09).
var t0 = time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	mod    *extension.Module
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	params *fakeParams
	org    *orgTree
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, extension.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(t0)
	params := &fakeParams{v: map[string]decimal.Decimal{}}
	org := newOrgTree()
	mod := extension.New(pool, clk, extension.Deps{Scope: org, Orgs: org, Units: newFakeUnits(), Params: params, Catalogs: fakeCatalogs{}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &fixture{t: t, env: env, iss: iss, app: srv, mod: mod, pool: pool, clk: clk, params: params, org: org, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	roles []string
	token string
	actor uuid.UUID
}

func (f *fixture) as(org uuid.UUID, roles ...string) caller {
	return f.asTenant(f.tenant, org, roles...)
}

func (f *fixture) asTenant(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, TTL: time.Hour})
	return caller{f: f, tid: tid, org: org, roles: roles, token: tok, actor: actor}
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) call(method, path, idem string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.f.app.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp {
	c.f.t.Helper()
	return c.call(http.MethodPost, path, key(), body)
}
func (c caller) get(path string) resp { c.f.t.Helper(); return c.call(http.MethodGet, path, "", nil) }

func (c caller) must(r resp, want int, what string) resp {
	c.f.t.Helper()
	if r.status != want {
		c.f.t.Fatalf("%s: muốn %d, nhận %d %v", what, want, r.status, r.body)
	}
	return r
}

func str(m map[string]any, path ...string) string {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

func num(m map[string]any, path ...string) float64 {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return -1
		}
		cur = mm[p]
	}
	n, _ := cur.(float64)
	return n
}

func items(r resp, field string) []map[string]any {
	raw, _ := r.body[field].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func code(r resp) string { return str(r.body, "code") }

// Vai và người dùng mẫu.
type people struct {
	national, admin89, admin92, off30001a, off30001b, off30002, off31001, coop1 caller
}

// seedPeople tạo người dùng và đăng ký danh bạ: hai cán bộ xã 30001, một ở 30002, một ở 31001.
func (f *fixture) seedPeople() people {
	var p people
	p.national = f.as(f.org.national, port.RoleKNNationalAdmin)
	p.admin89 = f.as(f.org.p89, port.RoleKNProvinceAdmin)
	p.admin92 = f.as(f.org.p92, port.RoleKNProvinceAdmin)
	p.off30001a = f.as(f.org.t30001, port.RoleKNOfficer)
	p.off30001b = f.as(f.org.t30001, port.RoleKNOfficer)
	p.off30002 = f.as(f.org.t30002, port.RoleKNOfficer)
	p.off31001 = f.as(f.org.t31001, port.RoleKNOfficer)
	p.coop1 = f.as(f.org.c1, port.RoleCoopManager)
	for _, s := range []struct {
		by   caller
		who  caller
		unit string
	}{
		{p.admin89, p.off30001a, "30001"}, {p.admin89, p.off30001b, "30001"}, {p.admin89, p.off30002, "30002"},
		{p.admin92, p.off31001, "31001"}, {p.admin89, p.admin89, "89"}, {p.admin92, p.admin92, "92"},
	} {
		s.by.must(s.by.post("/v1/extension/staff", map[string]any{
			"actor_ref": s.who.actor.String(), "unit_code": s.unit, "role_title": "Cán bộ khuyến nông", "display_name": "Cán bộ " + s.unit,
		}), http.StatusOK, "đăng ký danh bạ")
	}
	return p
}

func taskBody(commune string) map[string]any {
	return map[string]any{"commune_code": commune, "category": "sau_benh", "description": "Rầy nâu xuất hiện mật độ cao ở cánh đồng phía đông", "priority": "high"}
}

func (c caller) createTask(commune string, extra map[string]any) map[string]any {
	c.f.t.Helper()
	b := taskBody(commune)
	for k, v := range extra {
		b[k] = v
	}
	return c.must(c.post("/v1/extension/tasks", b), http.StatusCreated, "tạo nhiệm vụ").body
}

// US-TASK-02 AC-2: due_at = lúc tạo + SLA đọc từ tham số TẠI THỜI ĐIỂM TẠO; đổi
// tham số sau đó không đổi hạn của nhiệm vụ đã có. Chưa có tham số → 24 giờ.
func TestTaskSLAFromParamAtCreation(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()

	def := p.off30001a.createTask("30001", nil)
	if str(def, "sla_hours") != "24" || str(def, "due_at") != t0.Add(24*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("SLA mặc định phải 24 giờ: %v %v", def["sla_hours"], def["due_at"])
	}
	f.params.set(port.ParamTaskSLAHours, "12")
	a := p.off30001a.createTask("30001", nil)
	if str(a, "due_at") != t0.Add(12*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("due_at phải = tạo + 12 giờ, nhận %v", a["due_at"])
	}
	f.params.set(port.ParamTaskSLAHours, "48.5")
	f.clk.Advance(time.Hour)
	b := p.off30001a.createTask("30001", nil)
	if str(b, "due_at") != t0.Add(time.Hour+48*time.Hour+30*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("due_at phải = tạo + 48,5 giờ, nhận %v", b["due_at"])
	}
	again := p.off30001a.must(p.off30001a.get("/v1/extension/tasks/"+str(a, "task_id")), http.StatusOK, "đọc lại")
	if str(again.body, "task", "due_at") != str(a, "due_at") || str(again.body, "task", "sla_hours") != "12" {
		t.Fatalf("đổi tham số không được đổi hạn nhiệm vụ đã tạo: %v", again.body["task"])
	}
	if str(a, "province_code") != "89" || str(a, "status") != "new" || str(a, "source") != "officer" {
		t.Fatalf("thuộc tính sai: %v", a)
	}
}

// US-TASK-02 AC-2: job leo thang chỉ lấy nhiệm vụ quá hạn chưa hoàn thành; chạy lại không leo thang lần hai.
func TestEscalationJobOnlyOverdueAndIdempotent(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	overdue := p.off30001a.createTask("30001", map[string]any{"assignee_ref": p.off30001a.actor.String()})
	resolved := p.off30001a.createTask("30001", map[string]any{"assignee_ref": p.off30001a.actor.String()})
	p.off30001a.must(p.off30001a.post("/v1/extension/tasks/"+str(resolved, "task_id")+"/status",
		map[string]any{"action": "resolved", "note": "Đã hướng dẫn phun thuốc đúng liều, rầy giảm"}), http.StatusOK, "hoàn thành")
	f.clk.Advance(20 * time.Hour)
	fresh := p.off30001a.createTask("30001", nil)
	f.clk.Advance(5 * time.Hour) // t0 + 25h: chỉ "overdue" quá hạn

	lister := extension.OverdueTenants(f.env.Pool(t, migrations.PF, dbadmin.OpsJobs))
	tids, err := lister(context.Background(), f.clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range tids {
		found = found || id == f.tenant
	}
	if !found {
		t.Fatal("bộ tìm tenant phải thấy tenant có nhiệm vụ quá hạn")
	}
	job := f.mod.Jobs(lister, nil)
	if len(job) != 1 || job[0].Name != extension.JobEscalation {
		t.Fatalf("Jobs: %+v", job)
	}
	for i := 0; i < 2; i++ {
		if err := job[0].Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	get := func(id string) map[string]any {
		return p.admin89.must(p.admin89.get("/v1/extension/tasks/"+id), http.StatusOK, "đọc").body
	}
	o := get(str(overdue, "task_id"))
	if str(o, "task", "escalated_at") == "" || str(o, "task", "escalated_unit") != "89" || str(o, "task", "escalation_org_ref") != f.org.p89.String() {
		t.Fatalf("nhiệm vụ quá hạn phải leo thang lên tỉnh 89: %v", o["task"])
	}
	escalations := 0
	for _, e := range o["events"].([]any) {
		if e.(map[string]any)["action"] == "escalated" {
			escalations++
		}
	}
	if escalations != 1 {
		t.Fatalf("chạy job hai lần chỉ được một sự việc leo thang, nhận %d", escalations)
	}
	for _, id := range []string{str(resolved, "task_id"), str(fresh, "task_id")} {
		if s := str(get(id), "task", "escalated_at"); s != "" {
			t.Fatalf("nhiệm vụ %s không quá hạn/đã xong không được leo thang", id)
		}
	}
	if n := f.outboxCount(port.EventTaskEscalated, str(overdue, "task_id")); n != 1 {
		t.Fatalf("phải phát đúng một pf.extension.task.escalated, có %d", n)
	}
	// Danh sách lọc quá hạn.
	lst := p.admin89.must(p.admin89.get("/v1/extension/tasks?overdue=true"), http.StatusOK, "lọc quá hạn")
	if len(items(lst, "items")) != 1 {
		t.Fatalf("chỉ một nhiệm vụ quá hạn, nhận %d", len(items(lst, "items")))
	}
}

func (f *fixture) outboxCount(eventType, taskID string) int {
	f.t.Helper()
	var n int
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1
		    AND ($2 = '' OR payload->>'task_id' = $2)`, eventType, taskID).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// ADR-0009: cán bộ xã khác không thấy, không phân công được nhiệm vụ; người được giao phải phụ trách xã.
func TestOfficerOutsideCommuneCannotSeeOrAssign(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	task := p.off30001a.createTask("30001", nil)
	id := str(task, "task_id")

	if r := p.off30002.get("/v1/extension/tasks/" + id); r.status != http.StatusNotFound {
		t.Fatalf("cán bộ xã 30002 không được thấy nhiệm vụ xã 30001: %d", r.status)
	}
	if r := p.off30002.post("/v1/extension/tasks/"+id+"/assign", map[string]any{"assignee_ref": p.off30002.actor.String()}); r.status != http.StatusNotFound {
		t.Fatalf("cán bộ xã khác không được phân công: %d %v", r.status, r.body)
	}
	if n := len(items(p.off30002.get("/v1/extension/tasks"), "items")); n != 0 {
		t.Fatalf("danh sách của xã 30002 phải rỗng, có %d", n)
	}
	if r := p.off30002.post("/v1/extension/tasks", taskBody("30001")); r.status != http.StatusForbidden || code(r) != "extension.out_of_scope" {
		t.Fatalf("tạo nhiệm vụ ngoài xã phụ trách: %d %v", r.status, r.body)
	}
	// Tỉnh thấy, nhưng không giao được cho cán bộ xã khác.
	if r := p.admin89.post("/v1/extension/tasks/"+id+"/assign", map[string]any{"assignee_ref": p.off30002.actor.String()}); r.status != http.StatusUnprocessableEntity || code(r) != "extension.assignee_invalid" {
		t.Fatalf("giao cho cán bộ không phụ trách xã: %d %v", r.status, r.body)
	}
	if r := p.admin92.get("/v1/extension/tasks/" + id); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác không được thấy: %d", r.status)
	}
	ok := p.admin89.must(p.admin89.post("/v1/extension/tasks/"+id+"/assign", map[string]any{"assignee_ref": p.off30001b.actor.String()}), http.StatusOK, "phân công")
	if str(ok.body, "task", "status") != "assigned" {
		t.Fatalf("trạng thái sau phân công: %v", ok.body)
	}
	// Chỉ người được giao tiếp nhận; kết quả là bắt buộc khi đóng (US-TASK-02 AC-1).
	if r := p.off30001a.post("/v1/extension/tasks/"+id+"/status", map[string]any{"action": "accepted"}); r.status != http.StatusForbidden || code(r) != "extension.not_assignee" {
		t.Fatalf("người không được giao tiếp nhận: %d %v", r.status, r.body)
	}
	p.off30001b.must(p.off30001b.post("/v1/extension/tasks/"+id+"/status", map[string]any{"action": "accepted"}), http.StatusOK, "tiếp nhận")
	if r := p.off30001b.post("/v1/extension/tasks/"+id+"/status", map[string]any{"action": "resolved", "note": "xong"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("đóng không có kết quả cụ thể phải bị chặn: %d", r.status)
	}
	if r := p.off30001b.post("/v1/extension/tasks/"+id+"/status", map[string]any{"action": "progress", "note": "Gọi 0912345678 để hẹn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("ghi chú chứa SĐT phải bị chặn: %d", r.status)
	}
	done := p.off30001b.must(p.off30001b.post("/v1/extension/tasks/"+id+"/status",
		map[string]any{"action": "resolved", "note": "Đã hướng dẫn phun thuốc đặc trị, kiểm tra lại sau 3 ngày"}), http.StatusOK, "hoàn thành")
	if str(done.body, "task", "status") != "resolved" || str(done.body, "task", "resolved_at") == "" {
		t.Fatalf("hoàn thành: %v", done.body)
	}
	if r := p.off30001b.post("/v1/extension/tasks/"+id+"/status", map[string]any{"action": "accepted"}); r.status != http.StatusConflict || code(r) != "extension.task_state_conflict" {
		t.Fatalf("thao tác trên nhiệm vụ đã đóng: %d %v", r.status, r.body)
	}
}

// Quản lý HTX chỉ tạo yêu cầu cho HTX của mình; nhiệm vụ nhận TTKN tỉnh làm nơi leo thang;
// mô tả chứa SĐT bị chặn (R-04).
func TestCoopManagerCreatesForOwnCooperative(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	if r := p.coop1.post("/v1/extension/tasks", map[string]any{"commune_code": "30001", "category": "ky_thuat",
		"description": "Nhờ hướng dẫn bón phân đợt 2", "cooperative_ref": f.org.c2.String()}); r.status != http.StatusForbidden {
		t.Fatalf("tạo cho HTX khác: %d %v", r.status, r.body)
	}
	if r := p.coop1.post("/v1/extension/tasks", map[string]any{"commune_code": "30001", "category": "ky_thuat",
		"description": "Liên hệ ông Ba số 0987 654 321 để được hỗ trợ"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mô tả chứa SĐT phải bị chặn: %d %v", r.status, r.body)
	}
	task := p.coop1.createTask("30001", map[string]any{"category": "co_gioi"})
	if str(task, "source") != "coop_manager" || str(task, "cooperative_ref") != f.org.c1.String() || str(task, "escalation_org_ref") != f.org.p89.String() {
		t.Fatalf("nhiệm vụ của HTX: %v", task)
	}
	other := p.off30001a.createTask("30001", nil)
	lst := items(p.coop1.get("/v1/extension/tasks"), "items")
	if len(lst) != 1 || lst[0]["task_id"] != str(task, "task_id") {
		t.Fatalf("quản lý HTX chỉ thấy nhiệm vụ của HTX mình: %v", lst)
	}
	if r := p.coop1.get("/v1/extension/tasks/" + str(other, "task_id")); r.status != http.StatusNotFound {
		t.Fatalf("không thấy nhiệm vụ không thuộc HTX: %d", r.status)
	}
	if r := p.off30001a.post("/v1/extension/tasks", map[string]any{"commune_code": "30001", "category": "khac",
		"description": "Hỗ trợ HTX của xã bên cạnh", "cooperative_ref": f.org.c2.String()}); r.status != http.StatusUnprocessableEntity || code(r) != "extension.cooperative_invalid" {
		t.Fatalf("HTX không thuộc xã: %d %v", r.status, r.body)
	}
}

// US-TASK-04: phân công hàng loạt trong một lần xác nhận; một nhiệm vụ lỗi → không nhiệm vụ nào đổi.
func TestBulkAssign(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	a, b, c := p.off30001a.createTask("30001", nil), p.off30001a.createTask("30001", nil), p.off30002.createTask("30002", nil)
	ids3 := []string{str(a, "task_id"), str(b, "task_id"), str(c, "task_id")}
	if r := p.admin89.post("/v1/extension/tasks/bulk-assign", map[string]any{"task_ids": ids3,
		"assignee_refs": []string{p.off30001a.actor.String(), p.off30001b.actor.String()}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("nhiệm vụ xã 30002 giao cho cán bộ 30001 phải lỗi cả lô: %d %v", r.status, r.body)
	}
	if st := str(p.admin89.get("/v1/extension/tasks/"+ids3[0]).body, "task", "status"); st != "new" {
		t.Fatalf("lô lỗi không được đổi nhiệm vụ nào: %s", st)
	}
	r := p.admin89.must(p.admin89.post("/v1/extension/tasks/bulk-assign", map[string]any{"task_ids": ids3[:2],
		"assignee_refs": []string{p.off30001a.actor.String(), p.off30001b.actor.String()}}), http.StatusOK, "phân công hàng loạt")
	got := items(r, "items")
	if len(got) != 2 || got[0]["assignee_ref"] != p.off30001a.actor.String() || got[1]["assignee_ref"] != p.off30001b.actor.String() {
		t.Fatalf("chia vòng tròn: %v", got)
	}
	if r := p.off30001a.post("/v1/extension/tasks/bulk-assign", map[string]any{"task_ids": ids3[:1], "assignee_refs": []string{p.off30001a.actor.String()}}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không phân công hàng loạt: %d", r.status)
	}
}

func sowing(commune, coop, area string) map[string]any {
	return map[string]any{"kind": "sowing_progress", "commune_code": commune, "cooperative_ref": coop, "period": "2026-09",
		"season_code": "HT", "variety_code": "OM5451", "sown_area_ha": area, "sown_on": "2026-09-10"}
}

// US-DASH-01 AC-2: số liệu toàn quốc bằng đúng tổng các tỉnh; US-DASH-02 AC-1: thiếu báo cáo xã → "tạm tính".
func TestNationalKPIEqualsSumOfProvinces(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "10.5")), http.StatusCreated, "báo cáo 30001")
	p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", map[string]any{"kind": "sowing_progress", "commune_code": "30001",
		"cooperative_ref": f.org.c1.String(), "period": "2026-09", "season_code": "HT", "variety_code": "ST25", "sown_area_ha": "2.25"}), http.StatusCreated, "báo cáo giống 2")
	p.off31001.must(p.off31001.post("/v1/extension/field-reports", sowing("31001", f.org.c3.String(), "7.1234")), http.StatusCreated, "báo cáo 31001")
	p.off30001a.createTask("30001", nil)
	p.off31001.createTask("31001", nil)
	p.off31001.createTask("31001", nil)
	d := p.admin89.must(p.admin89.post("/v1/extension/directives", map[string]any{"title": "Phòng trừ rầy nâu", "body": "Tổ chức thăm đồng, phun thuốc tập trung",
		"audience_units": []string{"30001"}}), http.StatusCreated, "chỉ đạo")
	p.off30001a.must(p.off30001a.post("/v1/extension/directives/"+str(d.body, "directive_id")+"/ack", nil), http.StatusOK, "xác nhận")

	nat := p.national.must(p.national.get("/v1/extension/dashboard?period=2026-09"), http.StatusOK, "toàn quốc").body
	p89 := p.national.must(p.national.get("/v1/extension/dashboard?unit=89&period=2026-09"), http.StatusOK, "tỉnh 89").body
	p92 := p.admin92.must(p.admin92.get("/v1/extension/dashboard"), http.StatusOK, "tỉnh 92 mặc định").body
	if str(p92, "unit") != "92" || str(p92, "level") != "province" {
		t.Fatalf("dashboard mặc định của TTKN tỉnh phải là tỉnh: %v %v", p92["unit"], p92["level"])
	}
	sum := func(field string) decimal.Decimal {
		return decimal.RequireFromString(str(p89, "total", field)).Add(decimal.RequireFromString(str(p92, "total", field)))
	}
	if !decimal.RequireFromString(str(nat, "total", "sown_area_ha")).Equal(sum("sown_area_ha")) ||
		str(nat, "total", "sown_area_ha") != "19.8734" {
		t.Fatalf("diện tích toàn quốc %s ≠ tổng tỉnh %s", str(nat, "total", "sown_area_ha"), sum("sown_area_ha"))
	}
	for _, fld := range []string{"cooperatives_reporting", "open_tasks", "overdue_tasks", "directive_targets", "directive_acks", "expected_communes", "reported_communes"} {
		if num(nat, "total", fld) != num(p89, "total", fld)+num(p92, "total", fld) {
			t.Fatalf("%s: toàn quốc %v ≠ %v + %v", fld, num(nat, "total", fld), num(p89, "total", fld), num(p92, "total", fld))
		}
	}
	if num(nat, "total", "open_tasks") != 3 || num(nat, "total", "cooperatives_reporting") != 2 {
		t.Fatalf("KPI toàn quốc: %v", nat["total"])
	}
	// Con của toàn quốc là các tỉnh, trùng số với dashboard từng tỉnh.
	for _, ch := range items(resp{body: nat}, "children") {
		want := p89
		if ch["unit"] == "92" {
			want = p92
		}
		if str(ch, "kpi", "sown_area_ha") != str(want, "total", "sown_area_ha") {
			t.Fatalf("con %v lệch dashboard tỉnh", ch["unit"])
		}
	}
	// Xã 30002 có cán bộ nhưng chưa báo cáo → tỉnh 89 và toàn quốc "tạm tính"; tỉnh 92 đủ.
	if b, _ := p89["total"].(map[string]any)["provisional"].(bool); !b || str(p89, "total", "provisional_label") != "Số liệu tạm tính" {
		t.Fatalf("tỉnh 89 phải tạm tính: %v", p89["total"])
	}
	if b, _ := p92["total"].(map[string]any)["provisional"].(bool); b {
		t.Fatalf("tỉnh 92 đủ báo cáo: %v", p92["total"])
	}
	if b, _ := nat["total"].(map[string]any)["provisional"].(bool); !b {
		t.Fatal("toàn quốc phải tạm tính khi có xã thiếu")
	}
	if str(p89, "total", "directive_ack_rate") != "0.5" {
		t.Fatalf("tỷ lệ xác nhận 1/2: %v", p89["total"])
	}
	// Phạm vi: cán bộ xã không xem được tỉnh; tỉnh 92 không xem tỉnh 89.
	if r := p.off30001a.get("/v1/extension/dashboard?unit=89"); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã xem tỉnh: %d", r.status)
	}
	if r := p.admin92.get("/v1/extension/dashboard?unit=89"); r.status != http.StatusForbidden {
		t.Fatalf("tỉnh 92 xem tỉnh 89: %d", r.status)
	}
	com := p.off30001a.must(p.off30001a.get("/v1/extension/dashboard"), http.StatusOK, "xã").body
	if str(com, "level") != "commune" || str(com, "total", "sown_area_ha") != "12.75" {
		t.Fatalf("dashboard xã: %v", com)
	}
}

// US-DASH-02 AC-2: số liệu đã chốt kỳ không sửa; chỉ bản ghi bù kèm lý do. Kỳ còn mở: gửi lại = phiên bản mới.
func TestClosedPeriodNeedsCorrectionReason(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	first := p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "10")), http.StatusCreated, "báo cáo").body
	// Kỳ mở: gửi lại cùng khóa tạo phiên bản 2, không cần lý do; tổng hợp chỉ lấy bản mới.
	second := p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "12")), http.StatusCreated, "gửi lại").body
	if num(second, "version") != 2 || str(second, "previous_id") != str(first, "id") {
		t.Fatalf("gửi lại khi kỳ mở phải là phiên bản 2: %v", second)
	}
	if r := p.off30001a.post("/v1/extension/field-reports", map[string]any{"kind": "sowing_progress", "commune_code": "30001",
		"cooperative_ref": f.org.c1.String(), "period": "2026-09", "season_code": "HT", "variety_code": "KHONGCO", "sown_area_ha": "1"}); r.status != http.StatusUnprocessableEntity || code(r) != "extension.catalog_code_invalid" {
		t.Fatalf("giống ngoài danh mục (BR-01): %d %v", r.status, r.body)
	}
	if r := p.off30001a.post("/v1/extension/report-periods/close", map[string]any{"period": "2026-09", "province_code": "89"}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không chốt kỳ: %d", r.status)
	}
	if r := p.admin92.post("/v1/extension/report-periods/close", map[string]any{"period": "2026-09", "province_code": "89"}); r.status != http.StatusForbidden {
		t.Fatalf("tỉnh khác không chốt kỳ: %d", r.status)
	}
	p.admin89.must(p.admin89.post("/v1/extension/report-periods/close", map[string]any{"period": "2026-09", "province_code": "89"}), http.StatusCreated, "chốt kỳ")
	p.admin89.must(p.admin89.post("/v1/extension/report-periods/close", map[string]any{"period": "2026-09", "province_code": "89"}), http.StatusOK, "chốt lặp")

	if r := p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "15")); r.status != http.StatusConflict || code(r) != "extension.period_closed" {
		t.Fatalf("gửi báo cáo mới vào kỳ đã chốt: %d %v", r.status, r.body)
	}
	corr := "/v1/extension/field-reports/" + str(second, "id") + "/corrections"
	if r := p.off30001a.post(corr, map[string]any{"sown_area_ha": "15"}); r.status != http.StatusUnprocessableEntity || code(r) != "extension.correction_reason_required" {
		t.Fatalf("bản ghi bù thiếu lý do: %d %v", r.status, r.body)
	}
	if r := p.off30001a.post("/v1/extension/field-reports/"+str(first, "id")+"/corrections",
		map[string]any{"sown_area_ha": "15", "correction_reason": "Đo lại diện tích bằng GPS"}); r.status != http.StatusConflict || code(r) != "extension.report_superseded" {
		t.Fatalf("sửa trên phiên bản cũ: %d %v", r.status, r.body)
	}
	ok := p.off30001a.must(p.off30001a.post(corr, map[string]any{"sown_area_ha": "15", "correction_reason": "Đo lại diện tích bằng GPS"}), http.StatusCreated, "bản ghi bù").body
	if num(ok, "version") != 3 || str(ok, "supersedes_id") != str(second, "id") || str(ok, "variety_code") != "OM5451" {
		t.Fatalf("bản ghi bù: %v", ok)
	}
	dash := p.admin89.must(p.admin89.get("/v1/extension/dashboard?unit=89&period=2026-09"), http.StatusOK, "dashboard").body
	if str(dash, "total", "sown_area_ha") != "15" {
		t.Fatalf("tổng hợp phải lấy phiên bản hiện hành: %v", dash["total"])
	}
	if b, _ := dash["period_closed"].(bool); !b {
		t.Fatalf("dashboard phải báo kỳ đã chốt: %v", dash)
	}
	hist := p.off30001a.must(p.off30001a.get("/v1/extension/field-reports/"+str(ok, "id")), http.StatusOK, "lịch sử").body
	if len(hist["versions"].([]any)) != 3 {
		t.Fatalf("giữ đủ 3 phiên bản: %v", hist["versions"])
	}
	// Tỉnh 92 chưa chốt: vẫn nhận báo cáo.
	p.off31001.must(p.off31001.post("/v1/extension/field-reports", sowing("31001", f.org.c3.String(), "3")), http.StatusCreated, "tỉnh chưa chốt")
}

// US-TASK-03 AC: chỉ đạo chỉ tới đúng vùng, có xác nhận đã đọc; tỷ lệ xác nhận.
func TestDirectiveAckRate(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	if r := p.admin89.post("/v1/extension/directives", map[string]any{"title": "Cảnh báo bão", "body": "Chuẩn bị thu hoạch sớm",
		"audience_units": []string{"31001"}}); r.status != http.StatusForbidden {
		t.Fatalf("tỉnh 89 gửi chỉ đạo sang tỉnh 92: %d", r.status)
	}
	if r := p.off30001a.post("/v1/extension/directives", map[string]any{"title": "Cảnh báo bão", "body": "Chuẩn bị thu hoạch sớm",
		"audience_units": []string{"30001"}}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không ban hành chỉ đạo: %d", r.status)
	}
	if r := p.admin89.post("/v1/extension/directives", map[string]any{"title": "Cảnh báo bão", "body": "Chuẩn bị thu hoạch sớm",
		"audience_units": []string{}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("không chọn vùng phải bị chặn: %d", r.status)
	}
	d := p.admin89.must(p.admin89.post("/v1/extension/directives", map[string]any{"title": "Cảnh báo rầy nâu", "body": "Thăm đồng hằng ngày, báo cáo mật độ",
		"audience_units": []string{"30001"}}), http.StatusCreated, "ban hành").body
	id := str(d, "directive_id")
	if num(d, "target_count") != 2 || d["ack_rate"] != "0" {
		t.Fatalf("hai cán bộ xã 30001 nhận: %v", d)
	}
	if r := p.off30002.post("/v1/extension/directives/"+id+"/ack", nil); r.status != http.StatusForbidden || code(r) != "extension.not_directive_target" {
		t.Fatalf("người ngoài vùng xác nhận: %d %v", r.status, r.body)
	}
	if r := p.off30002.get("/v1/extension/directives/" + id); r.status != http.StatusNotFound {
		t.Fatalf("người ngoài vùng xem chỉ đạo: %d", r.status)
	}
	a1 := p.off30001a.must(p.off30001a.post("/v1/extension/directives/"+id+"/ack", nil), http.StatusOK, "xác nhận").body
	a2 := p.off30001a.must(p.off30001a.post("/v1/extension/directives/"+id+"/ack", nil), http.StatusOK, "xác nhận lặp").body
	if a1["acked_at"] != a2["acked_at"] || num(a2, "ack_count") != 1 || a2["ack_rate"] != "0.5" {
		t.Fatalf("xác nhận lặp giữ lần đầu, tỷ lệ 0.5: %v %v", a1, a2)
	}
	sup := p.admin89.must(p.admin89.get("/v1/extension/directives/"+id), http.StatusOK, "giám sát").body
	pend := sup["pending_targets"].([]any)
	if len(pend) != 1 || pend[0].(map[string]any)["actor_ref"] != p.off30001b.actor.String() {
		t.Fatalf("danh sách chưa xác nhận: %v", pend)
	}
	mine := items(p.off30001b.get("/v1/extension/directives"), "items")
	if len(mine) != 1 || mine[0]["is_target"] != true {
		t.Fatalf("người nhận thấy chỉ đạo của mình: %v", mine)
	}
	if n := len(items(p.off31001.get("/v1/extension/directives"), "items")); n != 0 {
		t.Fatalf("không gửi tràn: cán bộ tỉnh khác thấy %d chỉ đạo", n)
	}
	if n := f.outboxCount(port.EventDirectiveIssued, ""); n != 1 {
		t.Fatalf("phát đúng một pf.extension.directive.issued, có %d", n)
	}
}

func (f *fixture) syncCtx(c caller) context.Context {
	p := authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: c.roles}
	return tenant.WithID(authz.WithPrincipal(context.Background(), p), c.tid)
}

// sync-contract: bản ghi gửi lặp → duplicate; kỳ đã chốt mà sửa không lý do → rejected.
func TestSyncSinkDuplicatesAndClosedPeriod(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	task := p.admin89.createTask("30001", map[string]any{"assignee_ref": p.off30001a.actor.String()})
	sink, src := f.mod.SyncSink(), f.mod.SyncSource()
	ctx := f.syncCtx(p.off30001a)
	scope := authz.Scope{Units: []string{"30001"}}
	dev := ids.New()

	changes, last, err := src.Changes(ctx, scope, port.EntityTask, 0, 100)
	if err != nil || len(changes) != 1 || changes[0].ID.String() != str(task, "task_id") || last == 0 {
		t.Fatalf("chiều xuống task: %v %d %v", changes, last, err)
	}
	upd := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":1", Entity: port.EntityTaskUpdate, Op: syncport.OpCreate,
		EventTime: f.clk.Now().Add(-time.Hour), DeviceID: dev, Payload: json.RawMessage(`{"task_id":"` + str(task, "task_id") + `","action":"accepted"}`)}
	if r := sink.Accept(ctx, scope, upd); r.Status != syncport.StatusAccepted {
		t.Fatalf("task_update: %+v", r)
	}
	if r := sink.Accept(ctx, scope, upd); r.Status != syncport.StatusDuplicate {
		t.Fatalf("task_update gửi lặp phải duplicate: %+v", r)
	}
	again, _, err := src.Changes(ctx, scope, port.EntityTask, last, 100)
	if err != nil || len(again) != 1 || !bytes.Contains(again[0].Data, []byte(`"status":"accepted"`)) {
		t.Fatalf("thay đổi sau cập nhật: %v %v", again, err)
	}
	other := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":2", Entity: port.EntityTaskUpdate, Op: syncport.OpCreate,
		DeviceID: dev, Payload: json.RawMessage(`{"task_id":"` + str(task, "task_id") + `","action":"accepted"}`)}
	if r := sink.Accept(ctx, scope, other); r.Status != syncport.StatusRejected || r.Code != "extension.task_state_conflict" {
		t.Fatalf("tiếp nhận hai lần: %+v", r)
	}
	// Ngoài phạm vi: cán bộ xã 30002 cập nhật nhiệm vụ xã 30001.
	ctx2 := f.syncCtx(p.off30002)
	if r := sink.Accept(ctx2, authz.Scope{Units: []string{"30002"}}, syncport.Record{ID: ids.New(), IdemKey: "dev:x:1", Entity: port.EntityTaskUpdate,
		Op: syncport.OpCreate, Payload: json.RawMessage(`{"task_id":"` + str(task, "task_id") + `","action":"progress","note":"đang xử lý"}`)}); r.Status != syncport.StatusRejected || r.Code != "extension.task_not_found" {
		t.Fatalf("ngoài phạm vi: %+v", r)
	}

	reportPayload, _ := json.Marshal(sowing("30001", f.org.c1.String(), "8"))
	rep := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":3", Entity: port.EntityFieldReport, Op: syncport.OpCreate,
		EventTime: f.clk.Now(), DeviceID: dev, Payload: reportPayload}
	if r := sink.Accept(ctx, scope, rep); r.Status != syncport.StatusAccepted {
		t.Fatalf("field_report: %+v", r)
	}
	if r := sink.Accept(ctx, scope, rep); r.Status != syncport.StatusDuplicate {
		t.Fatalf("field_report gửi lặp: %+v", r)
	}
	p.admin89.must(p.admin89.post("/v1/extension/report-periods/close", map[string]any{"period": "2026-09", "province_code": "89"}), http.StatusCreated, "chốt kỳ")
	sup := rep.ID
	noReason := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":4", Entity: port.EntityFieldReport, Op: syncport.OpCorrect,
		SupersedesID: &sup, DeviceID: dev, Payload: json.RawMessage(`{"sown_area_ha":"9"}`)}
	if r := sink.Accept(ctx, scope, noReason); r.Status != syncport.StatusRejected || r.Code != "extension.correction_reason_required" {
		t.Fatalf("kỳ đã chốt, sửa không lý do: %+v", r)
	}
	late := syncport.Record{ID: ids.New(), IdemKey: "dev:" + dev.String() + ":5", Entity: port.EntityFieldReport, Op: syncport.OpCreate,
		DeviceID: dev, Payload: reportPayload}
	if r := sink.Accept(ctx, scope, late); r.Status != syncport.StatusRejected || r.Code != "extension.period_closed" {
		t.Fatalf("báo cáo mới vào kỳ đã chốt: %+v", r)
	}
	reason := "Đo lại bằng thiết bị GPS cầm tay"
	withReason := noReason
	withReason.ID, withReason.IdemKey, withReason.CorrectionReason = ids.New(), "dev:"+dev.String()+":6", &reason
	if r := sink.Accept(ctx, scope, withReason); r.Status != syncport.StatusAccepted {
		t.Fatalf("bản ghi bù có lý do: %+v", r)
	}
	got := p.off30001a.must(p.off30001a.get("/v1/extension/field-reports/"+withReason.ID.String()), http.StatusOK, "đọc bản ghi bù").body
	if str(got, "report", "sown_area_ha") != "9" || num(got, "report", "version") != 2 {
		t.Fatalf("bản ghi bù qua đồng bộ: %v", got["report"])
	}
	// Chiều xuống chỉ đạo: người nhận thấy.
	p.admin89.must(p.admin89.post("/v1/extension/directives", map[string]any{"title": "Cảnh báo rầy", "body": "Thăm đồng hằng ngày",
		"audience_units": []string{"30001"}}), http.StatusCreated, "chỉ đạo")
	dch, _, err := src.Changes(ctx, scope, port.EntityDirective, 0, 100)
	if err != nil || len(dch) != 1 {
		t.Fatalf("chiều xuống chỉ đạo: %v %v", dch, err)
	}
	if n, _, _ := src.Changes(f.syncCtx(p.off31001), authz.Scope{Units: []string{"31001"}}, port.EntityDirective, 0, 100); len(n) != 0 {
		t.Fatalf("cán bộ tỉnh khác không nhận chỉ đạo: %v", n)
	}
}

// RB-2: tenant khác không thấy gì, kể cả khi biết id.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	task := p.off30001a.createTask("30001", nil)
	other := f.asTenant(ids.New(), f.org.p89, port.RoleKNProvinceAdmin)
	if r := other.get("/v1/extension/tasks/" + str(task, "task_id")); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác đọc nhiệm vụ: %d", r.status)
	}
	if n := len(items(other.get("/v1/extension/tasks"), "items")); n != 0 {
		t.Fatalf("tenant khác thấy %d nhiệm vụ", n)
	}
	if r := other.post("/v1/extension/tasks/"+str(task, "task_id")+"/assign", map[string]any{"assignee_ref": p.off30001a.actor.String()}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác phân công: %d", r.status)
	}
	if n := len(items(other.get("/v1/extension/roster?unit=30001"), "items")); n != 0 {
		t.Fatalf("tenant khác thấy danh bạ: %d", n)
	}
	d := other.must(other.get("/v1/extension/dashboard?unit=89"), http.StatusOK, "dashboard tenant khác").body
	if str(d, "total", "sown_area_ha") != "0" || num(d, "total", "open_tasks") != 0 {
		t.Fatalf("dashboard tenant khác phải rỗng: %v", d["total"])
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi, không ghi hai lần.
func TestIdempotentPOST(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	k := key()
	r1 := p.off30001a.must(p.off30001a.call(http.MethodPost, "/v1/extension/tasks", k, taskBody("30001")), http.StatusCreated, "lần 1")
	r2 := p.off30001a.must(p.off30001a.call(http.MethodPost, "/v1/extension/tasks", k, taskBody("30001")), http.StatusCreated, "lần 2")
	if str(r1.body, "task_id") != str(r2.body, "task_id") || r2.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("gọi lặp phải trả cùng nhiệm vụ: %v %v", r1.body, r2.hdr)
	}
	if n := len(items(p.off30001a.get("/v1/extension/tasks"), "items")); n != 1 {
		t.Fatalf("chỉ một nhiệm vụ, có %d", n)
	}
	body := taskBody("30001")
	body["description"] = "Một mô tả khác cho cùng khóa"
	if r := p.off30001a.call(http.MethodPost, "/v1/extension/tasks", k, body); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("cùng khóa khác thân phải bị chặn: %d", r.status)
	}
	// Máy ngoại tuyến gửi id tự sinh: khóa mới nhưng cùng id → extension.id_reused, không ghi đè.
	body = taskBody("30001")
	body["id"] = ids.New().String()
	p.off30001a.must(p.off30001a.post("/v1/extension/tasks", body), http.StatusCreated, "id từ máy")
	if r := p.off30001a.post("/v1/extension/tasks", body); r.status != http.StatusConflict || code(r) != "extension.id_reused" {
		t.Fatalf("dùng lại id: %d %v", r.status, r.body)
	}
}

// US-DIR-01/02/03: trạng thái trực tự khai hết hạn → không xác định; ca trực; không ai trực → cán bộ tỉnh thay thế.
func TestRosterDutyStatus(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	f.params.set(port.ParamDutyStatusTTLHours, "8")
	r := p.admin89.must(p.admin89.get("/v1/extension/roster?unit=30001"), http.StatusOK, "danh bạ").body
	if num(r, "on_duty") != 0 || len(r["fallback"].([]any)) != 1 || str(r["fallback"].([]any)[0].(map[string]any), "actor_ref") != p.admin89.actor.String() {
		t.Fatalf("không ai trực → cán bộ TTKN tỉnh thay thế: %v", r)
	}
	p.off30001a.must(p.off30001a.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "đang trực")
	p.admin89.must(p.admin89.post("/v1/extension/roster/shifts", map[string]any{"actor_ref": p.off30001b.actor.String(),
		"starts_at": t0.Add(9 * time.Hour).Format(time.RFC3339), "ends_at": t0.Add(20 * time.Hour).Format(time.RFC3339)}), http.StatusCreated, "phân ca")
	if rr := p.off30001a.post("/v1/extension/roster/shifts", map[string]any{"actor_ref": p.off30001b.actor.String(),
		"starts_at": t0.Format(time.RFC3339), "ends_at": t0.Add(time.Hour).Format(time.RFC3339)}); rr.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không phân ca: %d", rr.status)
	}
	status := func() map[string]string {
		out := map[string]string{}
		r := p.coop1.must(p.coop1.get("/v1/extension/roster?unit=30001"), http.StatusOK, "danh bạ").body
		for _, it := range r["items"].([]any) {
			m := it.(map[string]any)
			out[m["actor_ref"].(string)] = m["duty_status"].(string) + "/" + m["status_source"].(string)
		}
		return out
	}
	s := status()
	if s[p.off30001a.actor.String()] != "on_duty/manual" || s[p.off30001b.actor.String()] != "unknown/none" {
		t.Fatalf("t0: %v", s)
	}
	f.clk.Advance(10 * time.Hour)
	s = status()
	if s[p.off30001a.actor.String()] != "unknown/none" || s[p.off30001b.actor.String()] != "on_duty/shift" {
		t.Fatalf("t0+10h: tự khai hết hạn sau 8 giờ, ca trực bắt đầu: %v", s)
	}
	if r := p.off30002.get("/v1/extension/roster?unit=30001"); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã khác tra danh bạ ngoài phạm vi: %d", r.status)
	}
	if r := p.off30002.post("/v1/extension/staff", map[string]any{"actor_ref": p.off30001a.actor.String(), "unit_code": "30002",
		"role_title": "Cán bộ", "display_name": "Người khác"}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã khai hộ người khác: %d", r.status)
	}
}

// fakeContacts là số điện thoại theo actor_ref (bản thật: pf/accounts đọc Keycloak).
type fakeContacts map[uuid.UUID]string

func (f fakeContacts) PhoneOf(_ context.Context, _, actor uuid.UUID) (string, error) {
	if p, ok := f[actor]; ok {
		return p, nil
	}
	return "", port.ErrNoPhone
}

// US-DIR-02 "gọi/nhắn 1 chạm": số điện thoại đọc lúc bấm, chỉ cho cán bộ người gọi thấy được
// trong danh bạ (kể cả cán bộ tỉnh dự phòng); không lưu đệm.
func TestRosterContactOneTap(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	path := func(c caller) string { return "/v1/extension/roster/" + c.actor.String() + "/contact" }
	if r := p.off30001a.get(path(p.off30001b)); r.status != http.StatusServiceUnavailable || code(r) != "extension.dependency_unavailable" {
		t.Fatalf("chưa nối pf/accounts: %d %v", r.status, r.body)
	}
	f.mod.UseContacts(fakeContacts{p.off30001b.actor: "0912345678", p.admin89.actor: "0292 3812 345", p.off31001.actor: "0987654321"})
	r := p.off30001a.must(p.off30001a.get(path(p.off30001b)), http.StatusOK, "cùng xã")
	if str(r.body, "phone") != "0912345678" || str(r.body, "actor_ref") != p.off30001b.actor.String() || r.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("liên hệ cùng xã: %v %v", r.body, r.hdr)
	}
	// Cán bộ TTKN tỉnh là nơi dự phòng khi xã không ai trực → cán bộ xã trong tỉnh gọi được.
	if r := p.off30001a.must(p.off30001a.get(path(p.admin89)), http.StatusOK, "tỉnh dự phòng"); str(r.body, "phone") != "0292 3812 345" {
		t.Fatalf("liên hệ cán bộ tỉnh: %v", r.body)
	}
	// TTKN tỉnh gọi cán bộ xã trong tỉnh; TTKN QG gọi mọi nơi.
	p.admin89.must(p.admin89.get(path(p.off30001b)), http.StatusOK, "tỉnh → xã")
	p.national.must(p.national.get(path(p.off31001)), http.StatusOK, "QG → xã tỉnh khác")
	// Ngoài phạm vi: 404 như không có (không lộ sự tồn tại).
	for _, c := range []struct {
		who, target caller
		what        string
	}{
		{p.off30001a, p.off31001, "xã tỉnh khác"}, {p.off30002, p.off30001b, "xã khác cùng tỉnh"}, {p.admin92, p.admin89, "tỉnh khác"},
	} {
		if r := c.who.get(path(c.target)); r.status != http.StatusNotFound || code(r) != "extension.staff_not_found" {
			t.Fatalf("%s: %d %v", c.what, r.status, r.body)
		}
	}
	if r := p.off30001b.get(path(p.off30001a)); r.status != http.StatusNotFound || code(r) != "extension.contact_unavailable" {
		t.Fatalf("chưa khai số: %d %v", r.status, r.body)
	}
	if r := p.off30001a.get("/v1/extension/roster/" + ids.New().String() + "/contact"); r.status != http.StatusNotFound || code(r) != "extension.staff_not_found" {
		t.Fatalf("không có trong danh bạ: %d %v", r.status, r.body)
	}
	if r := f.as(f.org.t30001, "mrv_viewer").get(path(p.off30001b)); r.status != http.StatusForbidden {
		t.Fatalf("vai không đọc danh bạ: %d", r.status)
	}
}
