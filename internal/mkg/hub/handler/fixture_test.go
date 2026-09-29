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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
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
	t          *testing.T
	env        *pgtest.Env
	iss        *authztest.FakeIssuer
	app, admin *httptest.Server
	pool       *pgxpool.Pool
	clk        *clock.Fixed
	tenant     uuid.UUID // tài khoản MKG của kiểm thử
}

// newFixture dựng API nội bộ MKG giống cmd/api-internal/assembly: pool vai
// hub_app ở cụm mkg, idempotency trên shared.idempotency_key của cụm mkg.
func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, hub.DBRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	mod := hub.New(pool, clk)
	iss := authztest.New(t)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(pool)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pool)))
	a, ad := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(a.Close)
	t.Cleanup(ad.Close)
	return &fixture{t: t, env: env, iss: iss, app: a, admin: ad, pool: pool, clk: clk, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	actor uuid.UUID
	token string
}

func (f *fixture) as(tid uuid.UUID, mfa bool, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: mfa})}
}

func (f *fixture) operator() caller { return f.as(f.tenant, true, port.RoleHubOperator) }
func (f *fixture) approver() caller { return f.as(f.tenant, true, port.RoleHubApprover) }

type resp struct {
	status int
	body   map[string]any
	raw    []byte
}

func (c caller) do(base, method, path, key string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // số JSON giữ dạng json.Number: không float trong mkg/hub (R-10)
	_ = dec.Decode(&m)
	return resp{status: r.StatusCode, body: m, raw: raw}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) app(method, path string, body any) resp {
	k := ""
	if method == http.MethodPost {
		k = key()
	}
	return c.do(c.f.app.URL, method, path, k, body)
}

func (c caller) adm(method, path string, body any) resp {
	k := ""
	if method == http.MethodPost {
		k = key()
	}
	return c.do(c.f.admin.URL, method, path, k, body)
}

func mustStatus(t *testing.T, r resp, want int, what string) {
	t.Helper()
	if r.status != want {
		t.Fatalf("%s: HTTP %d (muốn %d): %s", what, r.status, want, r.raw)
	}
}

func code(r resp) string { s, _ := r.body["code"].(string); return s }

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

func obj(m map[string]any, k string) map[string]any { o, _ := m[k].(map[string]any); return o }

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

// facilityBody là một đề xuất dữ liệu chủ hợp lệ.
func (f *fixture) facilityBody(code string, capacityKg string) map[string]any {
	return map[string]any{
		"code": code, "name": "Hub Cờ Đỏ " + code, "kind": "hub",
		"location":      map[string]any{"lat": json.Number("10.0452"), "lng": json.Number("105.7469")},
		"boundary":      json.RawMessage(`{"type":"Polygon","coordinates":[[[105.74,10.04],[105.75,10.04],[105.75,10.05],[105.74,10.05],[105.74,10.04]]]}`),
		"province_code": "92", "capacity_kg": capacityKg, "effective_from": f.ts(-time.Hour),
	}
}

// approvedFacility tạo và phê duyệt (hai người khác nhau) một cơ sở; trả id.
func (f *fixture) approvedFacility(code, capacityKg string) string {
	f.t.Helper()
	p := f.operator().adm(http.MethodPost, "/v1/hub/facilities", f.facilityBody(code, capacityKg))
	mustStatus(f.t, p, http.StatusCreated, "đề xuất cơ sở")
	id := str(p.body, "facility_id")
	d := f.approver().adm(http.MethodPost, "/v1/hub/facilities/"+id+"/versions/1/decision", map[string]any{"decision": "approved"})
	mustStatus(f.t, d, http.StatusCreated, "phê duyệt cơ sở")
	return id
}

// receive nhận một lô một nguồn; trả lot id và movement id.
func (f *fixture) receive(op caller, facility, massKg, moisture string, ago time.Duration) (string, string) {
	f.t.Helper()
	body := map[string]any{
		"product": "straw_bale", "mass_kg": massKg, "event_time": f.ts(-ago),
		"sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "mass_kg": massKg}},
	}
	if moisture != "" {
		body["moisture_pct"] = moisture
	}
	r := op.app(http.MethodPost, "/v1/hub/facilities/"+facility+"/receipts", body)
	mustStatus(f.t, r, http.StatusCreated, "nhận lô")
	return str(r.body, "lot", "id"), str(r.body, "movement", "id")
}

// inTenant chạy fn trong giao dịch tenant bằng vai hub_app (RLS vẫn áp).
func (f *fixture) inTenant(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}
