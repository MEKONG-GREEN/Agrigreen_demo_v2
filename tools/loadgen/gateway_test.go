package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

func TestParseMixAndPartnersFile(t *testing.T) {
	m, err := ParseMix("feed=6,checksums=1,batch=2,file=0")
	if err != nil || m[gwOpFeed] != 6 || m[gwOpFile] != 0 {
		t.Fatalf("%v %v", m, err)
	}
	for _, bad := range []string{"bogus=1", "feed=-1", "feed=x"} {
		if _, err := ParseMix(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "p.tsv")
	ps := []*GWPartner{{ID: "a", Key: "k1", Class: ClassNormal}, {ID: "b", Key: "k2", Class: ClassNoisy}}
	if err := WritePartners(p, ps); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPartners(p)
	if err != nil || len(got) != 2 || got[1].Class != ClassNoisy || got[0].Key != "k1" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := os.WriteFile(p, []byte("a\tk\tloud\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPartners(p); err == nil {
		t.Fatal("class lạ phải lỗi")
	}
}

func TestNewGatewayRejectsNormalAboveQuota(t *testing.T) {
	ps := []*GWPartner{{ID: "a", Key: "k", Class: ClassNormal}}
	cfg := GWConfig{Base: "http://localhost:1", PartnerRPS: 2, QuotaRPM: 60, NoisyFactor: 10, Mix: map[string]int{gwOpFeed: 1}, BatchItems: 1, FileRecords: 1}
	if _, err := NewGateway(cfg, ps); err == nil {
		t.Fatal("đối tác thường 120 lượt/phút > hạn mức 60 phải bị từ chối")
	}
	cfg.PartnerRPS, cfg.Mix = 0.5, map[string]int{}
	if _, err := NewGateway(cfg, ps); err == nil {
		t.Fatal("trộn rỗng phải lỗi")
	}
}

// fakeGateway giả lập cổng: token bucket theo khóa (capacity = quota/phút), 429 + Retry-After khi hết, kiểm dạng lời gọi.
type fakeGateway struct {
	mu       sync.Mutex
	quota    float64
	buckets  map[string]*[2]float64 // tokens, last (giây)
	failures []string
	seen     map[string]int
	noRetry  bool
}

func (f *fakeGateway) fail(s string) { f.mu.Lock(); f.failures = append(f.failures, s); f.mu.Unlock() }

func (f *fakeGateway) drain(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buckets[key] = &[2]float64{0, float64(time.Now().UnixNano()) / 1e9}
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(apiclientport.KeyHeader)
	now := float64(time.Now().UnixNano()) / 1e9
	f.mu.Lock()
	b := f.buckets[key]
	if b == nil {
		b = &[2]float64{f.quota, now}
		f.buckets[key] = b
	}
	b[0] = min(f.quota, b[0]+(now-b[1])*f.quota/60)
	b[1] = now
	allowed := b[0] >= 1
	if allowed {
		b[0]--
	}
	noRetry := f.noRetry
	f.mu.Unlock()
	if !allowed {
		if !noRetry {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/feeds/hp":
		f.mu.Lock()
		f.seen["feed"]++
		f.mu.Unlock()
		items := []any{map[string]any{"area_code": "31117"}}
		if r.URL.Query().Get("cursor") == "c1" {
			items = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next_cursor": "c1"})
	case r.Method == http.MethodGet && r.URL.Path == "/v1/feeds/hp/checksums":
		f.mu.Lock()
		f.seen["checksums"]++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"versions":[]}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/batches/rcs":
		var env struct {
			Contract string `json:"contract"`
			BatchRef string `json:"batch_ref"`
			Items    []struct {
				EntityKey string         `json:"entity_key"`
				Record    map[string]any `json:"record"`
			} `json:"items"`
		}
		if json.Unmarshal(body, &env) != nil || env.Contract != "rcs" || env.BatchRef == "" || env.BatchRef != r.Header.Get("Idempotency-Key") ||
			len(env.Items) != 3 || env.Items[0].EntityKey != env.Items[0].Record["statement_ref"] {
			f.fail("lô sai dạng: " + string(body))
		}
		f.mu.Lock()
		f.seen["batch"]++
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/files/rcs":
		if r.URL.Query().Get("contract_version") != "1" || r.Header.Get("Content-Type") != "application/x-ndjson" ||
			bytes.Count(body, []byte("\n")) != 4 {
			f.fail("tệp sai dạng")
		}
		f.mu.Lock()
		f.seen["file"]++
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	default:
		f.fail(r.Method + " " + r.URL.String())
		w.WriteHeader(http.StatusNotFound)
	}
}

// Kịch bản cổng trên cổng giả: đối tác thường dưới hạn mức không bao giờ 429; đối tác ồn (10 × hạn mức) nhận 429 +
// Retry-After (tính là đúng, không phải lỗi); đủ bốn thao tác; báo cáo có so sánh cách ly + markdown.
func TestGatewayScenarioAgainstFakeGateway(t *testing.T) {
	f := &fakeGateway{quota: 60, buckets: map[string]*[2]float64{}, seen: map[string]int{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var ps []*GWPartner
	for i := range 5 {
		ps = append(ps, &GWPartner{ID: string(rune('a' + i)), Key: "key-" + string(rune('a'+i)), Class: ClassNormal})
	}
	ps = append(ps, &GWPartner{ID: "z", Key: "key-z", Class: ClassNoisy})
	g, err := NewGateway(GWConfig{Base: srv.URL, Feed: "hp", Contract: "rcs", PartnerRPS: 10, QuotaRPM: 600, NoisyFactor: 10,
		Mix: map[string]int{gwOpFeed: 2, gwOpChecksums: 1, gwOpBatch: 1, gwOpFile: 1}, BatchItems: 3, FileRecords: 4}, ps)
	if err != nil {
		t.Fatal(err)
	}
	// Hạn mức 600/phút = 10 lượt/giây, bucket đầy 600: đối tác ồn 100 lượt/giây cạn bucket sau ~6,7 s — rút ngắn bằng
	// cách làm cạn bucket của nó trước.
	f.drain("key-z")
	rep := g.Compare(context.Background(), GWRun{Baseline: 600 * time.Millisecond, Noisy: 600 * time.Millisecond, Rounds: 2, MaxIncrease: 0.10,
		Slack: time.Second, MaxErrRate: 0.01})
	for _, s := range f.failures {
		t.Error(s)
	}
	if len(rep.Violations) != 0 {
		t.Fatalf("vi phạm: %v\n%s", rep.Violations, rep.Markdown())
	}
	for _, op := range gwOps {
		if f.seen[op] == 0 {
			t.Errorf("thiếu thao tác %s: %v", op, f.seen)
		}
	}
	if len(rep.Phases) != 2 || rep.Isolation == nil || !rep.Isolation.Pass || len(rep.Isolation.PerOp) == 0 {
		t.Fatalf("%+v", rep)
	}
	noisy := rep.Phases[1]
	if noisy.Counters[ClassNoisy+".429"] == 0 || noisy.Counters[ClassNormal+".429"] != 0 || noisy.Counters[ClassNoisy+".429_without_retry_after"] != 0 {
		t.Fatalf("bộ đếm 429: %v", noisy.Counters)
	}
	if _, ok := findSummary(rep.Phases[0], ClassNoisy); ok {
		t.Fatal("lượt nền không có đối tác ồn")
	}
	md := rep.Markdown()
	for _, want := range []string{"### Lượt baseline", "### Lượt noisy", "### Cách ly: ĐẠT", "| normal/feed |", "đạt tiêu chí §42"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown thiếu %q:\n%s", want, md)
		}
	}

	// 429 thiếu Retry-After → vi phạm.
	f.mu.Lock()
	f.noRetry = true
	f.mu.Unlock()
	f.drain("key-z")
	rep = g.Compare(context.Background(), GWRun{Noisy: 400 * time.Millisecond, Rounds: 1, MaxIncrease: 0.10, MaxErrRate: 0.01})
	if !strings.Contains(strings.Join(rep.Violations, "\n"), "thiếu Retry-After") || rep.Isolation != nil {
		t.Fatalf("429 thiếu Retry-After phải là vi phạm; không lượt nền thì không so cách ly: %v", rep.Violations)
	}
}

func TestEvaluateIsolationThreshold(t *testing.T) {
	ph := func(name string, p95 time.Duration) *GWPhase {
		return &GWPhase{Name: name, Summaries: []Summary{{Op: ClassNormal, Count: 100, OK: 100, P95: p95}}, Counters: map[string]int64{ClassNoisy + ".429": 5}}
	}
	rep := &GWReport{Noisy: 1}
	v := evaluate(rep, ph("baseline", 100*time.Millisecond), ph("noisy", 111*time.Millisecond), GWRun{MaxIncrease: 0.10, MaxErrRate: 0.01})
	if len(v) != 1 || rep.Isolation.Pass || rep.Isolation.Increase < 0.109 {
		t.Fatalf("+11%% > 10%% phải vi phạm: %v %+v", v, rep.Isolation)
	}
	v = evaluate(rep, ph("baseline", 100*time.Millisecond), ph("noisy", 110*time.Millisecond), GWRun{MaxIncrease: 0.10, MaxErrRate: 0.01})
	if len(v) != 0 || !rep.Isolation.Pass {
		t.Fatalf("+10%% đúng ngưỡng phải đạt: %v", v)
	}
	v = evaluate(rep, ph("baseline", 10*time.Millisecond), ph("noisy", 14*time.Millisecond), GWRun{MaxIncrease: 0.10, Slack: 5 * time.Millisecond, MaxErrRate: 0.01})
	if len(v) != 0 {
		t.Fatalf("dung sai tuyệt đối: %v", v)
	}
}

func TestGatewaySubcommandFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"gateway", "-gateway", "https://partners.agrigreen.vn", "-partners", "x.tsv"}, &out, &errb); code != 2 ||
		!strings.Contains(errb.String(), "R-95") {
		t.Fatalf("URL không phải staging phải bị chặn: %d %s", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"gateway"}, &out, &errb); code != 2 {
		t.Fatalf("thiếu -gateway/-partners: %d", code)
	}
	errb.Reset()
	if code := run([]string{"gateway-provision", "-admin", "https://admin.agrigreen.vn"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "R-95") {
		t.Fatalf("provision ngoài staging phải bị chặn: %d %s", code, errb.String())
	}
}
