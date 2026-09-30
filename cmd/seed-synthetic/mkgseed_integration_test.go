package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internalapi "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fakePlatform là bề mặt đối tác giả của nền tảng cho nguồn thửa/HTX của fieldops (ADR-0020:
// cấp quyền chia sẻ coi như đã duyệt) — token client credentials + /v1/partner/cooperatives,
// /v1/partner/plots.
func fakePlatform(t *testing.T, plan MKGPlan) *httptest.Server {
	plots := map[string]string{}
	for _, l := range plan.Lots {
		for _, p := range l.Plots {
			plots[p.Ref.String()] = l.HTXRef.String()
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sa", "expires_in": 3600})
	})
	mux.HandleFunc("GET /v1/partner/cooperatives/{ref}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": r.PathValue("ref"), "commune_code": "31117", "active": true})
	})
	mux.HandleFunc("GET /v1/partner/plots/{ref}", func(w http.ResponseWriter, r *http.Request) {
		htx, ok := plots[r.PathValue("ref")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": r.PathValue("ref"), "commune_code": "31117", "cooperative_ref": htx, "status": "active"})
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// clientCert sinh chứng thư mTLS client có SAN = hai mã thiết bị (ADR-0019: CN/SAN = device_id).
func clientCert(t *testing.T, names ...string) tls.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// R-95 (A8) đầu–cuối: kế hoạch MKG chạy qua API nội bộ MKG THẬT (cmd/api-internal/assembly:
// fieldops + measurement + hub, adapter mkgwire) và ingest THẬT qua mTLS (lô ký Ed25519 →
// ts_weigh → vòng tích phân → phiếu). Golden seed prototype: 4 400 cuộn × 20 = 88 t so với
// HUB_IN 96,8 t → +10 % gắn cờ (theo tấn); x % CUSTOMER_IN vượt dung sai → tranh chấp; lô
// DIRECT đọc số cuộn qua liên kết tay. Chạy lại cùng seed → không bản ghi mới (idempotent).
func TestMKGSeedEndToEnd(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	clk := clock.System{}
	today := time.Now().In(clock.Zone())
	const seed = 11
	plan := BuildMKGPlan(seed, mkgScales["small"], 10, today)
	pf := fakePlatform(t, plan)
	t.Setenv("AG_PLATFORM_API_URL", pf.URL)
	t.Setenv("AG_PLATFORM_TOKEN_URL", pf.URL+"/token")
	t.Setenv("AG_MKG_SA_CLIENT_ID", "mkg-sa")
	t.Setenv("AG_MKG_SA_CLIENT_SECRET", "dev-only")
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.MKG, role), nil
	}
	mods, ops, pools, err := internalapi.Build(ctx, open, clk)
	if err != nil {
		t.Fatal(err)
	}
	iss := authztest.New(t)
	log := telemetry.NewLogger("test", io.Discard)
	d := internalapi.Deps{Log: log, Verifier: iss.Verifier(), Ops: ops, Modules: mods.List(), Pools: pools}
	app, adm := httptest.NewServer(internalapi.NewRouter(d)), httptest.NewServer(internalapi.NewAdminRouter(d))
	t.Cleanup(app.Close)
	t.Cleanup(adm.Close)

	// Ingest: bộ nhận mTLS + kho ts + vòng tích phân (như cmd/ingest), vai ingest_mkg / ingest_ts.
	ingestPool := env.Pool(t, migrations.MKG, mport.IngestRole)
	ports := measurement.NewIngestPorts(ingestPool, clk)
	store := &measurement.IngestTSStore{Writer: env.Pool(t, migrations.TS, mport.IngestTSRole), Reader: env.Pool(t, migrations.TS, mport.TSReaderRole)}
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := measurement.NewIngestMetrics(nil)
	rv := &measurement.IngestReceiver{Devices: ports.Devices, Store: store, Clock: clk, Log: lg, Metrics: metrics}
	ing := httptest.NewUnstartedServer(rv.NewEngine())
	ing.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12}
	ing.StartTLS()
	t.Cleanup(ing.Close)
	cert := clientCert(t, plan.Devices[0].ID, plan.Devices[1].ID)
	roots := x509.NewCertPool()
	roots.AddCert(ing.Certificate())
	ingestHTTP := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{cert}, RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	loop := &measurement.IngestLoop{Sessions: ports.Sessions, Integrator: &measurement.IngestIntegrator{Windows: store, Tare: ports.Tare},
		Tickets: ports.Tickets, Clock: clk, Log: lg, Metrics: metrics}

	tid := ids.New()
	opTok := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, MFA: true,
		Roles: []string{"field_supervisor", "field_collector", "scale_supervisor", "reconciliation_officer"}})
	apTok := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, MFA: true, Roles: []string{"field_supervisor", "device_approver"}})
	cfg := MKGConfig{AppURL: app.URL, AdminURL: adm.URL, IngestURL: ing.URL, Operator: opTok, Approver: apTok, ClientCert: cert.Certificate[0],
		IngestHTTP: ingestHTTP, Seed: seed, Scale: "small", OverPct: 10, Env: "dev", Today: today, Wait: 5 * time.Second,
		AfterClose: func(ctx context.Context) error { _, err := loop.RunOnce(ctx); return err }}
	res, err := RunMKG(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Orders != 6 || res.Imports != 6 || res.Batches == 0 || res.Sessions == 0 {
		t.Fatalf("thống kê: %+v", res)
	}

	get := func(path string) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, app.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+opTok)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Body.Close() }()
		var m map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		_ = dec.Decode(&m)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d %v", path, r.StatusCode, m)
		}
		return m
	}
	latest := func(l MKGLot) map[string]any {
		return obj(get("/v1/measurement/lots/"+l.LotRef.String()+"/reconciliations"), "latest")
	}
	// Golden: phiếu HUB_IN tích phân từ luồng thô = 96 800; cuộn 4 400 × 20 = 88 000 → +10,00 % cờ.
	g := plan.Lots[0]
	tk := get("/v1/measurement/lots/" + g.LotRef.String() + "/tickets")
	var hubIn string
	for _, x := range tk["tickets"].([]any) {
		if m := x.(map[string]any); m["point"] == "HUB_IN" {
			hubIn = m["mass_kg"].(string)
		}
	}
	gr := latest(g)
	roll := obj(gr, "roll_check")
	if hubIn != "96800.000" || str(roll, "status") != "flag" || str(roll, "variance_pct") != "10.00" || str(roll, "estimate_kg") != "88000.000" ||
		str(gr, "pricing_basis") != "per_tonne" {
		t.Fatalf("golden: HUB_IN %s, đối soát %v", hubIn, gr)
	}
	if lk := get("/v1/measurement/lots/" + g.LotRef.String() + "/collection"); lk["explicit"] != true || str(lk, "collection_order_ref") != res.OrderIDs[0] {
		t.Fatalf("liên kết golden: %v", lk)
	}
	disputes, direct := 0, 0
	for _, l := range plan.Lots {
		r := latest(l)
		if l.OverTolerance {
			if str(r, "outcome") != mport.OutcomeDisputeOpened {
				t.Fatalf("lô vượt dung sai: %v", r)
			}
			disputes++
		}
		if l.Route == "DIRECT" {
			direct++
			if str(r, "outcome") != mport.OutcomeDirectAggregate || str(obj(r, "roll_check"), "status") == "pending_rolls" {
				t.Fatalf("lô DIRECT phải có số cuộn qua liên kết tay: %v", r)
			}
		}
		if l.Basis == "per_bale" && str(obj(r, "roll_check"), "status") != "informational" {
			t.Fatalf("theo cuộn → chỉ tham khảo: %v", r)
		}
	}
	if disputes != 1 || direct == 0 {
		t.Fatalf("tranh chấp %d, DIRECT %d", disputes, direct)
	}
	// Chạy lại cùng seed: cùng lệnh gom, không đối soát mới.
	again, err := RunMKG(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(again.OrderIDs, ",") != strings.Join(res.OrderIDs, ",") {
		t.Fatalf("chạy lại tạo lệnh gom mới: %v vs %v", again.OrderIDs, res.OrderIDs)
	}
	if n := len(get("/v1/measurement/lots/" + g.LotRef.String() + "/reconciliations")["items"].([]any)); n != 1 {
		t.Fatalf("chạy lại: %d lần đối soát", n)
	}
}
