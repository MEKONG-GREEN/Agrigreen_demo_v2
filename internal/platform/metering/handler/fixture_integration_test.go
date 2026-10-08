package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fakeParams là pf/masterdata giả: tham số đã duyệt theo (tenant, khóa) — phiên bản tăng dần.
type fakeParams struct {
	mu   sync.Mutex
	vals map[string]params.ParamValue
}

func newFakeParams() *fakeParams { return &fakeParams{vals: map[string]params.ParamValue{}} }

func (f *fakeParams) set(tid uuid.UUID, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.vals[tid.String()+"|"+key]
	f.vals[tid.String()+"|"+key] = params.ParamValue{Key: key, Value: decimal.RequireFromString(value), Version: prev.Version + 1}
}

func (f *fakeParams) At(ctx context.Context, key string, _ time.Time) (params.ParamValue, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return params.ParamValue{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[tid.String()+"|"+key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return v, nil
}

// fakePlans là pf/apiclient giả: gói hạn mức theo khách hàng.
type fakePlans struct {
	mu    sync.Mutex
	plans map[uuid.UUID]apiclientport.Plan
}

func (f *fakePlans) PlanFor(_ context.Context, _, client uuid.UUID, _ time.Time) (apiclientport.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.plans[client]; ok {
		return p, nil
	}
	return apiclientport.Plan{ClientID: client, DailyUnits: apiclientport.DefaultDailyUnits, MonthlyUnits: apiclientport.DefaultMonthlyUnits}, nil
}

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	admin  *httptest.Server
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	params *fakeParams
	plans  *fakePlans
	mod    *metering.Module
	op     uuid.UUID // tenant đơn vị vận hành
}

func newFixture(t *testing.T, mode meter.Mode) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, port.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	clk := clock.NewFixed(time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC))
	f := &fixture{t: t, env: env, iss: iss, pool: pool, clk: clk, params: newFakeParams(), plans: &fakePlans{plans: map[uuid.UUID]apiclientport.Plan{}},
		op: ids.New()}
	f.mod = metering.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clk, metering.Deps{Plans: f.plans, Params: f.params, Mode: mode, Metrics: app.NewMetrics(nil)})
	log := telemetry.NewLogger("test", io.Discard)
	eng := httpx.NewEngine(log, 1<<20)
	f.mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.admin = httptest.NewServer(eng)
	t.Cleanup(f.admin.Close)
	return f
}

// call là một lời gọi Public API giả (như cmd/api-public gửi).
func call(client, clientTenant, dataTenant uuid.UUID, at time.Time) meter.Call {
	return meter.Call{RequestID: ids.New(), ReceivedAt: at, ClientID: client, ClientTenantID: clientTenant, KeyPrefix: "agk_ABCDEFGHIJKL",
		AuthMethod: "api_key", Kind: meter.CallRead, Product: "collection_volume", ContractVersion: 1, Scope: "dp:collection_volume:read",
		RouteTemplate: "GET /v1/data/collection-volumes", Status: 200, DataTenantID: dataTenant, Rows: 12, Bytes: 2048, Latency: 35 * time.Millisecond}
}

type caller struct {
	f     *fixture
	actor uuid.UUID
	token string
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})}
}

type resp struct {
	status int
	hdr    http.Header
	raw    []byte
	body   map[string]any
}

func (c caller) do(method, path string, body any, idem string) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.f.admin.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		if idem == "" {
			idem = "k-" + ids.New().String()
		}
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // không float (agnofloat phủ cả kiểm thử của metering)
	_ = dec.Decode(&m)
	return resp{status: r.StatusCode, hdr: r.Header, raw: raw, body: m}
}

func (r resp) str(k string) string {
	s, _ := r.body[k].(string)
	return s
}

// num trả số JSON dạng chuỗi ("" khi không phải số).
func num(v any) string {
	n, _ := v.(json.Number)
	return n.String()
}

func want(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || (code != "" && r.str("code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
}

func (f *fixture) in(tid uuid.UUID, fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) count(tid uuid.UUID, q string, args ...any) int {
	f.t.Helper()
	var n int
	f.in(tid, func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), q, args...).Scan(&n) })
	return n
}
