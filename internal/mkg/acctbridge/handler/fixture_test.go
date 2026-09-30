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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fakePeriods là costing/port.ClosedPeriods giả (costing được làm song song — chỉ phụ thuộc port).
type fakePeriods struct {
	mu  sync.Mutex
	m   map[[2]uuid.UUID]cp.PeriodSummary
	err error
}

func (p *fakePeriods) ClosedPeriod(_ context.Context, tid, period uuid.UUID) (cp.PeriodSummary, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return cp.PeriodSummary{}, p.err
	}
	s, ok := p.m[[2]uuid.UUID{tid, period}]
	if !ok {
		return cp.PeriodSummary{}, cp.ErrPeriodNotFound
	}
	return s, nil
}

func (p *fakePeriods) put(tid uuid.UUID, s cp.PeriodSummary) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[[2]uuid.UUID{tid, s.PeriodID}] = s
}

type fixture struct {
	t       *testing.T
	env     *pgtest.Env
	iss     *authztest.FakeIssuer
	srv     *httptest.Server
	admin   *httptest.Server
	pool    *pgxpool.Pool
	clk     *clock.Fixed
	periods *fakePeriods
	fake    *adapter.Fake
	svc     *app.Service
	tenant  uuid.UUID
}

// newFixture dựng API nội bộ MKG như cmd/api-internal/assembly (pool vai acctbridge_app, hai bề mặt) và
// handler consumer như cmd/worker-internal. ad = nil → bộ giả.
func newFixture(t *testing.T, ad port.LedgerAdapter) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, port.DBRole)
	clk := clock.NewFixed(time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC))
	f := &fixture{t: t, env: env, pool: pool, clk: clk, periods: &fakePeriods{m: map[[2]uuid.UUID]cp.PeriodSummary{}}, fake: adapter.NewFake(clk),
		tenant: ids.New()}
	if ad == nil {
		ad = f.fake
	}
	f.svc = &app.Service{DB: pool, Clock: clk, Periods: f.periods, Adapter: ad, RetryMax: 3, Metrics: app.NewMetrics(nil)}
	f.iss = authztest.New(t)
	h := handler.Handler{Svc: f.svc}
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	h.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(pool)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	adm := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	h.RegisterAdmin(adm.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pool)))
	f.admin = httptest.NewServer(adm)
	t.Cleanup(f.admin.Close)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	actor uuid.UUID
	token string
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})}
}

func (f *fixture) operator() caller { return f.as(f.tenant, port.RoleAccountingBridgeOperator) }
func (f *fixture) chief() caller    { return f.as(f.tenant, port.RoleChiefAccountant) }

type resp struct {
	status int
	body   map[string]any
	raw    []byte
	header http.Header
}

func (c caller) call(base, method, path, key string, body any) resp {
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
	dec.UseNumber() // R-10: không float trong acctbridge
	_ = dec.Decode(&m)
	return resp{status: r.StatusCode, body: m, raw: raw, header: r.Header}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp {
	return c.call(c.f.srv.URL, http.MethodPost, path, key(), body)
}
func (c caller) get(path string) resp { return c.call(c.f.srv.URL, http.MethodGet, path, "", nil) }
func (c caller) adminPost(path string, body any) resp {
	return c.call(c.f.admin.URL, http.MethodPost, path, key(), body)
}

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

func (f *fixture) inTenant(tid uuid.UUID, fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, fn); err != nil {
		f.t.Fatal(err)
	}
}

// approvedMapping: vận hành A đề xuất bảng mặc định (tạm), kế toán trưởng B duyệt. Trả version_id.
func (f *fixture) approvedMapping(tid uuid.UUID, rules any) string {
	f.t.Helper()
	body := map[string]any{"reason": "Bảng ánh xạ tạm theo VAS, chờ khách chốt", "effective_from": "2026-01-01"}
	if rules != nil {
		body["rules"] = rules
	}
	r := f.as(tid, port.RoleAccountingBridgeOperator).adminPost("/v1/acctbridge/mappings", body)
	mustStatus(f.t, r, http.StatusCreated, "đề xuất ánh xạ")
	v := str(r.body, "mapping", "version_id")
	d := f.as(tid, port.RoleChiefAccountant).adminPost("/v1/acctbridge/mappings/"+v+"/decision", map[string]any{"decision": "approved"})
	mustStatus(f.t, d, http.StatusCreated, "duyệt ánh xạ")
	return v
}

var (
	htxA = uuid.MustParse("0192a000-0000-7000-8000-00000000a001")
	cust = uuid.MustParse("0192a000-0000-7000-8000-00000000c001")
)

// summary là kết quả đóng kỳ giả của costing cho kỳ 2026-09.
func summary(period, closeRun uuid.UUID, hash string) cp.PeriodSummary {
	return cp.PeriodSummary{PeriodID: period, PeriodCode: "2026-09", From: time.Date(2026, 8, 31, 17, 0, 0, 0, time.UTC),
		To: time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC), Status: cp.PeriodClosed, CloseRunID: closeRun, RuleVersion: "costing.close.v1",
		InputSnapshotSHA256: hash, ResultSetSHA256: hash,
		Lines: []cp.SummaryLine{
			{Category: cp.CatStrawPurchase, DimensionKind: "htx", DimensionRef: &htxA, Amount: money.MustAmount("120000000.00"), Entries: 3},
			{Category: cp.CatTransportWaterway, DimensionKind: "mode", Amount: money.MustAmount("8100000.00"), Entries: 4},
			{Category: cp.CatHubProcessing, DimensionKind: "facility", Amount: money.MustAmount("12000000.00"), Entries: 5},
			{Category: cp.CatSalesRevenue, DimensionKind: "customer", DimensionRef: &cust, Amount: money.MustAmount("310000000.00"), Entries: 2},
		}}
}

func hashOf(s string) string { return domain.SHA256Hex([]byte(ids.New().String() + s)) }

// closePeriod đăng kỳ đã đóng ở costing giả và giao mkg.costing.period.closed cho consumer (inbox, R-61).
// Trả (close_run_id, event_id).
func (f *fixture) closePeriod(tid, period, closer uuid.UUID) (uuid.UUID, uuid.UUID) {
	f.t.Helper()
	run := ids.New()
	s := summary(period, run, hashOf("run"))
	f.periods.put(tid, s)
	eid := ids.New()
	f.deliverClosed(tid, s, closer, eid)
	return run, eid
}

func (f *fixture) deliverClosed(tid uuid.UUID, s cp.PeriodSummary, closer, eventID uuid.UUID) bool {
	f.t.Helper()
	ok, err := f.processClosed(context.Background(), tid, s, closer, eventID)
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

// processClosed giao mkg.costing.period.closed cho consumer qua inbox (an toàn trong goroutine — không dùng t).
func (f *fixture) processClosed(ctx context.Context, tid uuid.UUID, s cp.PeriodSummary, closer, eventID uuid.UUID) (bool, error) {
	raw, err := json.Marshal(cp.PeriodClosedV1{PeriodID: s.PeriodID, PeriodCode: s.PeriodCode, From: s.From, To: s.To, CloseRunID: s.CloseRunID,
		InputSnapshotSHA256: s.InputSnapshotSHA256, ResultSetSHA256: s.ResultSetSHA256, LineCount: len(s.Lines), ClosedBy: closer,
		RuleVersion: s.RuleVersion, EventTime: f.clk.Now()})
	if err != nil {
		return false, err
	}
	env := outbox.Envelope{EventID: eventID, EventType: cp.EventPeriodClosed, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		Producer: "mkg/costing", Payload: raw}
	return inbox.Process(ctx, f.pool, port.PeriodClosedConsumer, env, f.svc.HandlePeriodClosed)
}

// drain giả lập worker-internal: giao mọi sự kiện job (dựng, đẩy) của tenant trên outbox cho consumer
// tương ứng qua inbox.Process (giao lặp an toàn). Trả số sự kiện xử lý mới.
func (f *fixture) drain(tid uuid.UUID) int {
	f.t.Helper()
	n, err := drainTenant(context.Background(), f.pool, f.svc, tid)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func drainTenant(ctx context.Context, pool *pgxpool.Pool, svc *app.Service, tid uuid.UUID) (int, error) {
	total := 0
	for round := 0; round < 10; round++ {
		var envs []outbox.Envelope
		err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id, event_type, schema_version, occurred_at, producer, payload FROM shared.outbox
			  WHERE event_type IN ($1, $2) ORDER BY id`, port.EventJournalBuildRequested, port.EventJournalPushRequested)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				e := outbox.Envelope{TenantID: tid}
				var p []byte
				if err := rows.Scan(&e.EventID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.Producer, &p); err != nil {
					return err
				}
				e.Payload = p
				envs = append(envs, e)
			}
			return rows.Err()
		})
		if err != nil {
			return total, err
		}
		n := 0
		for _, e := range envs {
			consumer, h := port.JournalBuildConsumer, svc.HandleBuildRequested
			if e.EventType == port.EventJournalPushRequested {
				consumer, h = port.JournalPushConsumer, svc.HandlePushRequested
			}
			ok, err := inbox.Process(ctx, pool, consumer, e, h)
			if err != nil {
				return total, err
			}
			if ok {
				n++
			}
		}
		total += n
		if n == 0 {
			return total, nil
		}
	}
	return total, nil
}

// journalOf trả bút toán hiệu lực (không superseded) của kỳ qua API.
func (f *fixture) journalOf(c caller, period uuid.UUID) map[string]any {
	f.t.Helper()
	r := c.get("/v1/acctbridge/journals?period_id=" + period.String())
	mustStatus(f.t, r, http.StatusOK, "liệt kê bút toán")
	for _, j := range arr(r.body, "items") {
		if str(j, "status") != "superseded" {
			g := c.get("/v1/acctbridge/journals/" + str(j, "journal_id"))
			mustStatus(f.t, g, http.StatusOK, "đọc bút toán")
			return g.body
		}
	}
	f.t.Fatalf("kỳ %s chưa có bút toán hiệu lực: %s", period, r.raw)
	return nil
}
