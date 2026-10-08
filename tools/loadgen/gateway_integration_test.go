package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// TestMain dựng database kiểm thử (pgtest) chỉ khi không -short: kiểm thử đơn vị của loadgen không cần PostgreSQL.
func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	os.Exit(pgtest.Run(m))
}

// synthHarvest là nguồn ô THÔ tổng hợp của harvest_progress_by_commune: communes xã của tỉnh 92, mỗi xã 12 nông
// hộ (≥ k mặc định 10 → công bố). Dữ liệu giả, không nối pf/cropcycle (kiểm thử tải đo cổng, không đo job dựng).
type synthHarvest struct{ communes int }

func (s synthHarvest) AggregateCells(_ context.Context, dataset string, _ uuid.UUID, _ time.Time) ([]apiclientport.AggregateCell, error) {
	if dataset != apiclientport.FeedHarvestProgress {
		return nil, nil
	}
	out := make([]apiclientport.AggregateCell, 0, s.communes)
	for i := range s.communes {
		c := apiclientport.AggregateCell{ProvinceCode: "92", ProvinceValidFrom: "2025-07-01", CommuneCode: fmt.Sprintf("31%03d", i),
			AdminUnitValidFrom: "2025-07-01", SeasonCode: "2026-HT", SeasonYear: 2026, DateFrom: "2026-10-10", DateTo: "2026-10-25",
			Measures: map[string]decimal.Decimal{apiclientport.MeasureAreaSown: decimal.NewFromInt(120), apiclientport.MeasureAreaHarvested: decimal.NewFromInt(int64(i % 100))}}
		for u := range 12 {
			c.Units = append(c.Units, fmt.Sprintf("hh-%d-%d", i, u))
		}
		out = append(out, c)
	}
	return out, nil
}

// gwHarness là cổng tích hợp THẬT trong tiến trình (assembly.Build + gateway.NewHandler, như cmd/integration-gateway)
// trên PostgreSQL cục bộ, với nạp theo lô + kênh tệp (hàng đợi trong tiến trình, bộ xử lý chạy nền như tiến trình
// cổng) và Admin API của pf/apiclient để cấp khóa.
type gwHarness struct {
	public, admin *httptest.Server
	adminTok      string
	approverTok   string
}

func newGWHarness(t *testing.T, communes int) *gwHarness {
	t.Helper()
	pg := pgtest.Get(t)
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("p", apiclientport.MinPepperBytes))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	store, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	operator, grantor := ids.New(), ids.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := apiclient.NewIngestMemoryQueue(0)
	q.Backoff = nil
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return pg.Pool(t, migrations.PF, role), nil
	}
	mod, d, err := assembly.Build(context.Background(), open, assembly.Config{Operator: operator, Peppers: peppers, Store: store, Replicas: 1,
		EgressMode: meter.ModeEnforce, Clock: clock.System{}, Log: log, IngestQueue: q, IngestAllowProposed: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Hợp đồng feed/nạp còn proposed (A7, C-12): ngoài production cổng phục vụ khi bật cờ (AG_GATEWAY_FEED_ALLOW_PROPOSED).
	d.AllowUnapproved = true
	mod.UseFeedSources(synthHarvest{communes: communes}, apiclientport.StaticContributors{grantor}, nil)
	res, err := mod.BuildFeeds(context.Background())
	built := false
	for _, r := range res {
		built = built || r.Dataset == apiclientport.FeedHarvestProgress
	}
	if !built {
		t.Fatalf("dựng feed %s: %v", apiclientport.FeedHarvestProgress, err)
	}
	// Bộ xử lý hàng đợi đối tác chạy nền (trong tiến trình cổng — mỗi đối tác một goroutine, vách ngăn).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = q.Run(ctx, d.Ingest.Worker()) }()
	t.Cleanup(func() { cancel(); <-done })

	h := &gwHarness{public: httptest.NewServer(gateway.NewHandler(mod, d, nil, log))}
	t.Cleanup(h.public.Close)
	iss := authztest.New(t)
	adm := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	g := adm.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pg.Pool(t, migrations.PF, "platform_api")))
	mod.RegisterAdmin(g)
	h.admin = httptest.NewServer(adm)
	t.Cleanup(h.admin.Close)
	h.adminTok = iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: operator, Roles: []string{apiclientport.RoleAPIClientAdmin}, MFA: true})
	h.approverTok = iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: operator, Roles: []string{apiclientport.RoleAPIClientApprover}, MFA: true})
	return h
}

func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("%s: số nguyên ≥ 1", name)
		}
		return n
	}
	return def
}

func envFloat(t *testing.T, name string, def float64) float64 {
	t.Helper()
	if v := os.Getenv(name); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			t.Fatalf("%s: số ≥ 0", name)
		}
		return f
	}
	return def
}

func envDur(t *testing.T, name string, def time.Duration) time.Duration {
	t.Helper()
	if v := os.Getenv(name); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return d
	}
	return def
}

// W5.9 (§42, ADR-0033): kịch bản cổng tích hợp ở quy mô nhỏ trên cổng THẬT trong tiến trình + PostgreSQL cục bộ.
// Mặc định 20 đối tác (một ồn) × 0,5 lượt/giây, hạn mức 60 lượt/phút, đối tác ồn gửi 10 lượt/giây (10 × hạn mức);
// khởi động 10 giây rồi 2 vòng × (nền 15 giây + ồn 15 giây). Ở quy mô nhỏ lượt nền có đối tác ồn dùng ĐÚNG hạn mức
// (1 lượt/giây, AG_LOADGEN_GW_BASELINE_NOISY_FACTOR=1): tải hợp lệ của một đối tác là ~10% tổng tải của 20 đối tác,
// nên phép so chỉ đo ảnh hưởng của phần VƯỢT hạn mức; lượt đo chính thức 200 đối tác dùng 0 (vắng mặt, đúng chữ §42).
// Chỉ tiêu chí cách ly hỏng → đo lại một lần. Khẳng định:
//   - đối tác ồn nhận 429 apiclient.rate_limited + Retry-After, không lỗi nào khác;
//   - đối tác thường không nhận 429, tỷ lệ lỗi ≤ 1%;
//   - p95 của đối tác thường khi có đối tác ồn ≤ p95 nền × 1,10 + dung sai tuyệt đối (mặc định 5 ms — đo cục bộ trên
//     DB dùng chung, p95 vài chục mili giây, nhiễu lịch CPU/đồng hồ Windows ~1–2 ms; lượt đo chính thức dùng dung sai 0).
//
// Quy mô/thời lượng chỉnh qua AG_LOADGEN_GW_PARTNERS, _RPS, _QUOTA_RPM, _PHASE, _ROUNDS, _WARMUP, _SLACK, _MAX_INCREASE,
// _BASELINE_NOISY_FACTOR;
// AG_LOADGEN_GW_REPORT=<thư mục> ghi w59-report.json + w59-report.md. Bỏ qua dưới -short.
func TestGatewayNoisyPartnerIsolation_W59(t *testing.T) {
	if testing.Short() {
		t.Skip("kiểm thử tải cổng (W5.9) — bỏ qua dưới -short")
	}
	partners := envInt(t, "AG_LOADGEN_GW_PARTNERS", 20)
	quota := int64(envInt(t, "AG_LOADGEN_GW_QUOTA_RPM", 60))
	h := newGWHarness(t, 30)
	ctx := context.Background()
	start := time.Now()
	ps, err := Provision(ctx, GWProvision{Admin: h.admin.URL, AdminToken: h.adminTok, ApproverToken: h.approverTok, Partners: partners, Noisy: 1,
		Feed: apiclientport.FeedHarvestProgress, Contract: defaultGWContract, QuotaRPM: quota, DailyUnits: 1_000_000, Label: "loadgen-w59-test"})
	if err != nil {
		t.Fatalf("cấp đối tác qua Admin API: %v", err)
	}
	t.Logf("đã cấp %d đối tác qua Admin API trong %s", len(ps), time.Since(start).Round(time.Millisecond))
	mix, _ := ParseMix(defaultGWMix)
	g, err := NewGateway(GWConfig{Base: h.public.URL, Feed: apiclientport.FeedHarvestProgress, Contract: defaultGWContract,
		PartnerRPS: envFloat(t, "AG_LOADGEN_GW_RPS", 0.5), QuotaRPM: quota, NoisyFactor: 10, Mix: mix, BatchItems: 20, FileRecords: 50,
		MaxInFlight: 16, Timeout: 30 * time.Second}, ps)
	if err != nil {
		t.Fatal(err)
	}
	phase := envDur(t, "AG_LOADGEN_GW_PHASE", 15*time.Second)
	run := GWRun{Warmup: envDur(t, "AG_LOADGEN_GW_WARMUP", 10*time.Second), Baseline: phase, Noisy: phase,
		Rounds: envInt(t, "AG_LOADGEN_GW_ROUNDS", 2), BaselineNoisyFactor: envFloat(t, "AG_LOADGEN_GW_BASELINE_NOISY_FACTOR", 1),
		MaxIncrease: envFloat(t, "AG_LOADGEN_GW_MAX_INCREASE", 0.10), Slack: envDur(t, "AG_LOADGEN_GW_SLACK", 5*time.Millisecond), MaxErrRate: 0.01}
	rep := g.Compare(ctx, run)
	if rep.Isolation != nil && !rep.Isolation.Pass && len(rep.Violations) == 1 {
		// Chỉ tiêu chí cách ly hỏng: đo lại MỘT lần (DB cục bộ dùng chung với tiến trình khác — nhiễu p95 giữa hai lượt
		// ngắn có thể vượt 10%). Hỏng hai lần liên tiếp mới là hồi quy.
		t.Logf("cách ly không đạt ở lần đo đầu, đo lại:\n%s", rep.Markdown())
		rep = g.Compare(ctx, run)
	}
	md := rep.Markdown()
	t.Log("\n" + md)
	if dir := os.Getenv("AG_LOADGEN_GW_REPORT"); dir != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "w59-report.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "w59-report.md"), []byte(md), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rep.Isolation == nil {
		t.Fatal("thiếu kết quả so sánh cách ly")
	}
	for _, v := range rep.Violations {
		t.Error(v)
	}
	noisy := rep.Phases[len(rep.Phases)-1]
	if s, ok := findSummary(noisy, ClassNoisy); !ok || s.ByStatus[strconv.Itoa(http.StatusTooManyRequests)] == 0 {
		t.Errorf("đối tác ồn phải nhận 429: %+v", s)
	}
}
