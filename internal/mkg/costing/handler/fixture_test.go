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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Bản giả của cổng module khác (logistics, orders song song ở G4 — chỉ dùng port đóng băng).

type fakeContracts struct {
	mu sync.Mutex
	m  map[uuid.UUID]ordport.ContractTerms
}

func (f *fakeContracts) ContractFor(_ context.Context, _, htx uuid.UUID, _ time.Time) (ordport.ContractTerms, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.m[htx]; ok {
		return c, nil
	}
	return ordport.ContractTerms{}, ordport.ErrNoContract
}

func (f *fakeContracts) set(htx uuid.UUID, basis ordport.PricingBasis, price string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, _ := moneyPrice(price)
	f.m[htx] = ordport.ContractTerms{ContractID: ids.New(), Code: "HDR00001", HTXRef: htx, Basis: basis, UnitPrice: p, Version: 1}
}

type fakeOrigins struct {
	mu sync.Mutex
	m  map[uuid.UUID]hubport.LotInfo
}

func (f *fakeOrigins) LotOrigin(_ context.Context, _, lot uuid.UUID) (hubport.LotInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.m[lot]; ok {
		return v, nil
	}
	return hubport.LotInfo{}, hubport.ErrLotNotFound
}

type fakeTrips struct {
	mu        sync.Mutex
	forLot    map[uuid.UUID][]logport.TripView
	completed []logport.TripView
}

func (f *fakeTrips) Trip(context.Context, uuid.UUID, uuid.UUID) (logport.TripView, error) {
	return logport.TripView{}, logport.ErrTripNotFound
}

func (f *fakeTrips) TripsForLot(_ context.Context, _, lot uuid.UUID) ([]logport.TripView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forLot[lot], nil
}

func (f *fakeTrips) Completed(_ context.Context, _ uuid.UUID, from, to, _ time.Time, _ string, _ int) (logport.TripPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []logport.TripView
	for _, t := range f.completed {
		if t.CompletedAt != nil && !t.CompletedAt.Before(from) && t.CompletedAt.Before(to) {
			out = append(out, t)
		}
	}
	return logport.TripPage{Items: out}, nil
}

type fixture struct {
	t          *testing.T
	env        *pgtest.Env
	iss        *authztest.FakeIssuer
	app, admin *httptest.Server
	pool       *pgxpool.Pool
	clk        *clock.Fixed
	tenant     uuid.UUID
	mod        *costing.Module
	cons       costing.Consumers
	contracts  *fakeContracts
	origins    *fakeOrigins
	trips      *fakeTrips
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, costing.DBRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	contracts := &fakeContracts{m: map[uuid.UUID]ordport.ContractTerms{}}
	origins := &fakeOrigins{m: map[uuid.UUID]hubport.LotInfo{}}
	trips := &fakeTrips{forLot: map[uuid.UUID][]logport.TripView{}}
	deps := costing.Deps{Origins: origins, Contracts: contracts, Trips: trips}
	mod := costing.New(pool, clk, deps)
	iss := authztest.New(t)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(pool)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pool)))
	a, ad := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(a.Close)
	t.Cleanup(ad.Close)
	return &fixture{t: t, env: env, iss: iss, app: a, admin: ad, pool: pool, clk: clk, tenant: ids.New(), mod: mod,
		cons: costing.NewConsumers(clk, deps), contracts: contracts, origins: origins, trips: trips}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	actor uuid.UUID
	token string
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	return f.asActor(tid, ids.New(), roles...)
}

func (f *fixture) asActor(tid, actor uuid.UUID, roles ...string) caller {
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})}
}

func (f *fixture) accountant() caller { return f.as(f.tenant, port.RoleCostAccountant) }
func (f *fixture) chief() caller      { return f.as(f.tenant, port.RoleChiefAccountant) }
func (f *fixture) closer() caller     { return f.as(f.tenant, port.RolePeriodCloser) }

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
	dec.UseNumber() // không float (R-10)
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

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// deliver giao một sự kiện nội bộ cho consumer costing qua inbox (như worker-internal).
func (f *fixture) deliver(tid uuid.UUID, consumer string, h inbox.Handler, eventType string, payload any, eventID uuid.UUID) bool {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	env := outbox.Envelope{EventID: eventID, EventType: eventType, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		Producer: "test", Payload: raw}
	ok, err := inbox.Process(context.Background(), f.pool, consumer, env, h)
	if err != nil {
		f.t.Fatalf("xử lý %s: %v", eventType, err)
	}
	return ok
}

// inTenant chạy fn trong giao dịch tenant bằng vai costing_app.
func (f *fixture) inTenant(tid uuid.UUID, fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) count(tid uuid.UUID, sql string, args ...any) int {
	f.t.Helper()
	var n int
	f.inTenant(tid, func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), sql, args...).Scan(&n) })
	return n
}

func (f *fixture) dump(tid uuid.UUID) map[string][]string {
	f.t.Helper()
	var d map[string][]string
	f.inTenant(tid, func(tx pgx.Tx) error {
		var err error
		d, err = store.ProjectionDump(context.Background(), tx)
		return err
	})
	return d
}

// prevMonth trả [from, to) của kỳ tháng trước (đã hết — đóng được) và mã kỳ.
func (f *fixture) prevMonth() (string, time.Time, time.Time) {
	curFrom, _, _ := domain.PeriodBounds(domain.PeriodCode(f.clk.Now()))
	code := domain.PeriodCode(curFrom.Add(-time.Hour))
	from, to, _ := domain.PeriodBounds(code)
	return code, from, to
}

func (f *fixture) periodID(tid uuid.UUID, code string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	f.inTenant(tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT period_id FROM mkg_costing.period_current WHERE code = $1", code).Scan(&id)
	})
	return id
}
