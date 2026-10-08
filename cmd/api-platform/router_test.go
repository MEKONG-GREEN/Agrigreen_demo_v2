package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t   *testing.T
	srv *httptest.Server
	iss *authztest.FakeIssuer
	env *pgtest.Env
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mods, pools, err := buildModules(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter(Deps{
		Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier(),
		Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods, Pools: pools,
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &fixture{t: t, srv: srv, iss: iss, env: env}
}

type caller struct {
	f     *fixture
	token string
	actor uuid.UUID
	tid   uuid.UUID
}

func (f *fixture) caller(tid uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, actor: actor, tid: tid, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles})}
}

func (c caller) do(method, path, idem string, body any) (*http.Response, map[string]any) {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.f.srv.URL+path, rd)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func consentBody(subject uuid.UUID, status string) map[string]any {
	return map[string]any{
		"subject_ref": subject.String(), "purpose": "mrv.evidence", "text_version": "2026-11",
		"status": status, "channel": "field_app", "event_time": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
	}
}

func TestUnauthenticated(t *testing.T) {
	f := newFixture(t)
	resp, body := caller{f: f}.do("GET", "/v1/consents?subject_ref="+ids.New().String(), "", nil)
	if resp.StatusCode != 401 || resp.Header.Get("Content-Type") != "application/problem+json" || body["trace_id"] == nil {
		t.Fatalf("muốn 401 problem+json có trace_id, nhận %d %s %v", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

// R-51: thiếu khóa → 400; gọi lặp → đúng phản hồi lần đầu; cùng khóa khác thân → 422.
func TestIdempotentCreate(t *testing.T) {
	f := newFixture(t)
	w := f.caller(ids.New(), port.RoleConsentWriter)
	subject := ids.New()
	body := consentBody(subject, "granted")

	resp, pb := w.do("POST", "/v1/consents", "", body)
	if resp.StatusCode != 400 || pb["code"] != "idempotency_key_required" {
		t.Fatalf("thiếu Idempotency-Key: %d %v", resp.StatusCode, pb)
	}

	key := "k-" + ids.New().String()
	r1, b1 := w.do("POST", "/v1/consents", key, body)
	if r1.StatusCode != 201 || r1.Header.Get("Location") == "" {
		t.Fatalf("tạo: %d %v", r1.StatusCode, b1)
	}
	r2, b2 := w.do("POST", "/v1/consents", key, body)
	if r2.StatusCode != 201 || r2.Header.Get("Idempotent-Replayed") != "true" || b2["id"] != b1["id"] || b2["seq"] != b1["seq"] {
		t.Fatalf("gọi lặp phải trả đúng phản hồi lần đầu: %d %v vs %v", r2.StatusCode, b2, b1)
	}
	other := consentBody(subject, "withdrawn")
	r3, b3 := w.do("POST", "/v1/consents", key, other)
	if r3.StatusCode != 422 || b3["code"] != "idempotency_key_reused" {
		t.Fatalf("cùng khóa khác thân: %d %v", r3.StatusCode, b3)
	}

	// Chỉ đúng một bản ghi sổ và một sự kiện outbox.
	pub := f.env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	var n int
	if err := pub.QueryRow(context.Background(),
		"SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND payload->>'consent_id' = $2", w.tid, b1["id"]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("outbox phải có đúng 1 sự kiện, có %d", n)
	}
}

func TestValidationProblem(t *testing.T) {
	f := newFixture(t)
	w := f.caller(ids.New(), port.RoleConsentWriter)
	body := consentBody(ids.New(), "granted")
	body["purpose"] = "Sai Định Dạng"
	resp, pb := w.do("POST", "/v1/consents", "k-"+ids.New().String(), body)
	if resp.StatusCode != 422 || pb["code"] != "validation_failed" || pb["errors"] == nil {
		t.Fatalf("muốn 422 có errors theo trường: %d %v", resp.StatusCode, pb)
	}
	if !strings.HasSuffix(pb["type"].(string), "validation_failed") {
		t.Fatalf("type = %v", pb["type"])
	}
}

func TestRoleAndTenantIsolation(t *testing.T) {
	f := newFixture(t)
	a := f.caller(ids.New(), port.RoleConsentWriter)
	_, created := a.do("POST", "/v1/consents", "k-"+ids.New().String(), consentBody(ids.New(), "granted"))
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("tạo thất bại: %v", created)
	}

	noRole := f.caller(a.tid)
	if resp, _ := noRole.do("GET", "/v1/consents/"+id, "", nil); resp.StatusCode != 403 {
		t.Fatalf("thiếu vai phải 403, nhận %d", resp.StatusCode)
	}
	b := f.caller(ids.New(), port.RoleConsentReader)
	if resp, _ := b.do("GET", "/v1/consents/"+id, "", nil); resp.StatusCode != 404 {
		t.Fatalf("tenant khác phải 404, nhận %d", resp.StatusCode)
	}
	if resp, _ := a.do("GET", "/v1/consents/"+id, "", nil); resp.StatusCode != 200 {
		t.Fatalf("chủ sở hữu phải đọc được, nhận %d", resp.StatusCode)
	}
}

// R-53: phân trang cursor.
func TestCursorPagination(t *testing.T) {
	f := newFixture(t)
	w := f.caller(ids.New(), port.RoleConsentWriter)
	subject := ids.New()
	for i := 0; i < 3; i++ {
		if resp, b := w.do("POST", "/v1/consents", "k-"+ids.New().String(), consentBody(subject, "granted")); resp.StatusCode != 201 {
			t.Fatalf("tạo %d: %v", i, b)
		}
	}
	_, p1 := w.do("GET", "/v1/consents?limit=2&subject_ref="+subject.String(), "", nil)
	items, _ := p1["items"].([]any)
	cur, _ := p1["next_cursor"].(string)
	if len(items) != 2 || cur == "" {
		t.Fatalf("trang 1: %v", p1)
	}
	_, p2 := w.do("GET", "/v1/consents?limit=2&subject_ref="+subject.String()+"&cursor="+cur, "", nil)
	items2, _ := p2["items"].([]any)
	if len(items2) != 1 || p2["next_cursor"] != nil {
		t.Fatalf("trang 2: %v", p2)
	}
	seq := items2[0].(map[string]any)["seq"].(float64)
	if seq != 3 {
		t.Fatalf("chuỗi băm theo chủ thể: bản ghi thứ 3 phải có seq 3, nhận %v", seq)
	}
}
