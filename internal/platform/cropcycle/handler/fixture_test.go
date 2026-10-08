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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
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

// startTime: 29/09/2026 10:00 giờ Việt Nam.
var startTime = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

// fixture của van thu gom (W3.3: pf/cropcycle chỉ còn collection_*; vụ ở pf/cooperative — kiểm thử dùng
// cooperativeport giả cho thửa và mã vụ).
type fixture struct {
	t       *testing.T
	iss     *authztest.FakeIssuer
	app     *httptest.Server
	mod     *cropcycle.Module
	pool    *pgxpool.Pool
	db      tenant.Beginner // pool app_platform, giao dịch dưới r_cropcycle
	clk     *clock.Fixed
	plots   *fakePlots
	seasons *fakeSeasons
	tenant  uuid.UUID
	mkg     uuid.UUID         // tenant MKG riêng (ADR-0020) — tenant phong bì van, tenant tài khoản mkg_sync
	share   *porttest.Sharing // cấp quyền chia sẻ giả tenant → mkg (pf/party)
	coopA   uuid.UUID         // xã 31117
	coopB   uuid.UUID         // xã 31120
	knA     uuid.UUID         // tổ KN xã 31117
	p1, p2  uuid.UUID         // thửa HTX A
	p3, p4  uuid.UUID         // p3: HTX A; p4: HTX B
	p5      uuid.UUID         // thửa thứ hai HTX A
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f := &fixture{t: t, iss: iss, pool: pool, clk: clock.NewFixed(startTime), tenant: ids.New(), mkg: ids.New(), share: porttest.New(),
		coopA: ids.New(), coopB: ids.New(), knA: ids.New(), p1: ids.New(), p2: ids.New(), p3: ids.New(), p4: ids.New(), p5: ids.New(),
		seasons: &fakeSeasons{codes: map[uuid.UUID]coopport.SeasonKey{}}}
	sc := &fakeScope{scopes: map[uuid.UUID]authz.Scope{f.knA: {Units: []string{"31117"}}}}
	f.plots = &fakePlots{plots: map[uuid.UUID]coopport.Plot{}, dist: map[uuid.UUID]decimal.Decimal{}}
	area := decimal.RequireFromString("1.5000")
	for i, p := range []struct {
		ref, coop uuid.UUID
		commune   string
	}{{f.p1, f.coopA, "31117"}, {f.p2, f.coopA, "31117"}, {f.p3, f.coopA, "31117"}, {f.p4, f.coopB, "31120"}, {f.p5, f.coopA, "31117"}} {
		f.plots.put(coopport.Plot{Ref: p.ref, Code: "TH000000" + string(rune('1'+i)), CommuneCode: p.commune, CooperativeRef: p.coop,
			HouseholdRef: ids.New(), Status: coopport.PlotActive, Version: 1, AreaHa: area}, 0)
	}
	f.db = shareddb.ForModule(pool, cropcycle.DBModule)
	f.mod = cropcycle.New(pool, f.clk, cropcycle.Deps{Scope: sc, Plots: f.plots})
	f.share.Grant(f.tenant, f.mkg, partyport.ShareKinds...) // ADR-0020: tenant chương trình → tenant MKG, mọi loại
	f.mod.UseSharing(f.share, f.share)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 4<<20)
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	f.app = httptest.NewServer(appEng)
	t.Cleanup(f.app.Close)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, token: tok, actor: actor}
}

func (f *fixture) platformAdmin() caller { return f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin) }
func (f *fixture) managerA() caller      { return f.as(f.tenant, f.coopA, port.RoleCoopManager) }
func (f *fixture) managerB() caller      { return f.as(f.tenant, f.coopB, port.RoleCoopManager) }
func (f *fixture) officerA() caller      { return f.as(f.tenant, f.knA, partyport.RoleKNOfficer) }

func (f *fixture) ctxFor(c caller, roles ...string) context.Context {
	ctx := tenant.WithID(context.Background(), c.tid)
	return authz.WithPrincipal(ctx, authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: roles})
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, raw: raw, body: m}
}

func (c caller) post(path string, body any) resp {
	c.f.t.Helper()
	return c.postKey(path, key(), body)
}

func (c caller) postKey(path, idem string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.app.URL, http.MethodPost, path, idem, "application/json", b)
}

func (c caller) get(path string) resp {
	c.f.t.Helper()
	return c.do(c.f.app.URL, http.MethodGet, path, "", "application/json", nil)
}

func get(m any, path ...string) any {
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m any, path ...string) string {
	s, _ := get(m, path...).(string)
	return s
}

func num(m any, path ...string) float64 {
	n, _ := get(m, path...).(float64)
	return n
}

func list(m any, path ...string) []any {
	l, _ := get(m, path...).([]any)
	return l
}

func code(r resp) string { return str(r.body, "code") }

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: HTTP %d, muốn %d: %s", what, r.status, status, r.raw)
	}
}

func expectCode(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || code(r) != c {
		t.Fatalf("%s: HTTP %d %s, muốn %d %s: %s", what, r.status, code(r), status, c, r.raw)
	}
}
