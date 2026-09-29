package handler_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/simtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

var t0 = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

type fixture struct {
	t         *testing.T
	env       *pgtest.Env
	iss       *authztest.FakeIssuer
	app       *httptest.Server
	internal  *httptest.Server
	mod       *simulation.Module
	pool      *pgxpool.Pool
	params    *simtest.Params
	supply    *simtest.Supply
	router    *simtest.Router
	tenant    uuid.UUID
	coopA     uuid.UUID
	coopNoGPS uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	f := &fixture{t: t, env: env, iss: authztest.New(t), tenant: ids.New(), params: &simtest.Params{}, router: &simtest.Router{Version: "ww-v7", WaterwayFound: true},
		coopA: ids.New(), coopNoGPS: ids.New()}
	f.supply = &simtest.Supply{Data: port.SupplyData{
		Cooperatives: []port.CooperativeSupply{
			simtest.Coop(f.coopA, 10.42, 105.47, 120, "9000", "8000", "5000"),
			simtest.Coop(ids.New(), 10.30, 105.40, 80, "6000", "5500", "3000"),
			simtest.Coop(ids.New(), 11.50, 107.50, 50, "4000", "4000", "2000"), // ngoài mọi bán kính
			simtest.Coop(f.coopNoGPS, 0, 0, 30, "3000", "3000", "1000"),        // thiếu toạ độ → bị loại
		},
		Seasons: simtest.Seasons,
	}}
	f.pool = env.Pool(t, migrations.PF, simulation.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f.mod = simulation.New(f.pool, clock.NewFixed(t0), simulation.Deps{Params: f.params, Supply: f.supply, Router: f.router,
		InternalAuth: func(*http.Request) error { return nil }})
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	appEng.Use(logErrors(t))
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(ops)))
	intEng := httpx.NewEngine(log, handler.MaxResultBytes)
	intEng.Use(logErrors(t))
	f.mod.RegisterInternal(intEng.Group("/internal/v1"))
	f.app, f.internal = httptest.NewServer(appEng), httptest.NewServer(intEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.internal.Close)
	return f
}

func logErrors(t *testing.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("%s %s: %v", c.Request.Method, c.FullPath(), e.Err)
		}
	}
}

type caller struct {
	f      *fixture
	token  string
	tenant uuid.UUID
}

func (f *fixture) analyst() caller { return f.asTenant(f.tenant, port.RoleSimulationAnalyst) }

func (f *fixture) asTenant(tid uuid.UUID, roles ...string) caller {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, TTL: time.Hour})
	return caller{f: f, token: tok, tenant: tid}
}

type resp struct {
	status int
	body   map[string]any
	raw    []byte
	header http.Header
}

func do(t *testing.T, method, url string, hdr map[string]string, body []byte) resp {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, body: m, raw: raw, header: r.Header}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) postKey(path, idem string, body any) resp {
	b, _ := json.Marshal(body)
	return do(c.f.t, http.MethodPost, c.f.app.URL+path, map[string]string{"Authorization": "Bearer " + c.token, "Idempotency-Key": idem}, b)
}

func (c caller) post(path string, body any) resp { return c.postKey(path, key(), body) }

func (c caller) get(path string) resp {
	return do(c.f.t, http.MethodGet, c.f.app.URL+path, map[string]string{"Authorization": "Bearer " + c.token}, nil)
}

func must(t *testing.T, r resp, want int, what string) resp {
	t.Helper()
	if r.status != want {
		t.Fatalf("%s: muốn %d, nhận %d %s", what, want, r.status, r.raw)
	}
	return r
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// internalGet/internalPost gọi Internal API như optimizer.
func (f *fixture) internalGet(tid uuid.UUID, simID string) resp {
	return do(f.t, http.MethodGet, f.internal.URL+"/internal/v1/simulations/"+simID+"/snapshot", map[string]string{handler.TenantHeader: tid.String()}, nil)
}

func (f *fixture) internalPost(tid uuid.UUID, simID string, body []byte) resp {
	return do(f.t, http.MethodPost, f.internal.URL+"/internal/v1/simulations/"+simID+"/result", map[string]string{handler.TenantHeader: tid.String()}, body)
}

// events đọc sự kiện của tenant trong outbox (tài khoản quản trị, chỉ kiểm thử).
func (f *fixture) events(tid uuid.UUID, eventType string) []outbox.Envelope {
	f.t.Helper()
	rows, err := f.env.AdminPool(f.t, migrations.PF).Query(context.Background(), `SELECT id, tenant_id, event_type, schema_version,
	    occurred_at, recorded_at, coalesce(trace_id, ''), producer, payload FROM shared.outbox
	  WHERE tenant_id = $1 AND event_type = $2 ORDER BY id`, tid, eventType)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []outbox.Envelope
	for rows.Next() {
		var e outbox.Envelope
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.TraceID, &e.Producer, &e.Payload); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// runWorker giao mọi pf.simulation.run.submitted cho consumer dựng ảnh chụp (qua inbox, như worker).
func (f *fixture) runWorker(tid uuid.UUID) {
	f.t.Helper()
	for _, e := range f.events(tid, port.EventRunSubmitted) {
		if _, err := inbox.Process(context.Background(), f.pool, port.SnapshotConsumer, e, f.mod.SnapshotHandler()); err != nil {
			f.t.Fatal(err)
		}
	}
}

// scenario dựng Hub + kịch bản + gán Hub, trả (scenario_id, hub_id).
func (f *fixture) scenario(c caller, lifecycle int, discounted bool) (string, string) {
	f.t.Helper()
	hub := must(f.t, c.post("/v1/candidate-hubs", map[string]any{"name": "Hub Long Xuyên", "location": map[string]any{"lat": 10.38, "lng": 105.435}}), 201, "tạo Hub")
	sc := must(f.t, c.post("/v1/scenarios", map[string]any{"name": "Phương án 1 Hub", "plant": map[string]any{"location": map[string]any{"lat": 10.9, "lng": 106.7}},
		"lifecycle_years": lifecycle, "discounted": discounted, "reference_year": 2026}), 201, "tạo kịch bản")
	must(f.t, c.post("/v1/scenarios/"+str(sc.body, "id")+"/hubs", map[string]any{"hub_id": str(hub.body, "id"), "service_radius_km": "25",
		"design_capacity_tons": "30000"}), 200, "gán Hub")
	return str(sc.body, "id"), str(hub.body, "id")
}

// submitAndSnapshot gửi mô phỏng rồi chạy worker; trả id mô phỏng và byte ảnh chụp.
func (f *fixture) submitAndSnapshot(c caller, scenarioID string, extra map[string]any) (string, []byte) {
	f.t.Helper()
	body := map[string]any{"scenario_id": scenarioID}
	for k, v := range extra {
		body[k] = v
	}
	r := must(f.t, c.post("/v1/simulations", body), 202, "gửi mô phỏng")
	id := str(r.body, "id")
	if loc := r.header.Get("Location"); loc != "/v1/jobs/"+id {
		f.t.Fatalf("Location = %q", loc)
	}
	f.runWorker(c.tenant)
	snap := must(f.t, f.internalGet(c.tenant, id), 200, "đọc ảnh chụp")
	return id, snap.raw
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// CGH FN-13 / mục 12: gửi 202 → worker dựng ảnh chụp → optimizer (giả) nộp kết quả → job.completed.
func TestSimulationAsyncFlowEndToEnd(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	id, raw := f.submitAndSnapshot(c, scID, map[string]any{"sensitivity": true})

	req := f.events(f.tenant, port.EventJobRequested)
	if len(req) != 1 {
		t.Fatalf("muốn 1 job.requested, có %d", len(req))
	}
	var pl port.JobRequestedV1
	_ = json.Unmarshal(req[0].Payload, &pl)
	if pl.SnapshotSHA256 != sha(raw) || pl.SnapshotPath != "/internal/v1/simulations/"+id+"/snapshot" {
		t.Fatalf("job.requested không trỏ đúng ảnh chụp: %+v", pl)
	}
	job := must(t, c.get("/v1/jobs/"+id), 200, "job")
	if str(job.body, "status") != "running" {
		t.Fatalf("job = %v", job.body)
	}
	if r := c.get("/v1/simulations/" + id + "/result"); r.status != 409 || str(r.body, "code") != "simulation.result_not_ready" {
		t.Fatalf("kết quả trước khi tính: %d %s", r.status, r.raw)
	}
	must(t, f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, sha(raw), 350000, 10, false)), 201, "nộp kết quả")
	res := must(t, c.get("/v1/simulations/"+id+"/result"), 200, "đọc kết quả")
	if str(res.body, "status") != "tham_khao" || res.body["official_eligible"] != false || res.body["sensitivity"] == nil {
		t.Fatalf("kết quả: %s", res.raw)
	}
	done := f.events(f.tenant, port.EventJobCompleted)
	var cpl port.JobCompletedV1
	if len(done) == 1 {
		_ = json.Unmarshal(done[0].Payload, &cpl)
	}
	if len(done) != 1 || cpl.Status != "completed" || cpl.ResultSHA256 == nil || cpl.OfficialEligible {
		t.Fatalf("job.completed: %v", done)
	}
	if s := must(t, c.get("/v1/simulations/"+id), 200, "đọc mô phỏng"); str(s.body, "status") != "completed" {
		t.Fatalf("trạng thái %v", s.body["status"])
	}
}

// Mục 16: ảnh chụp bất biến (UPDATE/DELETE bị chặn cả với tài khoản quản trị), băm =
// sha256 của đúng byte Internal API trả; dựng lại lần hai không gọi lại nguồn.
func TestSnapshotIsImmutableAndHashed(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	id, raw := f.submitAndSnapshot(c, scID, nil)
	sim := must(t, c.get("/v1/simulations/"+id), 200, "đọc mô phỏng")
	snap := sim.body["snapshot"].(map[string]any)
	if str(snap, "sha256") != sha(raw) {
		t.Fatalf("băm lưu %s ≠ sha256(byte trả) %s", str(snap, "sha256"), sha(raw))
	}
	again := must(t, f.internalGet(f.tenant, id), 200, "đọc lại")
	if !bytes.Equal(again.raw, raw) || again.header.Get("X-Content-SHA256") != sha(raw) {
		t.Fatal("đọc lại ảnh chụp phải cho cùng byte")
	}
	ctx := context.Background()
	for name, pool := range map[string]*pgxpool.Pool{"simulation_app": f.pool, "admin": f.env.AdminPool(t, migrations.PF)} {
		for _, q := range []string{
			`UPDATE pf_simulation.input_snapshot SET network_version = 'x' WHERE simulation_id = $1`,
			`DELETE FROM pf_simulation.input_snapshot WHERE simulation_id = $1`,
		} {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", f.tenant.String())
			if _, err := tx.Exec(ctx, q, uuid.MustParse(id)); err == nil {
				t.Fatalf("%s: %q phải bị chặn", name, q)
			}
			_ = tx.Rollback(ctx)
		}
	}
	// Giao lặp sự kiện: không dựng ảnh chụp thứ hai (R-61).
	calls := f.supply.Calls
	f.runWorker(f.tenant)
	if f.supply.Calls != calls || len(f.events(f.tenant, port.EventJobRequested)) != 1 {
		t.Fatal("giao lặp run.submitted không được dựng lại ảnh chụp")
	}
}

// RB-7: ảnh chụp chỉ có mã tham chiếu — không tên, không toạ độ; HTX thiếu GPS và ngoài bán kính bị loại;
// kết quả định tuyến ghi kèm nguồn (router giả: đường thủy tuyến số hoá, đường bộ Haversine hiệu chỉnh).
func TestSnapshotIsDeidentifiedAndCarriesRoutingResults(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	_, raw := f.submitAndSnapshot(c, scID, nil)
	text := string(raw)
	for _, bad := range []string{`"name":"Hub`, `"lat"`, `"lng"`, "Long Xuyên", "Phương án", f.coopNoGPS.String(), "10.42", "105.47"} {
		if strings.Contains(text, bad) {
			t.Fatalf("ảnh chụp lộ %q", bad)
		}
	}
	var s map[string]any
	_ = json.Unmarshal(raw, &s)
	if n := len(s["cooperatives"].([]any)); n != 2 {
		t.Fatalf("muốn 2 HTX trong bán kính có toạ độ, có %d", n)
	}
	if s["network_version"] != "ww-v7" {
		t.Fatalf("network_version = %v", s["network_version"])
	}
	sources := map[string]bool{}
	for _, r := range s["routes"].([]any) {
		m := r.(map[string]any)
		sources[m["mode"].(string)+"/"+m["source"].(string)] = true
		if m["source"] == port.SourceHaversine && m["straight_km"] == nil {
			t.Fatal("tuyến Haversine phải ghi straight_km")
		}
	}
	if !sources["road/"+port.SourceHaversine] || !sources["waterway/"+port.SourceDigitized] {
		t.Fatalf("nguồn định tuyến: %v", sources)
	}
}

// CGH FN-01 BR-04: chỉ đánh dấu "Chính thức" khi mọi tham số của ảnh chụp đã phê duyệt.
func TestOfficialOnlyWhenAllParamsApproved(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)

	id, raw := f.submitAndSnapshot(c, scID, nil)
	must(t, f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, sha(raw), 300000, 10, false)), 201, "nộp")
	r := c.post("/v1/simulations/"+id+"/mark-official", map[string]any{})
	if r.status != 409 || str(r.body, "code") != "simulation.params_unapproved" {
		t.Fatalf("thiếu phê duyệt phải bị chặn: %d %s", r.status, r.raw)
	}
	if list, _ := r.body["unapproved_parameters"].([]any); len(list) == 0 {
		t.Fatalf("phải liệt kê tham số chưa phê duyệt: %s", r.raw)
	}

	f.params.SetApproveAll(true)
	id2, raw2 := f.submitAndSnapshot(c, scID, nil)
	must(t, f.internalPost(f.tenant, id2, simtest.ResultEnvelope(id2, sha(raw2), 300000, 10, false)), 201, "nộp")
	ok := must(t, c.post("/v1/simulations/"+id2+"/mark-official", map[string]any{}), 200, "đánh dấu chính thức")
	if ok.body["official"] != true {
		t.Fatalf("official = %v", ok.body["official"])
	}
	res := must(t, c.get("/v1/simulations/"+id2+"/result"), 200, "kết quả")
	if str(res.body, "status") != "chinh_thuc" {
		t.Fatalf("status = %v", res.body["status"])
	}
	// Đánh dấu lặp idempotent.
	must(t, c.post("/v1/simulations/"+id2+"/mark-official", map[string]any{}), 200, "đánh dấu lặp")
}

// R-51: gửi lặp cùng Idempotency-Key → cùng việc mô phỏng, một sự kiện.
func TestSubmitIdempotentReplay(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	k := key()
	a := must(t, c.postKey("/v1/simulations", k, map[string]any{"scenario_id": scID}), 202, "lần 1")
	b := must(t, c.postKey("/v1/simulations", k, map[string]any{"scenario_id": scID}), 202, "lần 2")
	if str(a.body, "id") != str(b.body, "id") || b.header.Get(idempotency.ReplayedHeader) == "" {
		t.Fatalf("gọi lặp phải trả đúng phản hồi đầu: %s / %s", a.raw, b.raw)
	}
	if n := len(f.events(f.tenant, port.EventRunSubmitted)); n != 1 {
		t.Fatalf("muốn 1 run.submitted, có %d", n)
	}
}

// RB-2: tenant khác không thấy kịch bản, mô phỏng, kết quả — cả qua Internal API.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	id, raw := f.submitAndSnapshot(c, scID, nil)
	must(t, f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, sha(raw), 300000, 10, false)), 201, "nộp")
	other := f.asTenant(ids.New(), port.RoleSimulationAnalyst)
	for _, p := range []string{"/v1/scenarios/" + scID, "/v1/simulations/" + id, "/v1/simulations/" + id + "/result", "/v1/jobs/" + id} {
		if r := other.get(p); r.status != 404 {
			t.Fatalf("%s từ tenant khác: %d", p, r.status)
		}
	}
	if r := f.internalGet(other.tenant, id); r.status != 404 {
		t.Fatalf("Internal API tenant khác: %d", r.status)
	}
	if r := other.post("/v1/simulations", map[string]any{"scenario_id": scID}); r.status != 404 {
		t.Fatalf("gửi mô phỏng kịch bản của tenant khác: %d", r.status)
	}
	cmp := must(t, other.post("/v1/scenarios:compare", map[string]any{"simulation_ids": []string{id}}), 200, "so sánh")
	if rows := cmp.body["rows"].([]any); len(rows) != 0 {
		t.Fatalf("tenant khác thấy kết quả: %v", rows)
	}
	// Không có vai → 403.
	if r := f.asTenant(f.tenant, "kn_officer").get("/v1/simulations/" + id); r.status != 403 {
		t.Fatalf("thiếu vai simulation_analyst: %d", r.status)
	}
}

// Mục 12/R-61: nộp kết quả lặp → 201 rồi 200 (không ghi thêm); sai ảnh chụp → 409; phong bì hỏng → 422.
func TestResultPostingIsIdempotent(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	id, raw := f.submitAndSnapshot(c, scID, nil)
	if r := f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, strings.Repeat("0", 64), 300000, 10, false)); r.status != 409 ||
		str(r.body, "code") != "simulation.snapshot_mismatch" {
		t.Fatalf("sai băm ảnh chụp: %d %s", r.status, r.raw)
	}
	if r := f.internalPost(f.tenant, id, []byte(`{"schema_version":1,"status":"completed"}`)); r.status != 422 {
		t.Fatalf("phong bì hỏng: %d %s", r.status, r.raw)
	}
	body := simtest.ResultEnvelope(id, sha(raw), 300000, 10, false)
	first := must(t, f.internalPost(f.tenant, id, body), 201, "lần 1")
	second := must(t, f.internalPost(f.tenant, id, body), 200, "lần 2")
	if first.body["replayed"] != false || second.body["replayed"] != true {
		t.Fatalf("replayed: %v / %v", first.body, second.body)
	}
	// Kết quả khác (vd optimizer chạy lại) cũng không ghi đè bản đã lưu.
	must(t, f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, sha(raw), 999999, 10, false)), 200, "lần 3")
	res := must(t, c.get("/v1/simulations/"+id+"/result"), 200, "kết quả")
	if str(res.body, "result_sha256") != sha(body) {
		t.Fatal("kết quả đã lưu bị thay")
	}
	if n := len(f.events(f.tenant, port.EventJobCompleted)); n != 1 {
		t.Fatalf("muốn 1 job.completed, có %d", n)
	}
	var cnt int
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT count(*) FROM pf_simulation.result WHERE simulation_id = $1`, id).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("số kết quả = %d (%v)", cnt, err)
	}
}

// Optimizer báo lỗi → việc hỏng, job.completed(failed); nộp lại bất kỳ → 200 trạng thái đã lưu.
func TestOptimizerFailureIsRecorded(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	id, raw := f.submitAndSnapshot(c, scID, nil)
	must(t, f.internalPost(f.tenant, id, simtest.FailedEnvelope(id, sha(raw))), 201, "nộp lỗi")
	s := must(t, c.get("/v1/simulations/"+id), 200, "đọc")
	if str(s.body, "status") != "failed" || str(s.body, "error_code") != "optimizer.snapshot_incomplete" {
		t.Fatalf("mô phỏng: %s", s.raw)
	}
	r := must(t, f.internalPost(f.tenant, id, simtest.ResultEnvelope(id, sha(raw), 1, 10, false)), 200, "nộp sau khi hỏng")
	if str(r.body, "status") != "failed" {
		t.Fatalf("trạng thái đã lưu phải giữ nguyên: %s", r.raw)
	}
}

// Thiếu nguồn cung (pf/cropcycle chưa nối) → việc hỏng simulation.dependency_unavailable, có job.completed(failed).
func TestMissingSupplySourceFailsJob(t *testing.T) {
	f := newFixture(t)
	f.mod = simulation.New(f.pool, clock.NewFixed(t0), simulation.Deps{})
	c := f.analyst()
	scID, _ := f.scenario(c, 10, false)
	r := must(t, c.post("/v1/simulations", map[string]any{"scenario_id": scID}), 202, "gửi")
	f.runWorker(f.tenant)
	var status, code string
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT status, error_code FROM pf_simulation.simulation WHERE id = $1`,
		str(r.body, "id")).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "simulation.dependency_unavailable" {
		t.Fatalf("%s %s", status, code)
	}
	if n := len(f.events(f.tenant, port.EventJobCompleted)); n != 1 {
		t.Fatalf("muốn 1 job.completed(failed), có %d", n)
	}
}

// CGH FN-14: bảng so sánh xếp theo TCO/Ton tăng dần, cột No-Hub Baseline ghim cuối (BR-03, AC-04);
// khác vòng đời → chặn (BR-02); mô phỏng chưa có kết quả bị loại kèm cảnh báo (BR-01).
func TestCompareScenarios(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scA, _ := f.scenario(c, 10, false)
	scB, _ := f.scenario(c, 10, false)
	scC, _ := f.scenario(c, 12, false)
	idA, rawA := f.submitAndSnapshot(c, scA, nil)
	idB, rawB := f.submitAndSnapshot(c, scB, nil)
	idC, rawC := f.submitAndSnapshot(c, scC, nil)
	must(t, f.internalPost(f.tenant, idA, simtest.ResultEnvelope(idA, sha(rawA), 420000, 10, false)), 201, "A")
	must(t, f.internalPost(f.tenant, idB, simtest.ResultEnvelope(idB, sha(rawB), 310000, 10, false)), 201, "B")
	must(t, f.internalPost(f.tenant, idC, simtest.ResultEnvelope(idC, sha(rawC), 200000, 12, false)), 201, "C")
	pending := must(t, c.post("/v1/simulations", map[string]any{"scenario_id": scA}), 202, "chưa tính")

	cmp := must(t, c.post("/v1/scenarios:compare", map[string]any{"simulation_ids": []string{idA, idB, str(pending.body, "id")}}), 200, "so sánh")
	rows := cmp.body["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("muốn A, B + cột baseline, có %d: %s", len(rows), cmp.raw)
	}
	first, second, last := rows[0].(map[string]any), rows[1].(map[string]any), rows[2].(map[string]any)
	if str(first, "simulation_id") != idB || str(second, "simulation_id") != idA || last["is_baseline_column"] != true || str(last, "capex") != "0" {
		t.Fatalf("thứ tự/baseline sai: %s", cmp.raw)
	}
	if ws := cmp.body["warnings"].([]any); len(ws) < 2 {
		t.Fatalf("thiếu cảnh báo (chưa có kết quả, cột baseline): %v", ws)
	}
	blocked := must(t, c.post("/v1/scenarios:compare", map[string]any{"simulation_ids": []string{idA, idC}}), 200, "khác vòng đời")
	if b, _ := blocked.body["blocked"].(string); !strings.Contains(b, "vòng đời") {
		t.Fatalf("phải chặn so sánh khác vòng đời: %s", blocked.raw)
	}
}

// RB-1: gỡ Hub là bản ghi bù (supersedes_id + lý do); bảng chiếu giữ dòng, sổ giữ lịch sử;
// sửa một ô cấu hình không reset các ô khác (prototype attachHub).
func TestDetachHubIsCompensatingRecord(t *testing.T) {
	f := newFixture(t)
	c := f.analyst()
	scID, hubID := f.scenario(c, 10, false)
	upd := must(t, c.post("/v1/scenarios/"+scID+"/hubs", map[string]any{"hub_id": hubID, "barge_payload_tons": "2000"}), 200, "sửa sà lan")
	h := upd.body["hubs"].([]any)[0].(map[string]any)
	if str(h, "barge_payload_tons") != "2000" || str(h, "service_radius_km") != "25" || str(h, "design_capacity_tons") != "30000" {
		t.Fatalf("sửa một ô làm đổi ô khác: %v", h)
	}
	if r := c.post("/v1/scenarios/"+scID+"/hubs/"+hubID+"/detach", map[string]any{"reason": "ngắn"}); r.status != 422 {
		t.Fatalf("lý do quá ngắn: %d", r.status)
	}
	det := must(t, c.post("/v1/scenarios/"+scID+"/hubs/"+hubID+"/detach", map[string]any{"reason": "Vị trí ngập lũ, loại khỏi phương án"}), 200, "gỡ Hub")
	if n := len(det.body["hubs"].([]any)); n != 0 {
		t.Fatalf("còn %d Hub", n)
	}
	hist := must(t, c.get("/v1/scenarios/"+scID+"/history"), 200, "lịch sử").body["items"].([]any)
	var attachID string
	for _, it := range hist {
		m := it.(map[string]any)
		switch m["kind"] {
		case "hub_updated":
			attachID = str(m, "id")
		case "hub_detached":
			if str(m, "supersedes_id") != attachID || str(m, "correction_reason") == "" {
				t.Fatalf("bản ghi bù phải trỏ bản ghi trước và có lý do: %v", m)
			}
		}
	}
	if len(hist) != 4 {
		t.Fatalf("sổ phải giữ đủ 4 dòng (created, attached, updated, detached), có %d", len(hist))
	}
	if r := c.post("/v1/simulations", map[string]any{"scenario_id": scID}); r.status != 422 || str(r.body, "code") != "simulation.scenario_without_hubs" {
		t.Fatalf("kịch bản không còn Hub: %d %s", r.status, r.raw)
	}
	// Ngừng dùng Hub ứng viên: không xóa, có lý do; không gán lại được.
	must(t, c.post("/v1/candidate-hubs/"+hubID+"/retire", map[string]any{"reason": "Không mua được đất tại vị trí này"}), 200, "ngừng dùng")
	if r := c.post("/v1/scenarios/"+scID+"/hubs", map[string]any{"hub_id": hubID}); r.status != 409 {
		t.Fatalf("gán Hub đã ngừng dùng: %d", r.status)
	}
}

// Mục 11: Internal API mặc định đòi mTLS với chứng thư của dịch vụ được phép.
func TestInternalAPIRequiresMTLS(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/internal/v1/simulations/x/snapshot", nil)
	if err := handler.MTLSAuthorizer([]string{"optimizer"})(r); err == nil {
		t.Fatal("request không TLS phải bị từ chối")
	}
}
