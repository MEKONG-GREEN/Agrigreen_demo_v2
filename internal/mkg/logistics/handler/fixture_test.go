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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fixedKg là fieldops/port.KgPerRollEstimates giả: 22 kg/cuộn cho mọi HTX.
type fixedKg struct{}

func (fixedKg) KgPerRollAsOf(context.Context, uuid.UUID, uuid.UUID, time.Time, uuid.UUID) (fieldport.KgPerRollEstimate, error) {
	return fieldport.KgPerRollEstimate{Kg: mass.MustKg("22"), Source: "htx_learned", Samples: 30}, nil
}

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	ww     *adapter.FakeWaterway
	svc    *app.Service
	cons   app.Consumer
	tenant uuid.UUID
}

// newFixture dựng API nội bộ MKG như cmd/api-internal/assembly: pool vai logistics_app ở cụm mkg, nguồn
// định tuyến đường thủy giả (bề mặt đối tác), đường bộ Haversine × tham số. Bề mặt ứng dụng và quản trị
// gắn chung một nhóm (kiểm thử không kiểm MFA).
func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, port.DBRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	f := &fixture{t: t, env: env, pool: pool, clk: clk, ww: &adapter.FakeWaterway{Now: clk.Now}, tenant: ids.New()}
	f.svc = &app.Service{DB: pool, Clock: clk, Waterway: f.ww}
	f.cons = app.Consumer{Clock: clk, KgPerRoll: fixedKg{}}
	f.iss = authztest.New(t)
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	g := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(pool))
	h := handler.Handler{Svc: f.svc}
	h.RegisterApp(g)
	h.RegisterAdmin(g)
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
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
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles})}
}

func (f *fixture) dispatcher() caller { return f.as(f.tenant, port.RoleDispatcher) }
func (f *fixture) driver() caller     { return f.as(f.tenant, port.RoleDriver) }
func (f *fixture) chief() caller      { return f.as(f.tenant, app.RoleChiefAccountant) }

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
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // R-10: không float trong mkg
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

func arr(m map[string]any, path ...string) []map[string]any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	xs, _ := cur.([]any)
	out := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		o, _ := x.(map[string]any)
		out = append(out, o)
	}
	return out
}

func hasFlag(m map[string]any, flag string, path ...string) bool {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur = mm[p]
	}
	xs, _ := cur.([]any)
	for _, x := range xs {
		if s, _ := x.(string); s == flag {
			return true
		}
	}
	return false
}

func (f *fixture) ts(d time.Duration) string { return f.clk.Now().Add(d).Format(time.RFC3339) }

// createTrip lập chuyến tay (điều phối) và trả trip_id.
func (f *fixture) createTrip(c caller, mode string, lot uuid.UUID, kg string) string {
	f.t.Helper()
	r := c.post("/v1/logistics/trips", map[string]any{"mode": mode, "cargo": []map[string]any{{"lot_ref": lot.String(), "mass_kg": kg}}})
	mustStatus(f.t, r, http.StatusCreated, "lập chuyến")
	return str(r.body, "trip", "trip_id")
}

// waterLeg là một chặng đường thủy từ điểm tập kết tới hub.
func waterLeg() map[string]any {
	return map[string]any{"mode": "waterway",
		"from": map[string]any{"kind": "collection_point", "lat": "10.0452", "lng": "105.7469"},
		"to":   map[string]any{"kind": "facility", "ref": ids.New().String(), "lat": "10.3860", "lng": "105.4352"}}
}

func roadLeg(fromLat, fromLng, toLat, toLng string) map[string]any {
	return map[string]any{"mode": "road",
		"from": map[string]any{"kind": "facility", "lat": fromLat, "lng": fromLng},
		"to":   map[string]any{"kind": "customer_site", "lat": toLat, "lng": toLng}}
}

// deliver giao một sự kiện nội bộ cho consumer logistics qua inbox (như worker-internal).
func (f *fixture) deliver(tid uuid.UUID, consumer string, h inbox.Handler, eventType string, payload any, eventID uuid.UUID) bool {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	env := outbox.Envelope{EventID: eventID, EventType: eventType, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		Producer: "mkg/test", Payload: raw}
	ok, err := inbox.Process(context.Background(), f.pool, consumer, env, h)
	if err != nil {
		f.t.Fatalf("xử lý %s: %v", eventType, err)
	}
	return ok
}

// boatLoad giao sự kiện đếm cuộn boat_load của lô (lệnh gom) lot; trả payload.
func (f *fixture) boatLoad(tid, lot uuid.UUID, rolls int, supersedes *uuid.UUID, at time.Time) fieldport.RollCountRecordedV1 {
	f.t.Helper()
	p := fieldport.RollCountRecordedV1{RollCountID: ids.New(), LotRef: lot, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: rolls,
		Stage: fieldport.StageBoatLoad, SupersedesID: supersedes, RuleVersion: "fieldops.count.v1", EventTime: at}
	f.deliver(tid, port.RollCountConsumer, f.cons.HandleRollCount, fieldport.EventRollCountRecorded, p, ids.New())
	return p
}

// weigh giao phiếu cân HUB_IN của lô lot; trả payload.
func (f *fixture) weigh(tid, lot uuid.UUID, kg string, supersedes *uuid.UUID) measport.WeighTicketRecordedV1 {
	f.t.Helper()
	p := measport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub,
		Source: measport.SourceEdge, MassKg: mass.MustKg(kg), EventTime: f.clk.Now().Add(-time.Minute), SupersedesID: supersedes}
	f.deliver(tid, port.WeighTicketConsumer, f.cons.HandleWeighTicket, measport.EventWeighTicketRecorded, p, ids.New())
	return p
}

// tripsForLot đọc chuyến của lô qua port.Trips (cổng cho costing).
func (f *fixture) tripsForLot(tid, lot uuid.UUID) []port.TripView {
	f.t.Helper()
	ts, err := f.svc.TripsForLot(context.Background(), tid, lot)
	if err != nil {
		f.t.Fatal(err)
	}
	return ts
}

type outboxRow struct {
	Kind    string
	Payload port.TripCompletedV1
	Raw     []byte
}

// completedEvents đọc sự kiện trip.completed trên outbox của tenant (thứ tự ghi).
func (f *fixture) completedEvents(tid uuid.UUID) []outboxRow {
	f.t.Helper()
	var out []outboxRow
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT kind, payload FROM shared.outbox WHERE event_type = $1 ORDER BY recorded_at, id`,
			port.EventTripCompleted)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r outboxRow
			if err := rows.Scan(&r.Kind, &r.Raw); err != nil {
				return err
			}
			if err := json.Unmarshal(r.Raw, &r.Payload); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) inTenant(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) mustFail(pool *pgxpool.Pool, what, sql string, args ...any) {
	f.t.Helper()
	err := tenant.WithTenantTx(context.Background(), pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err == nil {
		f.t.Fatalf("%s: câu lệnh phải hỏng", what)
	}
}
