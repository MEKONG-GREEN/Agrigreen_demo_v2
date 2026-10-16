package reportstest

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// t0: 10:00 giờ Việt Nam thứ Hai 28/09/2026.
var t0 = time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)

type fixture struct {
	t        *testing.T
	env      *pgtest.Env
	iss      *authztest.FakeIssuer
	app      *httptest.Server
	admin    *httptest.Server
	mod      *reports.Module
	pool     *pgxpool.Pool
	clk      *clock.Fixed
	params   *fakeParams
	org      *orgTree
	ext      *fakeExt
	mech     *fakeMech
	cc       *fakeCC
	store    *objstore.Local
	storeDir string
	mailer   *fakeMailer
	dir      fakeDirectory
	tenant   uuid.UUID
}

func newFixture(t *testing.T, withDirectory bool) *fixture {
	env := pgtest.Get(t)
	f := &fixture{t: t, env: env, iss: authztest.New(t), clk: clock.NewFixed(t0), params: &fakeParams{v: map[string]decimal.Decimal{}},
		org: newOrgTree(), ext: &fakeExt{}, mailer: &fakeMailer{}, tenant: ids.New(), storeDir: t.TempDir()}
	f.mech = newFakeMech(f.org.coop)
	f.cc = newFakeCC(f.org.coop, f.org.coopCaiKhe)
	f.pool = env.Pool(t, migrations.PF, reports.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	st, err := objstore.NewLocal(f.storeDir, objstore.LocalOptions{PresignSecret: []byte("reports-test-presign-secret-32-bytes!"), BaseURL: "http://files.test"})
	if err != nil {
		t.Fatal(err)
	}
	f.store = st
	f.dir = fakeDirectory{emails: map[uuid.UUID]string{}, members: map[uuid.UUID]uuid.UUID{}}
	deps := reports.Deps{Builders: extension.ReportBuilders(extension.ReportBuilderDeps{Data: f.ext, Orgs: f.org, Units: newFakeUnits(), Catalogs: fakeCatalogs{}, Clock: f.clk}), Catalog: []port.CatalogRegistration{extension.ReportCatalog()}, Policies: []port.Policy{extension.ReportPolicy()}, Scope: f.org, Orgs: f.org, Units: newFakeUnits(), Params: f.params, Catalogs: fakeCatalogs{}, Store: st,
		Extension: f.ext, Mechanization: f.mech, CropCycle: f.cc, Mailer: f.mailer, LinkBase: "https://ops.test"}
	if withDirectory {
		deps.Directory, deps.Recipients = f.dir, f.dir
	}
	f.mod = reports.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, deps)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	appEng.Use(logErrors(t))
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	adminEng.Use(logErrors(t))
	f.mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	return f
}

// logErrors in nguyên nhân lỗi 5xx vào log kiểm thử (không lộ ra client).
func logErrors(t *testing.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("%s %s: %v", c.Request.Method, c.FullPath(), e.Err)
		}
	}
}

type caller struct {
	f     *fixture
	token string
	actor uuid.UUID
}

func (f *fixture) as(org uuid.UUID, roles ...string) caller {
	return f.asTenant(f.tenant, org, roles...)
}

func (f *fixture) asTenant(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true, TTL: time.Hour})
	return caller{f: f, token: tok, actor: actor}
}

type resp struct {
	status int
	body   map[string]any
}

func (c caller) do(srv *httptest.Server, method, path, idem string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
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
	return resp{status: r.StatusCode, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp {
	return c.do(c.f.app, http.MethodPost, path, key(), body)
}
func (c caller) get(path string) resp { return c.do(c.f.app, http.MethodGet, path, "", nil) }
func (c caller) adminPost(path string, body any) resp {
	return c.do(c.f.admin, http.MethodPost, path, key(), body)
}
func (c caller) adminGet(path string) resp { return c.do(c.f.admin, http.MethodGet, path, "", nil) }

func (c caller) must(r resp, want int, what string) resp {
	c.f.t.Helper()
	if r.status != want {
		c.f.t.Fatalf("%s: muốn %d, nhận %d %v", what, want, r.status, r.body)
	}
	return r
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func code(r resp) string                    { return str(r.body, "code") }

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := []map[string]any{}
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

// events đọc sự kiện của tenant trong outbox (tài khoản quản trị, chỉ kiểm thử).
func (f *fixture) events(eventType string) []outbox.Envelope {
	f.t.Helper()
	rows, err := f.env.AdminPool(f.t, migrations.PF).Query(context.Background(), `SELECT id, tenant_id, event_type, schema_version,
	    occurred_at, recorded_at, coalesce(trace_id, ''), producer, payload FROM shared.outbox
	  WHERE tenant_id = $1 AND event_type = $2 ORDER BY id`, f.tenant, eventType)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []outbox.Envelope
	for rows.Next() {
		var e outbox.Envelope
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.TraceID, &e.Producer, &e.Payload); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// deliver giao một sự kiện cho handler qua inbox (như worker).
func (f *fixture) deliver(consumer string, env outbox.Envelope, h inbox.Handler) bool {
	f.t.Helper()
	ok, err := inbox.Process(context.Background(), f.pool, consumer, env, h)
	if err != nil {
		f.t.Fatalf("xử lý %s: %v", env.EventType, err)
	}
	return ok
}

// runWorker xử lý mọi yêu cầu xuất đang có trong outbox.
func (f *fixture) runWorker() {
	for _, e := range f.events(port.EventExportRequested) {
		f.deliver(port.ExportConsumer, e, f.mod.ExportHandler())
	}
}

func (f *fixture) objects() []string {
	var out []string
	_ = filepath.Walk(filepath.Join(f.storeDir, "objects"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func (f *fixture) file(objectKey string) []byte {
	f.t.Helper()
	rc, _, err := f.store.Get(context.Background(), objectKey)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	return b
}

func (f *fixture) sheet(b []byte) *excelize.File {
	f.t.Helper()
	x, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = x.Close() })
	return x
}

func raw(t *testing.T, x *excelize.File, cell string) string {
	t.Helper()
	v, err := x.GetCellValue("Báo cáo", cell, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// findRow trả số dòng đầu tiên có cột A = text.
func findRow(t *testing.T, x *excelize.File, text string) int {
	t.Helper()
	rows, _ := x.GetRows("Báo cáo", excelize.Options{RawCellValue: true})
	for i, r := range rows {
		if len(r) > 0 && r[0] == text {
			return i + 1
		}
	}
	t.Fatalf("không thấy dòng %q trong %v", text, rows)
	return 0
}

func cellName(col, row int) string { n, _ := excelize.CoordinatesToCellName(col, row); return n }

// ---------------------------------------------------------------------------

// US-DASH-02, R-52, R-61: 202 + job; worker qua inbox dựng Excel; giao lặp → một tệp; số tạm tính.
func TestExportXLSXAsyncViaInboxOnce(t *testing.T) {
	f := newFixture(t, false)
	admAG := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r := admAG.must(admAG.post("/v1/reports/exports", map[string]any{
		"template_key": "kn.sowing_progress", "format": "xlsx", "params": map[string]any{"period": "2026-09"},
	}), http.StatusAccepted, "yêu cầu xuất")
	id := str(r.body, "id")
	if str(r.body, "status") != "queued" || r.body["template_version"].(float64) != 0 {
		t.Fatalf("việc xuất: %v", r.body)
	}
	evs := f.events(port.EventExportRequested)
	if len(evs) != 1 {
		t.Fatalf("một sự kiện yêu cầu: %d", len(evs))
	}
	if !f.deliver(port.ExportConsumer, evs[0], f.mod.ExportHandler()) || f.deliver(port.ExportConsumer, evs[0], f.mod.ExportHandler()) {
		t.Fatal("giao lần 2 cùng event_id phải bị inbox bỏ qua")
	}
	// Giao một sự kiện KHÁC event_id cho cùng việc xuất: việc đã xong → không dựng lại.
	dup := evs[0]
	dup.EventID = ids.New()
	f.deliver(port.ExportConsumer, dup, f.mod.ExportHandler())
	if objs := f.objects(); len(objs) != 1 {
		t.Fatalf("đúng một tệp: %v", objs)
	}
	q := f.ext.lastQuery()
	if q.TenantID != f.tenant || len(q.Periods) != 1 || q.Periods[0] != "2026-09" || q.Scope.All || len(q.Scope.Units) != 3 {
		t.Fatalf("worker đọc với phạm vi đã giải lúc yêu cầu: %+v", q)
	}
	job := admAG.must(admAG.get("/v1/reports/exports/"+id), http.StatusOK, "trạng thái").body
	if job["status"] != "completed" || job["provisional"] != true || job["row_count"].(float64) != 2 {
		t.Fatalf("việc xuất xong, tạm tính: %v", job)
	}
	if n := len(f.events(port.EventExportCompleted)); n != 1 {
		t.Fatalf("một sự kiện hoàn tất: %d", n)
	}
	key := "reports/" + f.tenant.String() + "/" + id + ".xlsx"
	x := f.sheet(f.file(key))
	if got := raw(t, x, "A1"); got != "Báo cáo tiến độ gieo sạ" {
		t.Fatalf("tiêu đề: %q", got)
	}
	if got := raw(t, x, "A2"); got != "Ngày xuất: 28/09/2026 10:00" {
		t.Fatalf("ngày xuất giờ VN: %q", got)
	}
	hr := findRow(t, x, "Tỉnh/Thành phố")
	want := [][]string{
		{"Tỉnh/Thành phố", "Xã/Phường", "Vụ", "Giống", "Diện tích đã sạ (ha)", "Số HTX báo cáo"},
		{"An Giang", "Phường Bình Đức", "Hè Thu", "ST25", "1234.5678", "2"},
		{"An Giang", "Phường Bình Đức", "Hè Thu", "OM 5451", "0.1", "1"},
		{"Tổng cộng", "", "", "", "1234.6678", "3"},
	}
	for i, row := range want {
		for j, v := range row {
			if got := raw(t, x, cellName(j+1, hr+i)); got != v {
				t.Errorf("ô %s = %q, muốn %q", cellName(j+1, hr+i), got, v)
			}
		}
	}
	if got := raw(t, x, cellName(1, hr+5)); !strings.HasPrefix(got, "Số liệu tạm tính") {
		t.Fatalf("chân trang tạm tính: %q", got)
	}
	// Tải về: URL có hạn, đi qua điểm kiểm soát (ghi nhật ký download).
	dl := admAG.must(admAG.get("/v1/reports/exports/"+id+"/download"), http.StatusOK, "tải").body
	if !strings.HasPrefix(str(dl, "url"), "http://files.test/"+key) || !strings.HasSuffix(str(dl, "file_name"), ".xlsx") {
		t.Fatalf("URL tải: %v", dl)
	}
	var n int
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(),
		"SELECT count(*) FROM pf_reports.export_audit WHERE tenant_id = $1 AND kind = 'download'", f.tenant).Scan(&n); err != nil || n != 1 {
		t.Fatalf("nhật ký tải: %d %v", n, err)
	}
}

// FN-10 BR-04: tệp CGH ghi NGUYÊN số của kết quả đã lưu (không làm tròn, không tính lại); PDF định dạng kiểu Việt Nam.
func TestCGHExportEqualsSavedResults(t *testing.T) {
	f := newFixture(t, false)
	viewer := f.as(f.org.dcrd, mechport.RoleCGHViewer)
	for _, format := range []string{"xlsx", "pdf"} {
		viewer.must(viewer.post("/v1/reports/exports", map[string]any{
			"template_key": "cgh.shortage_by_coop", "format": format, "params": map[string]any{"season_code": "DX", "season_year": 2025},
		}), http.StatusAccepted, "xuất thiếu hụt "+format)
	}
	viewer.must(viewer.post("/v1/reports/exports", map[string]any{"template_key": "cgh.summary", "format": "xlsx"}), http.StatusAccepted, "tổng hợp")
	if r := viewer.post("/v1/reports/exports", map[string]any{"template_key": "cgh.season_compare", "format": "xlsx",
		"params": map[string]any{"season_code": "DX", "season_year": 2025, "season_code_b": "DX", "season_year_b": 2025}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("Vụ A = Vụ B phải bị chặn: %d", r.status)
	}
	f.runWorker()
	var xlsxShort, pdfShort, xlsxSum []byte
	for _, e := range f.events(port.EventExportCompleted) {
		var pl port.ExportCompletedV1
		_ = json.Unmarshal(e.Payload, &pl)
		b := f.file(pl.ObjectKey)
		switch {
		case pl.TemplateKey == "cgh.shortage_by_coop" && pl.Format == "xlsx":
			xlsxShort = b
		case pl.TemplateKey == "cgh.shortage_by_coop":
			pdfShort = b
		default:
			xlsxSum = b
		}
	}
	saved := f.mech.shortages[0]
	x := f.sheet(xlsxShort)
	hr := findRow(t, x, "Mã HTX")
	want := []string{"HTX000001", "HTX Lúa Xanh Bình Đức", "An Giang", "Phường Bình Đức", "Làm đất", "Máy làm đất",
		"3", "5", "2", saved.CoveragePct.String(), "Nhập tay"}
	for j, v := range want {
		if got := raw(t, x, cellName(j+1, hr+1)); got != v {
			t.Errorf("thiếu hụt ô %d = %q, muốn %q (đúng số đã lưu)", j+1, got, v)
		}
	}
	if got := raw(t, x, cellName(10, hr+1)); got != "60.0012" {
		t.Fatalf("mức đáp ứng giữ 4 chữ số đã lưu: %q", got)
	}
	s := f.sheet(xlsxSum)
	shr := findRow(t, s, "Tỉnh")
	for j, v := range []string{"An Giang", "Làm đất", "Máy làm đất", "11.2345", "9", "80.1103", "Cần chú ý"} {
		if got := raw(t, s, cellName(j+1, shr+1)); got != v {
			t.Errorf("tổng hợp ô %d = %q, muốn %q", j+1, got, v)
		}
	}
	if !bytes.HasPrefix(pdfShort, []byte("%PDF-")) || len(pdfShort) < 5000 {
		t.Fatalf("PDF thiếu hụt: %d byte", len(pdfShort))
	}
}

// ADR-0009: phạm vi theo cây — cán bộ xã chỉ xuất xã mình; tỉnh khác không thấy việc xuất; KN không xuất mẫu CGH.
func TestExportScope(t *testing.T) {
	f := newFixture(t, false)
	off := f.as(f.org.tBinhDuc, extport.RoleKNOfficer)
	if r := off.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "pdf",
		"params": map[string]any{"unit_code": cCaiKhe}}); r.status != http.StatusForbidden || code(r) != "reports.out_of_scope" {
		t.Fatalf("xã khác: %d %v", r.status, r.body)
	}
	if r := off.post("/v1/reports/exports", map[string]any{"template_key": "cgh.summary", "format": "xlsx"}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ KN không xuất mẫu CGH: %d", r.status)
	}
	id := str(off.must(off.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}),
		http.StatusAccepted, "xuất trong xã").body, "id")
	f.runWorker()
	if q := f.ext.lastQuery(); q.Scope.All || len(q.Scope.Units) != 1 || q.Scope.Units[0] != cBinhDuc || q.Periods[0] != "2026-09" {
		t.Fatalf("phạm vi xã + kỳ mặc định tháng hiện tại: %+v", q)
	}
	ct := f.as(f.org.pCT, extport.RoleKNProvinceAdmin)
	if r := ct.get("/v1/reports/exports/" + id); r.status != http.StatusNotFound {
		t.Fatalf("tỉnh khác không thấy: %d", r.status)
	}
	ag := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	ag.must(ag.get("/v1/reports/exports/"+id), http.StatusOK, "tỉnh cha thấy việc xuất của xã")
	other := f.asTenant(ids.New(), f.org.national, extport.RoleKNNationalAdmin)
	if r := other.get("/v1/reports/exports/" + id); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác: %d", r.status)
	}
	// Gọi lặp cùng Idempotency-Key → cùng việc xuất.
	k := key()
	a := off.do(f.app, http.MethodPost, "/v1/reports/exports", k, map[string]any{"template_key": "kn.sowing_progress", "format": "pdf"})
	b := off.do(f.app, http.MethodPost, "/v1/reports/exports", k, map[string]any{"template_key": "kn.sowing_progress", "format": "pdf"})
	if a.status != http.StatusAccepted || str(a.body, "id") != str(b.body, "id") {
		t.Fatalf("idempotency: %v / %v", a.body, b.body)
	}
}

// Mục 18 DLP: hạn mức ngày theo tham số → 429; vượt ngưỡng bất thường → đúng một sự kiện anomaly.
func TestQuotaAndAnomaly(t *testing.T) {
	f := newFixture(t, false)
	f.params.set(port.ParamDailyExportQuota, "3")
	f.params.set(port.ParamAnomalyThreshold, "1")
	u := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	for i := 0; i < 3; i++ {
		u.must(u.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}), http.StatusAccepted, "xuất")
	}
	r := u.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"})
	if r.status != http.StatusTooManyRequests || code(r) != "reports.export_quota_exceeded" {
		t.Fatalf("lần 4 vượt hạn mức: %d %v", r.status, r.body)
	}
	an := f.events(port.EventExportAnomaly)
	if len(an) != 1 {
		t.Fatalf("một cảnh báo khi vượt ngưỡng lần đầu: %d", len(an))
	}
	var pl port.ExportAnomalyV1
	_ = json.Unmarshal(an[0].Payload, &pl)
	if pl.ActorRef != u.actor.String() || pl.DailyCount != 2 || pl.Threshold != 1 || pl.Quota != 3 || pl.BusinessDate != "2026-09-28" {
		t.Fatalf("payload cảnh báo: %+v", pl)
	}
	// Người khác không bị ảnh hưởng; sang ngày mới (giờ VN) hạn mức đặt lại.
	o := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	o.must(o.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}), http.StatusAccepted, "người khác")
	f.clk.Advance(15 * time.Hour) // 01:00 ngày 29/09 giờ VN
	u.must(u.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}), http.StatusAccepted, "ngày mới")
}

// US-DASH-03: lịch tuần chạy đúng một lần mỗi mốc; gửi email liên kết ngắn hạn tới người nhận có email.
func TestScheduleFiresOncePerPeriodAndDelivers(t *testing.T) {
	f := newFixture(t, true)
	mgr := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r1, r2 := ids.New(), ids.New()
	f.dir.emails[r1] = "can.bo@ttkn-angiang.test"
	if r := mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 7, "recipients": []string{r1.String()}, "unit_code": cCaiKhe}); r.status != http.StatusForbidden {
		t.Fatalf("lịch ngoài phạm vi: %d", r.status)
	}
	off := f.as(f.org.tBinhDuc, extport.RoleKNOfficer)
	if r := off.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 7, "recipients": []string{r1.String()}}); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ xã không lập lịch: %d", r.status)
	}
	// Lập lịch lúc 09:00 thứ Hai, gửi 10:00: lúc lập, mốc gần nhất là tuần trước → không gửi bù.
	f.clk.Advance(-time.Hour)
	sch := mgr.must(mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 10, "recipients": []string{r1.String(), r2.String()}}), http.StatusCreated, "lập lịch").body
	job := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0]
	fire := func() {
		if err := job.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	jobs := func() int { return len(f.events(port.EventExportRequested)) }
	fire()
	if jobs() != 0 {
		t.Fatal("mốc trước khi lập lịch không chạy")
	}
	f.clk.Advance(2 * time.Hour) // 11:00 thứ Hai
	fire()
	fire()
	f.clk.Advance(48 * time.Hour)
	fire()
	if jobs() != 1 {
		t.Fatalf("một mốc tuần → đúng một việc xuất: %d", jobs())
	}
	f.clk.Advance(6 * 24 * time.Hour) // tuần sau
	fire()
	if jobs() != 2 {
		t.Fatalf("mốc tuần mới: %d", jobs())
	}
	runs := items(mgr.must(mgr.adminGet("/v1/reports/schedules/"+str(sch, "id")+"/runs"), http.StatusOK, "lần chạy"))
	if len(runs) != 2 || runs[0]["slot_key"] != "2026-W41" || runs[1]["slot_key"] != "2026-W40" {
		t.Fatalf("lần chạy theo mốc: %v", runs)
	}
	f.runWorker()
	var pl port.ExportRequestedV1
	_ = json.Unmarshal(f.events(port.EventExportRequested)[0].Payload, &pl)
	j := mgr.must(mgr.get("/v1/reports/exports/"+pl.JobID), http.StatusOK, "việc xuất theo lịch").body
	if j["trigger"] != "schedule" || j["status"] != "completed" {
		t.Fatalf("việc xuất theo lịch: %v", j)
	}
	for _, e := range f.events(port.EventExportCompleted) {
		f.deliver(port.DeliveryConsumer, e, f.mod.DeliveryHandler())
		dup := e
		dup.EventID = ids.New()
		f.deliver(port.DeliveryConsumer, dup, f.mod.DeliveryHandler()) // giao lặp: không gửi lại
	}
	msgs := f.mailer.messages()
	if len(msgs) != 2 { // 2 mốc × 1 người nhận có email
		t.Fatalf("email: %d", len(msgs))
	}
	m := msgs[0]
	if m.To != "can.bo@ttkn-angiang.test" || !strings.Contains(m.Subject, "Báo cáo tiến độ gieo sạ") ||
		!strings.Contains(m.Text, "http://files.test/reports/"+f.tenant.String()+"/") || !strings.Contains(m.Text, "15 phút") ||
		!strings.Contains(m.Text, "https://ops.test/reports/exports/") {
		t.Fatalf("nội dung email: %+v", m)
	}
	runs = items(mgr.adminGet("/v1/reports/schedules/" + str(sch, "id") + "/runs"))
	if runs[0]["delivery_status"] != "sent" || runs[0]["sent_count"].(float64) != 1 || !strings.Contains(str(runs[0], "delivery_note"), "1 người nhận") {
		t.Fatalf("kết quả gửi: %v", runs[0])
	}
	mgr.must(mgr.adminPost("/v1/reports/schedules/"+str(sch, "id")+"/deactivate", map[string]any{"reason": "Đổi sang lịch gửi hằng tháng"}), http.StatusOK, "ngừng lịch")
	f.clk.Advance(7 * 24 * time.Hour)
	fire()
	if jobs() != 2 {
		t.Fatal("lịch đã ngừng không chạy")
	}
}

// Thiếu cổng MailDirectory (pf/accounts chưa nối) → bỏ gửi kèm cảnh báo, không lỗi.
func TestDeliverySkippedWithoutDirectory(t *testing.T) {
	f := newFixture(t, false)
	mgr := f.as(f.org.national, extport.RoleKNNationalAdmin)
	sch := mgr.must(mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "pdf",
		"frequency": "monthly", "day_of_month": 1, "hour": 6, "recipients": []string{ids.New().String()}}), http.StatusCreated, "lập lịch").body
	f.clk.Advance(4 * 24 * time.Hour) // qua mốc 01/10 06:00
	if err := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runWorker()
	if q := f.ext.lastQuery(); q.Periods[0] != "2026-09" || !q.Scope.All {
		t.Fatalf("lịch tháng gửi số liệu tháng trước: %+v", q)
	}
	for _, e := range f.events(port.EventExportCompleted) {
		f.deliver(port.DeliveryConsumer, e, f.mod.DeliveryHandler())
	}
	if len(f.mailer.messages()) != 0 {
		t.Fatal("không có danh bạ email thì không gửi")
	}
	runs := items(mgr.adminGet("/v1/reports/schedules/" + str(sch, "id") + "/runs"))
	if len(runs) != 1 || runs[0]["delivery_status"] != "skipped" {
		t.Fatalf("bỏ gửi: %v", runs)
	}
}

// CGH FN-10 BR-03: chỉ Admin quản lý mẫu; phiên bản mới được ghim vào việc xuất; ngừng dùng → 409.
func TestTemplateVersionsAdmin(t *testing.T) {
	f := newFixture(t, false)
	adm := f.as(f.org.national, port.RoleReportsAdmin)
	def := map[string]any{"title": "Tiến độ gieo sạ (rút gọn)", "source": "extension.sowing", "grouping": "province",
		"columns": []map[string]any{{"key": "province_name"}, {"key": "sown_area_ha", "title": "Đã sạ (ha)"}}, "formats": []string{"xlsx"}}
	user := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	if r := user.adminPost("/v1/reports/templates", map[string]any{"key": "kn.sowing_progress", "definition": def}); r.status != http.StatusForbidden {
		t.Fatalf("không phải Admin: %d", r.status)
	}
	noMFA := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{port.RoleReportsAdmin}, OrgRef: f.org.national})
	if r := (caller{f: f, token: noMFA}).adminGet("/v1/reports/templates"); r.status != http.StatusForbidden || code(r) != "mfa_required" {
		t.Fatalf("Admin API cần MFA: %d", r.status)
	}
	bad := map[string]any{"title": "Mẫu sai cột", "source": "extension.sowing", "columns": []map[string]any{{"key": "shortage"}}, "formats": []string{"xlsx"}}
	if r := adm.adminPost("/v1/reports/templates", map[string]any{"key": "kn.sowing_progress", "definition": bad}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("cột ngoài nguồn: %d", r.status)
	}
	v1 := adm.must(adm.adminPost("/v1/reports/templates", map[string]any{"key": "kn.sowing_progress", "definition": def}), http.StatusCreated, "phiên bản 1").body
	if v1["version"].(float64) != 1 {
		t.Fatalf("phiên bản: %v", v1)
	}
	if r := user.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "pdf"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("phiên bản mới chỉ có xlsx: %d", r.status)
	}
	id := str(user.must(user.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}), http.StatusAccepted, "xuất").body, "id")
	f.runWorker()
	x := f.sheet(f.file("reports/" + f.tenant.String() + "/" + id + ".xlsx"))
	hr := findRow(t, x, "Tỉnh/Thành phố")
	if raw(t, x, cellName(2, hr)) != "Đã sạ (ha)" || raw(t, x, cellName(2, hr+1)) != "1234.6678" || raw(t, x, cellName(3, hr)) != "" {
		t.Fatalf("việc xuất theo phiên bản 1 (cấp tỉnh, 2 cột)")
	}
	vers := items(adm.must(adm.adminGet("/v1/reports/templates/kn.sowing_progress/versions"), http.StatusOK, "lịch sử"))
	if len(vers) != 2 || vers[1]["builtin"] != true {
		t.Fatalf("phiên bản 1 + mẫu hệ thống: %v", vers)
	}
	adm.must(adm.adminPost("/v1/reports/templates/kn.sowing_progress/retire", map[string]any{"reason": "Thay bằng mẫu Bộ ban hành"}), http.StatusCreated, "ngừng dùng")
	if r := user.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}); r.status != http.StatusConflict || code(r) != "reports.template_retired" {
		t.Fatalf("mẫu đã ngừng: %d", r.status)
	}
	list := items(user.must(user.get("/v1/reports/templates"), http.StatusOK, "mẫu của người dùng"))
	for _, it := range list {
		if it["key"] == "kn.sowing_progress" || strings.HasPrefix(str(it, "key"), "cgh.") {
			t.Fatalf("danh sách chỉ gồm mẫu đang dùng mà vai được xuất: %v", it["key"])
		}
	}
}

// Lịch sử xuất của CHÍNH người gọi: mới nhất trước, phân trang cursor, cờ tải được
// sau khi worker xong; người khác (kể cả tỉnh cha thấy từng việc) không thấy trong danh sách.
func TestMyExportsHistory(t *testing.T) {
	f := newFixture(t, false)
	off := f.as(f.org.tBinhDuc, extport.RoleKNOfficer)
	var ids3 []string
	for _, format := range []string{"xlsx", "pdf", "xlsx"} {
		r := off.must(off.post("/v1/reports/exports", map[string]any{"template_key": "kn.sowing_progress", "format": format}),
			http.StatusAccepted, "xuất "+format)
		ids3 = append(ids3, str(r.body, "id"))
	}
	p1 := off.must(off.get("/v1/reports/exports?limit=2"), http.StatusOK, "trang 1")
	it1 := items(p1)
	if len(it1) != 2 || str(it1[0], "id") != ids3[2] || str(it1[1], "id") != ids3[1] || it1[0]["downloadable"] != false ||
		str(it1[0], "status") != "queued" || str(it1[1], "format") != "pdf" || str(it1[0], "template_key") != "kn.sowing_progress" {
		t.Fatalf("trang 1 (mới nhất trước): %v", p1.body)
	}
	cur, _ := p1.body["next_cursor"].(string)
	if cur == "" {
		t.Fatalf("thiếu next_cursor: %v", p1.body)
	}
	p2 := off.must(off.get("/v1/reports/exports?limit=2&cursor="+cur), http.StatusOK, "trang 2")
	if it2 := items(p2); len(it2) != 1 || str(it2[0], "id") != ids3[0] || p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %v", p2.body)
	}

	f.runWorker()
	all := items(off.must(off.get("/v1/reports/exports"), http.StatusOK, "sau khi dựng"))
	if len(all) != 3 {
		t.Fatalf("đủ 3 việc: %v", all)
	}
	for _, it := range all {
		if it["downloadable"] != true || str(it, "status") != "completed" || str(it, "completed_at") == "" {
			t.Fatalf("đã dựng xong phải tải được: %v", it)
		}
	}

	// Tỉnh cha đọc được từng việc, nhưng danh sách chỉ gồm việc của chính mình.
	ag := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	ag.must(ag.get("/v1/reports/exports/"+ids3[0]), http.StatusOK, "tỉnh cha đọc từng việc")
	if r := ag.must(ag.get("/v1/reports/exports"), http.StatusOK, "danh sách tỉnh"); len(items(r)) != 0 {
		t.Fatalf("không thấy việc của người khác: %v", r.body)
	}
	if r := off.get("/v1/reports/exports?cursor=khong-hop-le"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("cursor sai: %d", r.status)
	}
	if r := off.get("/v1/reports/exports?limit=0"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("limit sai: %d", r.status)
	}
	if r := f.as(f.org.tBinhDuc, "mrv_viewer").get("/v1/reports/exports"); r.status != http.StatusForbidden {
		t.Fatalf("thiếu vai: %d", r.status)
	}
}
