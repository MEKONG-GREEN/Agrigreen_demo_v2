package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
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

// fakeSource là nguồn chỉ số giả: giá trị theo (key, đơn vị, chiều), đặt được từ kiểm thử.
type fakeSource struct {
	mu     sync.Mutex
	values map[string]decimal.Decimal
	scopes []port.Scope
	calls  int
}

func fkey(k port.Key, sc port.Scope) string {
	u := ""
	if len(sc.Units) == 1 {
		u = sc.Units[0]
	}
	return string(k) + "|" + u + "|" + sc.Dimension
}

func (f *fakeSource) set(k port.Key, unit, dim, v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := port.Scope{Dimension: dim}
	if unit != "" {
		sc.Units = []string{unit}
	}
	f.values[fkey(k, sc)] = decimal.RequireFromString(v)
}

func (f *fakeSource) AsOf(ctx context.Context, k port.Key, _ clock.Date, sc port.Scope) (port.Value, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	v, ok := f.values[fkey(k, sc)]
	if !ok {
		return port.Value{}, port.ErrNoData
	}
	def, _ := port.DefinitionFrom(ctx)
	return port.Value{Value: v, DefinitionVersion: def.Version}, nil
}

func (f *fakeSource) Scopes(_ context.Context, _ port.Key, _ clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	if len(f.scopes) == 0 {
		return []port.Scope{{TenantID: tid}}, nil
	}
	out := []port.Scope{}
	for _, s := range f.scopes {
		s.TenantID = tid
		out = append(out, s)
	}
	return out, nil
}

// fakeScope: phạm vi theo actor (mặc định rỗng).
type fakeScope struct {
	mu sync.Mutex
	by map[uuid.UUID]authz.Scope
}

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.by[p.ActorRef], nil
}

type fakeGeom struct{}

func (fakeGeom) Geometries(_ context.Context, refs []port.GeometryRef) (map[port.GeometryRef]json.RawMessage, error) {
	out := map[port.GeometryRef]json.RawMessage{}
	for _, r := range refs {
		out[r] = json.RawMessage(`{"type":"Point","coordinates":[105.7,10.0]}`)
	}
	return out, nil
}

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	mod    *metrics.Module
	pool   *pgxpool.Pool
	owner  *pgxpool.Pool
	clk    *clock.Fixed
	src    *fakeSource
	scopes *fakeScope
	tenant uuid.UUID
}

func newFixture(t *testing.T, clk *clock.Fixed) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, metrics.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	src := &fakeSource{values: map[string]decimal.Decimal{}}
	sc := &fakeScope{by: map[uuid.UUID]authz.Scope{}}
	sources := map[string]port.Source{"pf/extension": src, "pf/mechanization": src, "pf/cropcycle": src, "pf/geo": src}
	mod := metrics.New(pool, clk, metrics.Deps{Sources: sources, Scope: sc, Geometry: fakeGeom{}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &fixture{t: t, iss: iss, srv: srv, mod: mod, pool: pool, owner: env.AdminPool(t, migrations.PF), clk: clk, src: src, scopes: sc, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	token string
}

func (f *fixture) as(tid uuid.UUID, sc authz.Scope, roles ...string) caller {
	actor := ids.New()
	f.scopes.mu.Lock()
	f.scopes.by[actor] = sc
	f.scopes.mu.Unlock()
	return caller{f: f, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles})}
}

type resp struct {
	status int
	body   map[string]any
}

func (c caller) get(path string) resp {
	c.f.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.f.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
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

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (r resp) items() []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(map[string]any))
	}
	return out
}

func expect(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		t.Fatalf("mong %d %s, nhận %d %v", status, code, r.status, r.body)
	}
}
