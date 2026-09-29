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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
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

type fixture struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	app    *httptest.Server
	admin  *httptest.Server
	mod    *waterway.Module
	pool   *pgxpool.Pool
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, waterway.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	// Không nối pf/masterdata: mọi tham số lấy mặc định trong mã (domain.DefaultParams).
	mod := waterway.New(pool, clock.System{}, waterway.Deps{})
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	return &fixture{t: t, iss: iss, app: appSrv, admin: adminSrv, mod: mod, pool: pool, tenant: ids.New()}
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})
	return caller{f: f, tid: tid, token: tok, actor: actor}
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) do(base, method, path, idem string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
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
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func (c caller) app(method, path string) resp { return c.do(c.f.app.URL, method, path, "", nil) }

func (c caller) adm(method, path string, body any) resp {
	return c.do(c.f.admin.URL, method, path, "", body)
}

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (r resp) str(k string) string { s, _ := r.body[k].(string); return s }

func (r resp) num(k string) float64 { n, _ := r.body[k].(float64); return n }

func (r resp) items() []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(map[string]any))
	}
	return out
}

func (r resp) fieldReason(field string) string {
	errs, _ := r.body["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["field"] == field {
			s, _ := m["reason"].(string)
			return s
		}
	}
	return ""
}

func expect(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		t.Fatalf("mong %d %s, nhận %d %v", status, code, r.status, r.body)
	}
}

// line dựng GeoJSON LineString từ các cặp (lat, lng).
func line(latlng ...float64) map[string]any {
	var coords [][]float64
	for i := 0; i+1 < len(latlng); i += 2 {
		coords = append(coords, []float64{latlng[i+1], latlng[i]})
	}
	return map[string]any{"type": "LineString", "coordinates": coords}
}

func segBody(name, status string, width, depth any, geom map[string]any) map[string]any {
	return map[string]any{"name": name, "waterway_class": port.ClassMainCanal, "status": status, "geometry": geom,
		"width_m": width, "depth_m": depth, "data_source": "survey"}
}

// segment tạo một đoạn và trả segment_ref.
func (c caller) segment(name, status string, width, depth any, geom map[string]any) string {
	c.f.t.Helper()
	r := c.adm(http.MethodPost, "/v1/waterway/segments", segBody(name, status, width, depth, geom))
	expect(c.f.t, r, 201, "")
	return r.str("segment_ref")
}

// requestedEvent đọc sự kiện pf.waterway.network_build.requested của một job từ outbox.
func (f *fixture) requestedEvent(tid uuid.UUID, buildID string) outbox.Envelope {
	f.t.Helper()
	var e outbox.Envelope
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at,
		    producer, payload FROM shared.outbox WHERE event_type = $1 AND payload->>'build_id' = $2`,
			port.EventBuildRequested, buildID).
			Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.Producer, &e.Payload)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

// build yêu cầu dựng (202), chạy worker, trả số phiên bản kết quả.
func (c caller) build() (int, resp) {
	c.f.t.Helper()
	j := c.adm(http.MethodPost, "/v1/waterway/networks/builds", nil)
	expect(c.f.t, j, 202, "")
	id := j.str("id")
	env := c.f.requestedEvent(c.tid, id)
	if _, err := inbox.Process(context.Background(), c.f.pool, port.BuildConsumer, env, c.f.mod.BuildHandler()); err != nil {
		c.f.t.Fatal(err)
	}
	st := c.adm(http.MethodGet, "/v1/waterway/networks/builds/"+id, nil)
	expect(c.f.t, st, 200, "")
	if st.str("status") != "completed" {
		c.f.t.Fatalf("job dựng chưa xong: %v", st.body)
	}
	return int(st.num("network_version_no")), st
}

func authztestToken(sub, tid uuid.UUID, roles ...string) authztest.Token {
	return authztest.Token{Subject: sub, TenantID: tid, Roles: roles, MFA: false}
}

func inboxProcess(ctx context.Context, f *fixture, env outbox.Envelope) (bool, error) {
	return inbox.Process(ctx, f.pool, port.BuildConsumer, env, f.mod.BuildHandler())
}

func (f *fixture) count(tid uuid.UUID, sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	}); err != nil {
		f.t.Fatal(err)
	}
	return n
}
