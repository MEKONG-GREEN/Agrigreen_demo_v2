package handler_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fakeRolls là RollTotals giả (fieldops chưa nối ở G3-A2).
type fakeRolls struct {
	mu sync.Mutex
	m  map[uuid.UUID]port.RollTotal
}

func (r *fakeRolls) RollTotal(_ context.Context, _ uuid.UUID, lot uuid.UUID) (port.RollTotal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.m[lot]; ok {
		return v, nil
	}
	return port.RollTotal{}, port.ErrNotFound
}

func (r *fakeRolls) set(lot uuid.UUID, rolls int64, kpr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[lot] = port.RollTotal{LotRef: lot, Rolls: rolls, KgPerRollEstimate: mass.MustKg(kpr), EstimateSource: "default"}
}

// fakeLots là nguồn gốc lô giả (mkg/hub port.LotOrigins).
type fakeLots struct {
	mu sync.Mutex
	m  map[uuid.UUID]hubport.LotInfo
}

func (l *fakeLots) LotOrigin(_ context.Context, _, id uuid.UUID) (hubport.LotInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := l.m[id]; ok {
		return v, nil
	}
	return hubport.LotInfo{}, hubport.ErrLotNotFound
}

// fakePlots là tỷ lệ thửa theo lệnh gom giả (port.CollectionPlots, G3-2).
type fakePlots struct {
	mu sync.Mutex
	m  map[uuid.UUID][]port.PlotShare
}

func (p *fakePlots) CollectionPlots(_ context.Context, _, order uuid.UUID) ([]port.PlotShare, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.m[order]; ok {
		return v, nil
	}
	return nil, port.ErrNotFound
}

type fixture struct {
	t          *testing.T
	env        *pgtest.Env
	iss        *authztest.FakeIssuer
	app, admin *httptest.Server
	pool       *pgxpool.Pool // measurement_app
	ingestPool *pgxpool.Pool // ingest_mkg
	ingest     measurement.Ingest
	clk        *clock.Fixed
	tenant     uuid.UUID
	rolls      *fakeRolls
	lots       *fakeLots
	plots      *fakePlots
	mod        *measurement.Module
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, measurement.DBRole)
	ingestPool := env.Pool(t, migrations.MKG, port.IngestRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	rolls := &fakeRolls{m: map[uuid.UUID]port.RollTotal{}}
	lots := &fakeLots{m: map[uuid.UUID]hubport.LotInfo{}}
	plots := &fakePlots{m: map[uuid.UUID][]port.PlotShare{}}
	mod := measurement.New(pool, clk, measurement.Deps{Rolls: rolls, Origins: lots, Plots: plots})
	iss := authztest.New(t)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(pool)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pool)))
	a, ad := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(a.Close)
	t.Cleanup(ad.Close)
	return &fixture{t: t, env: env, iss: iss, app: a, admin: ad, pool: pool, ingestPool: ingestPool, ingest: measurement.NewIngest(ingestPool, clk),
		clk: clk, tenant: ids.New(), rolls: rolls, lots: lots, plots: plots, mod: mod}
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

func (f *fixture) supervisor() caller { return f.as(f.tenant, true, port.RoleScaleSupervisor) }
func (f *fixture) approver() caller   { return f.as(f.tenant, true, port.RoleDeviceApprover) }
func (f *fixture) officer() caller    { return f.as(f.tenant, true, port.RoleReconciliationOfficer) }
func (f *fixture) disputer() caller   { return f.as(f.tenant, true, port.RoleDisputeOfficer) }
func (f *fixture) tolApprover() caller {
	return f.as(f.tenant, true, port.RoleToleranceApprover)
}

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

// scaleDevice là một thiết bị cân kiểm thử: khóa Ed25519 + băm chứng thư.
type scaleDevice struct {
	id     string
	pub    ed25519.PublicKey
	cert   string
	epoch  int
	facRef uuid.UUID
}

func newScale(t *testing.T, id string) scaleDevice {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("cert-der-" + id + ids.New().String()))
	return scaleDevice{id: id, pub: pub, cert: hex.EncodeToString(sum[:]), facRef: ids.New()}
}

func (d scaleDevice) body(point string) map[string]any {
	return map[string]any{"device_id": d.id, "facility_ref": d.facRef.String(), "point": point,
		"public_key": base64.StdEncoding.EncodeToString(d.pub), "cert_sha256": d.cert, "scale_model": "weighbridge-60t (C-6 tạm)"}
}

// approvedScale đăng ký và duyệt (hai người khác nhau) một kỳ thiết bị.
func (f *fixture) approvedScale(id, point string) scaleDevice {
	f.t.Helper()
	d := newScale(f.t, id)
	r := f.supervisor().adm(http.MethodPost, "/v1/measurement/devices", d.body(point))
	mustStatus(f.t, r, http.StatusCreated, "đề xuất thiết bị")
	ep, _ := r.body["epoch"].(json.Number).Int64()
	d.epoch = int(ep)
	mustStatus(f.t, f.approver().adm(http.MethodPost, "/v1/measurement/devices/"+id+"/epochs/"+ep2s(d.epoch)+"/decision",
		map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt thiết bị")
	return d
}

func ep2s(n int) string { return strconv.Itoa(n) }

func itoa(n int) string { return strconv.Itoa(n) }

// edgeTicket ghi phiếu edge qua vai ingest_mkg (như cmd/ingest sau khi tích phân).
func (f *fixture) edgeTicket(d scaleDevice, lot uuid.UUID, point port.WeighPoint, kg string, from, to int64, supersedes *uuid.UUID, reason string) (port.Ticket, error) {
	dr := port.TicketDraft{
		ID: ids.New(), IdemKey: "ingest:" + d.id + ":" + itoa(int(from)) + ":" + ids.New().String(), LotRef: lot, Point: point, Route: port.RouteHub,
		Source: port.SourceEdge, MassKg: mass.MustKg(kg), MoisturePct: "18.00", EventTime: f.clk.Now().Add(-time.Hour), ActorRef: ids.New(),
		Integration: &port.Integration{Window: port.Window{DeviceID: d.id, Epoch: d.epoch, CounterFrom: from, CounterTo: to},
			NetKg: mass.MustKg(kg), ReadingsUsed: int(to - from + 1), ReadingsTotal: int(to - from + 1), BatchIDs: []uuid.UUID{ids.New()},
			RuleVersion: "weigh.integrate.v1"},
		Supersedes: supersedes, CorrectionReason: reason,
	}
	return f.ingest.Tickets.Record(context.Background(), f.tenant, dr)
}

// inTenant chạy fn trong giao dịch tenant bằng vai measurement_app.
func (f *fixture) inTenant(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}

func tokenFor(actor, tid uuid.UUID, roles ...string) authztest.Token {
	return authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true}
}
