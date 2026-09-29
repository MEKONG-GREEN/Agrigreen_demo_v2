package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t       *testing.T
	env     *pgtest.Env
	iss     *authztest.FakeIssuer
	srv     *httptest.Server
	pool    *pgxpool.Pool
	clk     *clock.Fixed
	dir     *adapter.FakeDirectory
	weighed *adapter.FakeWeighed
	svc     *app.Service
	tenant  uuid.UUID
	htx     uuid.UUID
	plots   []uuid.UUID
}

// newFixture dựng API nội bộ MKG như cmd/api-internal/assembly: pool vai
// fieldops_app ở cụm mkg, nguồn thửa/HTX giả (PlatformDirectory), khối lượng cân giả.
func newFixture(t *testing.T, cfgKg *decimal.Decimal) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, port.DBRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	f := &fixture{t: t, env: env, pool: pool, clk: clk, dir: adapter.NewFakeDirectory(), weighed: adapter.NewFakeWeighed(),
		tenant: ids.New(), htx: ids.New()}
	f.dir.AddHTX(f.htx, "31117")
	for i := 0; i < 2; i++ {
		p := ids.New()
		f.dir.AddPlot(p, f.htx, "31117")
		f.plots = append(f.plots, p)
	}
	f.svc = &app.Service{DB: pool, Clock: clk, Directory: f.dir, Weighed: f.weighed, ConfiguredKgPerRoll: cfgKg,
		CursorKey: []byte("khoa-thu-nghiem")}
	f.iss = authztest.New(t)
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	handler.Handler{Svc: f.svc}.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(pool)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

type caller struct {
	f      *fixture
	tid    uuid.UUID
	actor  uuid.UUID
	token  string
	device string
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles})}
}

func (f *fixture) supervisor() caller { return f.as(f.tenant, port.RoleFieldSupervisor) }
func (f *fixture) collector() caller  { return f.as(f.tenant, port.RoleFieldCollector) }

type resp struct {
	status int
	body   map[string]any
	raw    []byte
}

func (c caller) do(method, path, key string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.f.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if c.device != "" {
		req.Header.Set(handler.DeviceHeader, c.device)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: r.StatusCode, body: m, raw: raw}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp { return c.do(http.MethodPost, path, key(), body) }
func (c caller) get(path string) resp            { return c.do(http.MethodGet, path, "", nil) }

func mustStatus(t *testing.T, r resp, want int, what string) {
	t.Helper()
	if r.status != want {
		t.Fatalf("%s: HTTP %d (muốn %d): %s", what, r.status, want, r.raw)
	}
}

func mustCode(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if got, _ := r.body["code"].(string); r.status != status || got != code {
		t.Fatalf("%s: HTTP %d %q (muốn %d %q): %s", what, r.status, got, status, code, r.raw)
	}
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
	switch v := cur.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

func arr(m map[string]any, k string) []map[string]any {
	xs, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		o, _ := x.(map[string]any)
		out = append(out, o)
	}
	return out
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fixture) ts(d time.Duration) string { return f.clk.Now().Add(d).Format(time.RFC3339) }

func (f *fixture) planBody() map[string]any {
	d := f.clk.Now()
	return map[string]any{
		"htx_ref": f.htx.String(), "window_from": d.Format("2006-01-02"), "window_to": d.AddDate(0, 0, 5).Format("2006-01-02"),
		"plot_refs": []string{f.plots[0].String(), f.plots[1].String()},
	}
}

// approvedOrder lập (giám sát A) và duyệt (giám sát B) một lệnh gom; trả order_id.
func (f *fixture) approvedOrder() string {
	f.t.Helper()
	r := f.supervisor().post("/v1/fieldops/orders", f.planBody())
	mustStatus(f.t, r, http.StatusCreated, "lập lệnh gom")
	id := str(r.body, "order", "order_id")
	d := f.supervisor().post("/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"})
	mustStatus(f.t, d, http.StatusCreated, "duyệt lệnh gom")
	return id
}

func (f *fixture) count(c caller, order string, plot uuid.UUID, stage string, rolls int, extra map[string]any) resp {
	body := map[string]any{"plot_ref": plot.String(), "stage": stage, "rolls": rolls, "event_time": f.ts(-time.Minute)}
	for k, v := range extra {
		body[k] = v
	}
	return c.post("/v1/fieldops/orders/"+order+"/roll-counts", body)
}

func (f *fixture) inTenant(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}
