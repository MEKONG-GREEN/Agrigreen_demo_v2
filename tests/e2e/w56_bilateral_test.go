package e2e

// W5.6 (ADR-0034, kiến trúc v4.1 §38.2) đầu–cuối trong tiến trình, cả hai vùng: phía MKG là module mkg/fieldops THẬT
// (cụm mkg, bộ điều hợp GatewayBilateral qua HTTP), phía nền tảng là cổng tích hợp THẬT (assembly.Build + router
// Public của pf/apiclient, hàng đợi trong tiến trình) + pf/cooperative (App HTX) trên cụm pf.
//
//	van valve_export (MKG) → job fieldops_bilateral: chiếu + POST /v1/batches → sổ song phương pf_coop
//	→ HTX thấy (và HTX khác không) → HTX xác nhận / phản đối → job dựng feed cặp → MKG kéo GET /v1/feeds/bilateral
//	→ tổng kiểm ngày bilateral_day.v1: bản sao MKG = cổng (cả hai phía); chạy lại job là no-op (idempotent)
//	→ HTX chấm dứt cặp → MKG ghi nhận cặp chấm dứt, bản ghi mới bị từ chối integration.pair_missing.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	publicasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type w56 struct {
	t        *testing.T
	pg       *pgtest.Env
	iss      *authztest.FakeIssuer
	operator uuid.UUID
	owner    uuid.UUID // tenant chủ của HTX
	mkg      uuid.UUID // tenant MKG
	gw       *httptest.Server
	admin    *httptest.Server
	mod      *apiclient.Module
	deps     apiclient.PublicDeps
	queue    *apiclient.IngestMemoryQueue
	coop     *cooperative.Module
}

func (w *w56) adminCall(roles []string, method, path string, body any) map[string]any {
	w.t.Helper()
	tok := w.iss.Sign(w.t, authztest.Token{Subject: ids.New(), TenantID: w.operator, Roles: roles, MFA: true})
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, w.admin.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", ids.New().String())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		w.t.Fatalf("Admin %s %s: %d %s", method, path, res.StatusCode, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// onboard: khách hàng API tenant MKG với scope luồng song phương (bốn mắt) + gói + khóa.
func (w *w56) onboard() string {
	ad, ap := []string{apiclientport.RoleAPIClientAdmin}, []string{apiclientport.RoleAPIClientApprover}
	c := w.adminCall(ad, http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": w.mkg.String(), "display_name": "MKG (thử W5.6)",
		"purpose": "Luồng song phương MKG ↔ HTX", "contact_ref": ids.New().String()})
	id := c["id"].(string)
	w.adminCall(ap, http.MethodPost, "/v1/api-clients/"+id+"/approve", nil)
	for product, scope := range map[string]string{"collection_schedule": apiclientport.IngestScope("collection_schedule"),
		"roll_count_statement": apiclientport.IngestScope("roll_count_statement"), "htx_roll_confirmation": apiclientport.FeedScope("htx_roll_confirmation")} {
		s := w.adminCall(ad, http.MethodPost, "/v1/api-clients/"+id+"/subscriptions", map[string]any{"product": product, "scopes": []string{scope}})
		w.adminCall(ap, http.MethodPost, "/v1/api-clients/"+id+"/subscriptions/"+s["id"].(string)+"/approve", nil)
	}
	p := w.adminCall(ad, http.MethodPost, "/v1/api-clients/"+id+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	w.adminCall(ap, http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", id, p["version"]), nil)
	k := w.adminCall(ad, http.MethodPost, "/v1/api-clients/"+id+"/keys", nil)
	return k["api_key"].(string)
}

func (w *w56) seedHTX() uuid.UUID {
	htx := ids.New()
	if err := tenant.WithTenantTx(context.Background(), w.pg.Pool(w.t, migrations.PF, dbadmin.Migrator), w.owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
		  VALUES ($1, $2, 'cooperative', $3, 'HTX W5.6', '89123', true, $4, 1)`, w.owner, htx, "E56-"+htx.String()[24:], ids.New()); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2)`, w.owner, htx)
		return err
	}); err != nil {
		w.t.Fatalf("seed HTX: %v", err)
	}
	return htx
}

func (w *w56) htx(node uuid.UUID) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: w.owner, OrgRef: node,
		Roles: []string{partyport.RoleCoopManager}})
}

// valve ghi một bản ghi van đã xuất ở cụm mkg (như fieldops.export — sổ valve_export).
func (w *w56) valve(contract string, subject, order uuid.UUID, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	ev := "mkg.fieldops." + contract + ".published"
	if err := tenant.WithTenantTx(context.Background(), w.pg.Pool(w.t, migrations.MKG, dbadmin.Migrator), w.mkg, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO mkg_fieldops.valve_export (id, tenant_id, stream_key, contract, contract_version, event_type,
		    schema_version, subject_ref, order_id, payload, payload_sha256, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, 1, $5, 1, $6, $7, $8::jsonb, $9, now(), $10, $11, 'test')`,
			ids.New(), w.mkg, "valve:"+contract+":"+subject.String(), contract, ev, subject, order, string(raw), sum[:], ids.New(), "w56:"+ids.New().String())
		return err
	}); err != nil {
		w.t.Fatalf("van: %v", err)
	}
}

func TestBilateralMKGToHTX_W56(t *testing.T) {
	pg := pgtest.Get(t)
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("p", apiclientport.MinPepperBytes))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	w := &w56{t: t, pg: pg, iss: authztest.New(t), operator: ids.New(), owner: ids.New(), mkg: ids.New()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	clk := clock.System{}
	w.queue = apiclient.NewIngestMemoryQueue(0)
	w.queue.Backoff = nil
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return pg.Pool(t, migrations.PF, role), nil
	}
	w.mod, w.deps, err = publicasm.Build(context.Background(), open, publicasm.Config{Operator: w.operator, Peppers: peppers, Replicas: 1,
		EgressMode: meter.ModeEnforce, Clock: clk, Log: log, IngestQueue: w.queue, IngestAllowProposed: true, AllowUnapproved: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	w.mod.UseBilateral(w.deps.Bilateral)
	w.gw = httptest.NewServer(w.mod.PublicRouter(w.deps))
	t.Cleanup(w.gw.Close)
	adm := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	w.mod.RegisterAdmin(adm.Group("/v1", httpx.Authenticate(w.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pg.Pool(t, migrations.PF, "platform_api"))))
	w.admin = httptest.NewServer(adm)
	t.Cleanup(w.admin.Close)
	w.coop = cooperative.New(pg.Pool(t, migrations.PF, dbadmin.AppPlatform), clk, cooperative.Deps{})
	w.coop.UseBilateral(false, true)

	key := w.onboard()
	mkgMod := fieldops.New(pg.Pool(t, migrations.MKG, dbadmin.AppInternal), clk, fieldops.Options{
		Bilateral: fieldops.NewGatewayBilateral(fieldops.GatewayConfig{BaseURL: w.gw.URL, APIKey: key}, clk)})
	ctx := context.Background()
	drain := func() { w.queue.Drain(ctx, w.deps.Ingest.Worker()) }
	sync := func(what string) fieldops.BilateralSync {
		t.Helper()
		r, err := mkgMod.SyncBilateral(ctx, w.mkg)
		if err != nil {
			t.Fatalf("%s: SyncBilateral: %v (%+v)", what, err, r)
		}
		return r
	}

	htx1, htx2 := w.seedHTX(), w.seedHTX()
	pair, err := w.coop.Bilateral().EstablishPair(w.htx(htx1), w.mkg)
	if err != nil {
		t.Fatal(err)
	}
	order, st1, st2 := ids.New(), ids.New(), ids.New()
	w.valve("collection_schedule", order, order, map[string]any{"schedule_ref": order.String(), "htx_ref": htx1.String(), "commune_code": "89123",
		"window_from": "2026-10-10", "window_to": "2026-10-12", "status": "confirmed", "revision": 2})
	for _, s := range []struct {
		ref   uuid.UUID
		rolls int
	}{{st1, 46}, {st2, 12}} {
		w.valve("htx_collection_statement", s.ref, order, map[string]any{"statement_ref": s.ref.String(), "htx_ref": htx1.String(),
			"plot_ref": ids.New().String(), "season_ref": nil, "collected_on": "2026-10-09", "rolls": s.rolls, "revision": 1})
	}
	// Bảng kê của HTX2 (chưa lập cặp với MKG) → từ chối integration.pair_missing.
	st3 := ids.New()
	w.valve("htx_collection_statement", st3, order, map[string]any{"statement_ref": st3.String(), "htx_ref": htx2.String(),
		"plot_ref": ids.New().String(), "season_ref": nil, "collected_on": "2026-10-09", "rolls": 7, "revision": 1})

	// Lượt 1: chiếu + gửi; cổng xử lý lô; lượt 2: nhận kết quả.
	if r := sync("lượt 1"); r.Staged != 4 || r.Batches != 2 {
		t.Fatalf("lượt 1: chiếu 4 bản ghi, gửi 2 lô (theo hợp đồng): %+v", r)
	}
	drain()
	if r := sync("lượt 2"); r.Accepted != 3 || r.Rejected != 1 || r.Pending != 0 || r.Submitted != 0 {
		t.Fatalf("lượt 2: 3 accepted + 1 rejected (pair_missing): %+v", r)
	}
	// HTX1 thấy 3 bản ghi của MKG; HTX2 không có cặp.
	recs, err := w.coop.Bilateral().Records(w.htx(htx1), pair.PairID, 0, 100)
	if err != nil || len(recs) != 3 {
		t.Fatalf("HTX1 thấy bản ghi của MKG: %d %v", len(recs), err)
	}
	if ps, err := w.coop.Bilateral().Pairs(w.htx(htx2)); err != nil || len(ps) != 0 {
		t.Fatalf("HTX2 không có cặp: %v %v", ps, err)
	}
	// HTX1 xác nhận st1, phản đối st2.
	conf := func(st uuid.UUID, mkgRolls int, outcome, htxRolls, reason string) {
		raw := fmt.Sprintf(`{"confirmation_ref":%q,"statement_ref":%q,"statement_revision":1,"htx_ref":%q,"plot_ref":%q,"mkg_rolls":%d,`+
			`"outcome":%q,"htx_rolls":%s,"reason":%s,"responded_at":"2026-10-09T17:00:00+07:00","supersedes_confirmation_ref":null}`,
			ids.New(), st, htx1, ids.New(), mkgRolls, outcome, htxRolls, reason)
		if _, _, err := w.coop.Bilateral().Confirm(w.htx(htx1), []byte(raw)); err != nil {
			t.Fatalf("HTX %s: %v", outcome, err)
		}
	}
	conf(st1, 46, "confirmed", "null", "null")
	conf(st2, 12, "disputed", "10", `"Đếm lại tại bãi còn 10 cuộn"`)
	// Job dựng feed cặp (cmd/worker) rồi MKG kéo feed.
	if _, err := w.mod.Service().BuildBilateral(ctx); err != nil {
		t.Fatal(err)
	}
	// htx2 (chưa từng có cặp → 403 pair_missing) đã ghi trạng thái "không có cặp" từ lượt 1.
	if r := sync("lượt 3"); r.Items != 5 || r.Pairs != 2 || r.Terminated != 0 {
		t.Fatalf("lượt 3: kéo 5 mục của cặp HTX1: %+v", r)
	}
	// Đối soát: tổng kiểm ngày bản sao MKG = cổng, cả hai phía; không bản ghi accepted nào thiếu trong feed.
	today := clock.BusinessDate(time.Now())
	from := clock.BusinessDate(clock.StartOfBusinessDate(today).Add(-24 * time.Hour))
	c, err := mkgMod.CompareBilateral(ctx, w.mkg, from, today)
	if err != nil || len(c.Mismatches) != 0 || c.NotInFeed != 0 || c.Days != 6 || c.Matched != 6 {
		t.Fatalf("đối soát tổng kiểm ngày: %+v %v", c, err)
	}
	// Idempotent: chạy lại không gửi / không áp gì mới.
	if r := sync("lượt 4"); r.Staged != 0 || r.Batches != 0 || r.Items != 0 {
		t.Fatalf("lượt lặp phải là no-op: %+v", r)
	}
	// HTX1 chấm dứt cặp → MKG ghi nhận; bản kê mới bị từ chối.
	if _, err := w.coop.Bilateral().TerminatePair(w.htx(htx1), pair.PairID, "Kết thúc hợp đồng thu mua vụ"); err != nil {
		t.Fatal(err)
	}
	st4 := ids.New()
	w.valve("htx_collection_statement", st4, order, map[string]any{"statement_ref": st4.String(), "htx_ref": htx1.String(),
		"plot_ref": ids.New().String(), "season_ref": nil, "collected_on": "2026-10-09", "rolls": 3, "revision": 1})
	if r := sync("lượt 5"); r.Terminated != 1 || r.Staged != 1 {
		t.Fatalf("lượt 5: cặp HTX1 ghi chấm dứt (feed 403), bản kê mới đã chiếu: %+v", r)
	}
	drain()
	if r := sync("lượt 6"); r.Rejected != 1 {
		t.Fatalf("sau chấm dứt: bản kê mới rejected + cặp ghi chấm dứt: %+v", r)
	}
	if c, err := mkgMod.CompareBilateral(ctx, w.mkg, from, today); err != nil || len(c.Mismatches) != 0 {
		t.Fatalf("lịch sử vẫn đối soát khớp sau chấm dứt: %+v %v", c, err)
	}
}
