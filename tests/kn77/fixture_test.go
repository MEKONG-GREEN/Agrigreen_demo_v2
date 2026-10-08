// Package kn77_test kiểm thử tích hợp vòng đời báo cáo KN v7.7 (gói P4) trên module THẬT: pf/extension
// (extension/reportcycle) + pf/reports (engine mẫu P3, báo cáo Chi cục) trên PostgreSQL, mỗi module bằng vai DB riêng
// (R-05). Ngữ cảnh phiên KN (P1) giả lập bằng middleware gắn authz.KNContext theo header X-AG-Context; cây tổ chức,
// khu vực (P2) và gán vai trò (P1) là bản giả.
package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const dept = "trong_trot"

// t0: 08:00 thứ Hai 07/09/2026 giờ Việt Nam.
var t0 = time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)

// ---- bản giả P1/P2 ----

type orgs struct{ m map[uuid.UUID]partyport.Org }

func (o orgs) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	org, ok := o.m[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return org, nil
}

type units struct{ names map[string]string }

func (u units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	n, ok := u.names[code]
	if !ok {
		return partyport.AdminUnit{Code: code, Name: "ĐV " + code, Level: partyport.LevelCommune}, nil
	}
	return partyport.AdminUnit{Code: code, Name: n, Level: partyport.LevelCommune}, nil
}
func (u units) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}
func (u units) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

type regions struct {
	byProvince map[string][]partyport.Region
	communes   map[string]map[string]partyport.CommuneRegion
}

func (r regions) ProvinceRegions(_ context.Context, p string, _ bool) ([]partyport.Region, error) {
	return r.byProvince[p], nil
}

func (r regions) CommuneRegions(_ context.Context, p string) (map[string]partyport.CommuneRegion, error) {
	out := map[string]partyport.CommuneRegion{}
	for k, v := range r.communes[p] {
		out[k] = v
	}
	return out, nil
}

type assignments struct {
	mu sync.Mutex
	l  []accport.Assignment
}

func (a *assignments) ActiveAssignments(_ context.Context, role, d string) ([]accport.Assignment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []accport.Assignment
	for _, x := range a.l {
		if (role == "" || x.Role == role) && (d == "" || containsStr(x.Departments, d)) {
			out = append(out, x)
		}
	}
	return out, nil
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

type mailer struct {
	mu   sync.Mutex
	sent []repport.Message
}

func (m *mailer) Send(_ context.Context, msg repport.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailer) all() []repport.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]repport.Message(nil), m.sent...)
}

// ---- fixture ----

type fixture struct {
	t       *testing.T
	env     *pgtest.Env
	iss     *authztest.FakeIssuer
	clk     *clock.Fixed
	tenant  uuid.UUID
	ext     *extension.Module
	rep     *reports.Module
	repPool tenant.Beginner
	app     *httptest.Server
	admin   *httptest.Server
	mail    *mailer
	asg     *assignments
	// repParams: tham số của pf/reports trong kiểm thử (A4: reports.dispatch_attachment_limit_mb,
	// reports.dispatch_link_ttl_hours); rỗng → mặc định.
	repParams *repParams

	mu   sync.Mutex
	ctxs map[uuid.UUID]*authz.KNContext

	national, p89, p92 uuid.UUID
	communes89         []string
}

type fixtureOpts struct {
	communes int // số xã của tỉnh 89 (mặc định 3)
}

func newFixture(t *testing.T, o fixtureOpts) *fixture {
	env := pgtest.Get(t)
	f := &fixture{t: t, env: env, iss: authztest.New(t), clk: clock.NewFixed(t0), tenant: ids.New(), mail: &mailer{}, asg: &assignments{},
		ctxs: map[uuid.UUID]*authz.KNContext{}, national: ids.New(), p89: ids.New(), p92: ids.New()}
	n := o.communes
	if n == 0 {
		n = 3
	}
	og := orgs{m: map[uuid.UUID]partyport.Org{
		f.national: {Ref: f.national, Kind: partyport.OrgTTKNNational, Active: true},
		f.p89:      {Ref: f.p89, Kind: partyport.OrgTTKNProvince, AdminUnitCode: "89", ParentRef: f.national, Active: true},
		f.p92:      {Ref: f.p92, Kind: partyport.OrgTTKNProvince, AdminUnitCode: "92", ParentRef: f.national, Active: true},
	}}
	rg := regions{byProvince: map[string][]partyport.Region{
		"89": {{Code: "KV1", Name: "Khu vực Long Xuyên", ProvinceCode: "89", SortOrder: 1, Active: true}, {Code: "KV2", Name: "Khu vực Châu Đốc", ProvinceCode: "89", SortOrder: 2, Active: true}},
		"92": {{Code: "KVA", Name: "Khu vực A", ProvinceCode: "92", Active: true}},
	}, communes: map[string]map[string]partyport.CommuneRegion{"89": {}, "92": {"31001": {CommuneCode: "31001", RegionCode: "KVA", RegionName: "Khu vực A"}}}}
	names := map[string]string{"31001": "Xã 92-1"}
	for i := 1; i <= n; i++ {
		c := fmt.Sprintf("3%04d", i)
		reg := "KV1"
		if i > (n+1)/2 {
			reg = "KV2"
		}
		rg.communes["89"][c] = partyport.CommuneRegion{CommuneCode: c, RegionCode: reg, SortOrder: i}
		names[c] = "Xã " + c
		f.communes89 = append(f.communes89, c)
	}
	sort.Strings(f.communes89)
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("kn77-test-presign-secret-32-bytes!!"), BaseURL: "http://files.test"})
	if err != nil {
		t.Fatal(err)
	}
	extPool := env.Pool(t, migrations.PF, extension.DBRole)
	repPool := env.Pool(t, migrations.PF, reports.DBRole)
	f.repPool = repPool
	f.ext = extension.New(extPool, f.clk, extension.Deps{Orgs: og, Units: units{names: names}})
	f.repParams = &repParams{v: map[string]decimal.Decimal{}}
	f.rep = reports.New(repPool, f.clk, reports.Deps{Orgs: og, Units: units{names: names}, Store: st, Mailer: f.mail, Getenv: func(string) string { return "" },
		Params: f.repParams})
	f.ext.UseReportCycle(extension.ReportCycleDeps{Forms: f.rep.Forms(), Regions: rg, Assignments: f.asg})
	f.ext.UseDispatchHistory(f.rep.DispatchHistory())
	// W6.2: ghi song song chốt/mở lại kỳ + ảnh chụp đóng băng sang cap/forms (cấu hình KN thật của gốc lắp ráp).
	fm, err := forms.New(nil, f.clk, forms.Deps{Profiles: []formsport.Profile{platformasm.KNProfile}})
	if err != nil {
		t.Fatal(err)
	}
	f.ext.UseFormsFreezer(fm.Freezer())
	f.rep.UseConsolidations(f.ext.Consolidations())

	ops := env.Pool(t, migrations.PF, "platform_api")
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 8<<20)
	appEng.Use(logErrors(t))
	g := appEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops))
	f.ext.RegisterApp(g)
	f.rep.RegisterApp(g)
	adminEng := httpx.NewEngine(log, 8<<20)
	adminEng.Use(logErrors(t))
	f.rep.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.rep.RegisterPublic(appEng.Group("/v1/public")) // A4: liên kết tải Chi cục (không đăng nhập)
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	f.rep.UseLinkDownloadBase(f.app.URL)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	return f
}

// repParams là tham số pf/masterdata giả cho pf/reports (khóa chưa đặt → ErrParamNotFound → mặc định).
type repParams struct {
	mu sync.Mutex
	v  map[string]decimal.Decimal
}

func (p *repParams) set(key, v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.v[key] = decimal.RequireFromString(v)
}

func (p *repParams) At(_ context.Context, key string, _ time.Time) (params.ParamValue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.v[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: v, Version: 1}, nil
}

func logErrors(t *testing.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("%s %s: %v", c.Request.Method, c.FullPath(), e.Err)
		}
	}
}

// knMiddleware giả lập middleware X-AG-Context của P1: context thuộc đúng người trong token, không thì 403.
func (f *fixture) knMiddleware() gin.HandlerFunc {
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
		if cp.Support != nil {
			s := *cp.Support
			s.Reason = c.GetHeader(authz.ReasonHeader)
			cp.Support = &s
		}
		p.KN = &cp
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

// ---- người gọi ----

type caller struct {
	f      *fixture
	token  string
	actor  uuid.UUID
	ctxID  uuid.UUID
	reason string
}

// kn tạo người dùng có ngữ cảnh phiên KN (vai, phòng ban, Nút, xã phụ trách).
func (f *fixture) kn(tid uuid.UUID, role, department string, org uuid.UUID, communes []string) caller {
	actor := ids.New()
	return f.knFor(tid, actor, role, department, org, communes, nil)
}

func (f *fixture) knFor(tid, actor uuid.UUID, role, department string, org uuid.UUID, communes []string, sup *authz.KNSupport) caller {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{accport.RoleKNUser}, MFA: true, TTL: 2 * time.Hour})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: department, OrgRef: org, Communes: communes, Support: sup}
	switch role {
	case authz.KNXa:
		kc.Scope = authz.Scope{Units: communes}
	case authz.KNTW, authz.KNAdmin:
		kc.Scope = authz.Scope{All: true}
	default:
		kc.Scope = authz.Scope{Units: append([]string{"89"}, f.communes89...)}
	}
	f.mu.Lock()
	f.ctxs[kc.ID] = kc
	f.mu.Unlock()
	return caller{f: f, token: tok, actor: actor, ctxID: kc.ID}
}

// admin tạo Admin nền tảng (Admin API, MFA).
func (f *fixture) adminCaller() caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, Roles: []string{authz.RolePlatformAdmin, repport.RoleReportsAdmin}, MFA: true, TTL: 2 * time.Hour,
		OrgRef: f.national})
	return caller{f: f, token: tok, actor: actor}
}

type resp struct {
	status int
	body   map[string]any
	raw    []byte
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
	if c.ctxID != uuid.Nil {
		req.Header.Set(authz.ContextHeader, c.ctxID.String())
	}
	if c.reason != "" {
		req.Header.Set(authz.ReasonHeader, c.reason)
	}
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
	return resp{status: r.StatusCode, body: m, raw: raw}
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
		c.f.t.Fatalf("%s: muốn %d, nhận %d %s", what, want, r.status, string(r.raw))
	}
	return r
}

func (c caller) fail(r resp, status int, code, what string) {
	c.f.t.Helper()
	if r.status != status || str(r.body, "code") != code {
		c.f.t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, string(r.raw))
	}
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func obj(m map[string]any, k string) map[string]any {
	o, _ := m[k].(map[string]any)
	return o
}
func arr(m map[string]any, k string) []map[string]any {
	raw, _ := m[k].([]any)
	out := []map[string]any{}
	for _, it := range raw {
		if o, ok := it.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}
func num(m map[string]any, k string) float64 { n, _ := m[k].(float64); return n }

// publishForms seed 14 mẫu, đề nghị (Admin A) rồi phát hành (Admin B, R-56) các mẫu codes; trả template_id theo mã.
func (f *fixture) publishForms(codes ...string) map[string]string {
	a, b := f.adminCaller(), f.adminCaller()
	a.must(a.adminPost("/v1/reports/forms/seed", map[string]any{"department_code": dept}), http.StatusOK, "seed")
	r := a.must(a.adminGet("/v1/reports/forms?department="+dept), http.StatusOK, "danh sách mẫu")
	out := map[string]string{}
	for _, it := range arr(r.body, "items") {
		tpl := obj(it, "template")
		out[str(tpl, "code")] = str(tpl, "id")
	}
	for _, c := range codes {
		base := "/v1/reports/forms/" + out[c] + "/versions/1"
		a.must(a.adminPost(base+"/propose", map[string]any{"effective_from": "2026-01-05", "effective_label": "Tuần 2/2026"}), http.StatusOK, "đề nghị "+c)
		b.must(b.adminPost(base+"/publish", map[string]any{}), http.StatusOK, "phát hành "+c)
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

// count đếm dòng của một bảng (tài khoản quản trị).
func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// setClock đặt đồng hồ giả tới t.
func (f *fixture) setClock(t time.Time) { f.clk.Advance(t.Sub(f.clk.Now())) }
