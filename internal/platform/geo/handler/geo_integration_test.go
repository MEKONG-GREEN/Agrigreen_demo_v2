package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
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

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	mod    *geo.Module
	pool   *pgxpool.Pool
	scope  *fakeScope
	tenant uuid.UUID
	// Tổ chức giả: cán bộ xã 31117, cán bộ xã 31120, hai HTX ở xã 31117.
	orgA, orgB, coopA, coopB uuid.UUID
}

func newFixture(t *testing.T, opts ...func(*geo.Deps)) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, geo.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	sc := newFakeScope()
	tol := decimal.NewFromInt(1)
	deps := geo.Deps{Scope: sc, Params: fakeParams{tolerance: &tol}, AdminUnits: newFakeUnits()}
	for _, o := range opts {
		o(&deps)
	}
	mod := geo.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, deps)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 4<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 4<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	f := &fixture{t: t, iss: iss, app: appSrv, admin: adminSrv, mod: mod, pool: pool, scope: sc, tenant: ids.New(),
		orgA: ids.New(), orgB: ids.New(), coopA: ids.New(), coopB: ids.New()}
	sc.set(f.orgA, authz.Scope{Units: []string{"31117"}})
	sc.set(f.orgB, authz.Scope{Units: []string{"31120"}})
	sc.set(f.coopA, authz.Scope{Units: []string{"31117"}})
	sc.set(f.coopB, authz.Scope{Units: []string{"31117"}})
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	roles []string
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, roles: roles, token: tok, actor: actor}
}

// national: quản trị toàn quốc (phạm vi All).
func (f *fixture) national() caller {
	return f.as(f.tenant, uuid.Nil, rolePlotEditor, port.RoleGeoAdmin, authz.RolePlatformAdmin)
}

func (f *fixture) officerA() caller { return f.as(f.tenant, f.orgA, rolePlotEditor) }
func (f *fixture) officerB() caller { return f.as(f.tenant, f.orgB, rolePlotEditor) }
func (f *fixture) managerA() caller {
	return f.as(f.tenant, f.coopA, rolePlotEditor, port.RoleCoopManager)
}

type resp struct {
	status int
	hdr    http.Header
	raw    []byte
	body   map[string]any
}

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
	return resp{status: r.StatusCode, hdr: r.Header, raw: raw, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) appDo(method, path string, body any) resp {
	c.f.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return c.do(c.f.app.URL, method, path, idem, "application/json", b)
}

func (c caller) adminPost(path, ctype string, body []byte) resp {
	c.f.t.Helper()
	return c.do(c.f.admin.URL, http.MethodPost, path, key(), ctype, body)
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

// ---------------------------------------------------------------------------
