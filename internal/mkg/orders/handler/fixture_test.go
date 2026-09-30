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

	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	admin  *httptest.Server
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	svc    *app.Service
	tenant uuid.UUID
	// Hai người khác nhau: người lập (sales_officer) và người duyệt (sales_approver).
	officer  caller
	approver caller
}

// newFixture dựng API nội bộ MKG như cmd/api-internal/assembly: pool vai orders_app ở cụm mkg, bề mặt
// ứng dụng + bề mặt quản trị (MFA).
func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.MKG, port.DBRole)
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	f := &fixture{t: t, env: env, pool: pool, clk: clk, tenant: ids.New()}
	f.svc = &app.Service{DB: pool, Clock: clk}
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
	f.officer = f.as(f.tenant, port.RoleSalesOfficer)
	f.approver = f.as(f.tenant, port.RoleSalesApprover)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	actor uuid.UUID
	token string
	base  string
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: tid, actor: actor, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles}),
		base: f.srv.URL}
}

// asAdmin là người gọi bề mặt quản trị (MFA).
func (f *fixture) asAdmin(roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: f.tenant, actor: actor, base: f.admin.URL,
		token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, Roles: roles, MFA: true})}
}

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
	req, _ := http.NewRequest(method, c.base+path, rd)
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
	dec.UseNumber() // R-10: không float
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
	case bool:
		if v {
			return "true"
		}
		return "false"
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

func (f *fixture) day(d int) string { return f.clk.Now().AddDate(0, 0, d).Format("2006-01-02") }

func (f *fixture) inTenant(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	f.inTenant(func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), q, args...).Scan(&n) })
	return n
}

// contract lập (officer) và duyệt (approver) một hợp đồng mua rơm; trả contract_id.
func (f *fixture) contract(htx uuid.UUID, basis, price string, from, to int) string {
	f.t.Helper()
	r := f.officer.post("/v1/orders/contracts", map[string]any{"htx_ref": htx.String(), "basis": basis, "unit_price": price,
		"committed_kg": "500000", "valid_from": f.day(from), "valid_to": f.day(to), "max_moisture_pct": "18.00"})
	mustStatus(f.t, r, http.StatusCreated, "lập hợp đồng")
	id := str(r.body, "contract_id")
	d := f.approver.post("/v1/orders/contracts/"+id+"/versions/1/decision", map[string]any{"decision": "approved"})
	mustStatus(f.t, d, http.StatusCreated, "duyệt hợp đồng")
	return id
}

// po lập (officer) và duyệt (approver) một đơn mua; trả po_id.
func (f *fixture) po(body map[string]any) string {
	f.t.Helper()
	r := f.officer.post("/v1/orders/purchase-orders", body)
	mustStatus(f.t, r, http.StatusCreated, "lập đơn mua")
	id := str(r.body, "purchase_order", "po_id")
	d := f.approver.post("/v1/orders/purchase-orders/"+id+"/decision", map[string]any{"decision": "approved"})
	mustStatus(f.t, d, http.StatusCreated, "duyệt đơn mua")
	return id
}

// confirmedSO lập đơn bán không chiết khấu, đúng giá bảng (không cần duyệt giá) và xác nhận; trả order_id.
func (f *fixture) confirmedSO(price string) string {
	f.t.Helper()
	r := f.officer.post("/v1/orders/sales-orders", map[string]any{"customer_ref": ids.New().String(), "product": "compressed_bale",
		"ordered_kg": "50000", "list_price": price, "unit_price": price})
	mustStatus(f.t, r, http.StatusCreated, "lập đơn bán")
	id := str(r.body, "sales_order", "order_id")
	mustStatus(f.t, f.officer.post("/v1/orders/sales-orders/"+id+"/confirm", nil), http.StatusCreated, "xác nhận đơn bán")
	return id
}

// ticket giao phiếu cân (mkg.measurement.weigh_ticket.recorded) cho consumer của orders trong giao dịch
// như inbox (vai orders_app, tenant của phong bì).
func (f *fixture) ticket(tid uuid.UUID, t mport.WeighTicketRecordedV1) {
	f.t.Helper()
	if err := f.deliver(tid, t); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) deliver(tid uuid.UUID, t mport.WeighTicketRecordedV1) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	env := outbox.Envelope{EventID: ids.New(), EventType: mport.EventWeighTicketRecorded, SchemaVersion: 1, TenantID: tid,
		OccurredAt: f.clk.Now(), Producer: "mkg/measurement", Payload: raw}
	ctx := context.Background()
	return tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return f.svc.HandleWeighTicket(ctx, tx, env) })
}

func mustKg(s string) mass.Kg { return mass.MustKg(s) }

func (f *fixture) weigh(lot uuid.UUID, point mport.WeighPoint, kgs string, supersedes *uuid.UUID) uuid.UUID {
	id := ids.New()
	route := mport.RouteHub
	f.ticket(f.tenant, mport.WeighTicketRecordedV1{TicketID: id, LotRef: lot, Point: point, Route: route, Source: mport.SourceEdge,
		MassKg: mass.MustKg(kgs), EventTime: f.clk.Now().Add(-time.Minute), SupersedesID: supersedes})
	return id
}
