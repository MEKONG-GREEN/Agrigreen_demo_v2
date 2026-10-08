// Lệnh loadgen: máy phát tải cho diễn tập tải ĐTK 2 (Phụ lục A G4, docs/design/mkg.md §7).
// Viết bằng Go thay k6 (AGPL-3.0, ngoài danh sách deploy/policies/allowed-licenses.txt — RB-4).
//
// Kịch bản (vòng hở, tốc độ cố định mỗi kịch bản, độ trễ đo từ lúc gửi tới khi đọc xong phản hồi):
//
//	sync_pull     GET  /v1/sync/pull (con trỏ theo thiết bị, X-Device-Id)            Application API
//	sync_push     POST /v1/sync/push (N bản ghi field_report, Idempotency-Key)        Application API
//	read          GET  các đường dẫn đọc (vd /v1/geo/standard-map?zoom=13)             Application API
//	ingest_batch  POST /v1/ingest/batches (lô số đo ký Ed25519, contracts/edge/batch.v1.json, mTLS)
//
// Báo cáo p50/p95/p99, tỷ lệ lỗi, rps theo kịch bản; -json ghi kết quả máy đọc được. Thoát 1 khi
// vượt ngưỡng (-max-p95, -max-error-rate) — ngưỡng mặc định lấy từ docs/runbooks/load-drill.md.
//
// Chỉ chạy với môi trường staging/dev có dữ liệu TỔNG HỢP (mục 21, R-95), không bao giờ production.
//
//	go run ./tools/loadgen -api https://api.staging.example.vn -accounts accounts.tsv \
//	   -rate sync_pull=40,sync_push=10,read=20 -duration 10m
//	go run ./tools/loadgen -ingest https://ingest.ot.staging.example.vn:8443/v1/ingest/batches \
//	   -devices devices.json -ingest-ca service-ca.crt -rate ingest_batch=15 -duration 10m
//
// Kịch bản cổng tích hợp (W5.9, §42 — 200 đối tác, một đối tác ồn gấp 10 lần hạn mức): lệnh con
// `gateway-provision` (cấp đối tác tổng hợp qua Admin API) và `gateway` (đo nền/ồn xen kẽ), xem gwcli.go.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type config struct {
	api          string
	accounts     string
	register     bool
	payload      string
	pushRecords  int
	pullLimit    int
	readPaths    string
	ingestURL    string
	devices      string
	ingestCA     string
	ingestSNI    string
	batchSize    int
	counterBase  int64
	rates        map[string]float64
	duration     time.Duration
	maxInFlight  int
	timeout      time.Duration
	tokenReload  time.Duration
	jsonOut      string
	maxP95       map[string]time.Duration
	maxErrorRate float64
	allowProd    bool
}

func parseKV(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("cần dạng khóa=giá trị: %q", part)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

var scenarioNames = []string{"sync_pull", "sync_push", "read", "ingest_batch"}

func parseFlags(args []string, stderr io.Writer) (*config, error) {
	fs := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := &config{}
	var rates, p95 string
	fs.StringVar(&c.api, "api", "", "URL gốc Application API (vd https://api.staging.<miền>)")
	fs.StringVar(&c.accounts, "accounts", "", "tệp TSV device_id<TAB>token (người dùng + thiết bị tổng hợp)")
	fs.BoolVar(&c.register, "register", false, "đăng ký thiết bị của mọi tài khoản trước khi chạy (POST /v1/devices)")
	fs.StringVar(&c.payload, "push-payload", "", "tệp {\"entity\",\"payload\"} cho sync_push (mặc định field_report mẫu)")
	fs.IntVar(&c.pushRecords, "push-records", 20, "số bản ghi mỗi lượt sync_push (≤ 500)")
	fs.IntVar(&c.pullLimit, "pull-limit", 500, "limit của sync_pull (≤ 1000)")
	fs.StringVar(&c.readPaths, "read-paths", "/v1/geo/standard-map?zoom=13,/v1/masterdata/catalogs/rice_variety,/v1/notifications", "đường dẫn GET của kịch bản read, phân tách dấu phẩy")
	fs.StringVar(&c.ingestURL, "ingest", "", "URL nhận lô của ingest (…/v1/ingest/batches)")
	fs.StringVar(&c.devices, "devices", "", "tệp JSON thiết bị đã duyệt [{device_id, epoch, source_key, cert, key}]")
	fs.StringVar(&c.ingestCA, "ingest-ca", "", "CA xác thực chứng thư máy chủ ingest (CA dịch vụ)")
	fs.StringVar(&c.ingestSNI, "ingest-server-name", "", "tên máy chủ TLS của ingest (khi URL dùng IP)")
	fs.IntVar(&c.batchSize, "batch-size", 100, "số số đo mỗi lô (1..1000)")
	fs.Int64Var(&c.counterBase, "counter-base", 0, "bộ đếm khởi đầu của thiết bị (0 = Unix ms × 1000: luôn tăng giữa các lần chạy)")
	fs.StringVar(&rates, "rate", "", "tốc độ lượt/giây theo kịch bản, vd sync_pull=40,sync_push=10,read=20,ingest_batch=15")
	fs.DurationVar(&c.duration, "duration", 5*time.Minute, "thời gian đo")
	fs.IntVar(&c.maxInFlight, "max-inflight", 200, "số lượt đang bay tối đa mỗi kịch bản (vượt → lượt bị bỏ, tính là lỗi)")
	fs.DurationVar(&c.timeout, "timeout", 30*time.Second, "hạn mỗi yêu cầu HTTP")
	fs.DurationVar(&c.tokenReload, "token-reload", time.Minute, "chu kỳ đọc lại token từ -accounts (0 = không)")
	fs.StringVar(&c.jsonOut, "json", "", "ghi kết quả JSON vào tệp")
	fs.StringVar(&p95, "max-p95", "2s", "ngưỡng p95: một giá trị cho mọi kịch bản hoặc kịch_bản=thời_gian,...")
	fs.Float64Var(&c.maxErrorRate, "max-error-rate", 0.01, "ngưỡng tỷ lệ lỗi (0..1), gồm cả lượt bị bỏ")
	fs.BoolVar(&c.allowProd, "i-know-this-is-not-production", false, "bắt buộc khi URL không chứa 'staging', 'dev', 'localhost' hoặc '127.0.0.1'")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	kv, err := parseKV(rates)
	if err != nil {
		return nil, fmt.Errorf("-rate: %w", err)
	}
	c.rates = map[string]float64{}
	for k, v := range kv {
		if !contains(scenarioNames, k) {
			return nil, fmt.Errorf("-rate: kịch bản lạ %q (%s)", k, strings.Join(scenarioNames, ", "))
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 10000 {
			return nil, fmt.Errorf("-rate %s: cần số lượt/giây trong (0, 10000]", k)
		}
		c.rates[k] = f
	}
	if len(c.rates) == 0 {
		return nil, errors.New("cần -rate cho ít nhất một kịch bản")
	}
	c.maxP95 = map[string]time.Duration{}
	if !strings.Contains(p95, "=") {
		d, err := time.ParseDuration(strings.TrimSpace(p95))
		if err != nil {
			return nil, fmt.Errorf("-max-p95: %w", err)
		}
		for _, n := range scenarioNames {
			c.maxP95[n] = d
		}
	} else {
		kv, err := parseKV(p95)
		if err != nil {
			return nil, fmt.Errorf("-max-p95: %w", err)
		}
		for k, v := range kv {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("-max-p95 %s: %w", k, err)
			}
			c.maxP95[k] = d
		}
	}
	needAPI := c.rates["sync_pull"] > 0 || c.rates["sync_push"] > 0 || c.rates["read"] > 0
	if needAPI && (c.api == "" || c.accounts == "") {
		return nil, errors.New("sync_pull/sync_push/read cần -api và -accounts")
	}
	if c.rates["ingest_batch"] > 0 && (c.ingestURL == "" || c.devices == "") {
		return nil, errors.New("ingest_batch cần -ingest và -devices")
	}
	if c.pushRecords < 1 || c.pushRecords > 500 {
		return nil, errors.New("-push-records phải 1..500 (sync-contract)")
	}
	if c.pullLimit < 1 || c.pullLimit > 1000 {
		return nil, errors.New("-pull-limit phải 1..1000 (sync-contract)")
	}
	if !c.allowProd {
		for _, u := range []string{c.api, c.ingestURL} {
			if u != "" && !looksNonProd(u) {
				return nil, fmt.Errorf("%s không giống môi trường staging/dev — diễn tập tải chỉ chạy trên dữ liệu tổng hợp (R-95); thêm -i-know-this-is-not-production nếu chắc chắn", u)
			}
		}
	}
	if c.counterBase == 0 {
		c.counterBase = time.Now().UnixMilli() * 1000
	}
	return c, nil
}

func looksNonProd(u string) bool {
	l := strings.ToLower(u)
	for _, s := range []string{"staging", "dev", "localhost", "127.0.0.1", "[::1]"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Result là báo cáo JSON.
type Result struct {
	StartedAt  time.Time          `json:"started_at"`
	Elapsed    string             `json:"elapsed"`
	Rates      map[string]float64 `json:"rates"`
	Summaries  []Summary          `json:"summaries"`
	Counters   map[string]int64   `json:"counters"`
	Violations []string           `json:"violations"`
}

// Check so kết quả với ngưỡng.
func Check(sums []Summary, maxP95 map[string]time.Duration, maxErr float64) []string {
	var v []string
	for _, s := range sums {
		if lim, ok := maxP95[s.Op]; ok && s.Count > 0 && s.P95 > lim {
			v = append(v, fmt.Sprintf("%s: p95 %s > %s", s.Op, round(s.P95), lim))
		}
		if s.ErrorRate > maxErr {
			v = append(v, fmt.Sprintf("%s: tỷ lệ lỗi %.3f%% > %.3f%%", s.Op, 100*s.ErrorRate, 100*maxErr))
		}
		if s.Count == 0 {
			v = append(v, fmt.Sprintf("%s: không có lượt nào hoàn tất", s.Op))
		}
	}
	return v
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "gateway":
			return runGateway(args[1:], stdout, stderr)
		case "gateway-provision":
			return runProvision(args[1:], stdout, stderr)
		}
	}
	c, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		outln(stderr, "loadgen:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var scen []Scenario
	if c.api != "" && c.accounts != "" {
		accs, err := LoadAccounts(c.accounts)
		if err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
		api, err := NewAPI(c.api, accs, c.payload, c.maxInFlight, c.timeout)
		if err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
		api.PushRecords, api.PullLimit = c.pushRecords, c.pullLimit
		if c.register {
			if err := api.Register(ctx); err != nil {
				outln(stderr, "loadgen:", err)
				return 2
			}
		}
		if c.tokenReload > 0 {
			go func() {
				t := time.NewTicker(c.tokenReload)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						if err := ReloadTokens(c.accounts, accs); err != nil {
							outln(stderr, "loadgen: đọc lại token:", err)
						}
					}
				}
			}()
		}
		var paths []string
		for _, p := range strings.Split(c.readPaths, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		scen = append(scen,
			Scenario{Name: "sync_pull", Rate: c.rates["sync_pull"], Do: api.SyncPull},
			Scenario{Name: "sync_push", Rate: c.rates["sync_push"], Do: api.SyncPush})
		if len(paths) > 0 {
			scen = append(scen, Scenario{Name: "read", Rate: c.rates["read"], Do: api.Read(paths)})
		}
	}
	if c.ingestURL != "" && c.devices != "" {
		in, err := NewIngest(c.ingestURL, c.devices, c.ingestCA, c.ingestSNI, c.batchSize, c.counterBase, c.timeout)
		if err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
		scen = append(scen, Scenario{Name: "ingest_batch", Rate: c.rates["ingest_batch"], Do: in.Batch})
	}
	rec := NewRecorder()
	started := time.Now()
	outf(stdout, "loadgen: chạy %s, tốc độ %v\n", c.duration, c.rates)
	elapsed := Run(ctx, scen, c.duration, c.maxInFlight, rec)
	sums := rec.Summaries(elapsed)
	PrintTable(stdout, sums)
	counters := rec.Counters()
	keys := make([]string, 0, len(counters))
	for k := range counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		outf(stdout, "  %-32s %d\n", k, counters[k])
	}
	viol := Check(sums, c.maxP95, c.maxErrorRate)
	seen := map[string]bool{}
	for _, s := range sums {
		seen[s.Op] = true
	}
	for _, n := range scenarioNames {
		if c.rates[n] > 0 && !seen[n] {
			viol = append(viol, n+": không có lượt nào")
		}
	}
	if c.jsonOut != "" {
		b, _ := json.MarshalIndent(Result{StartedAt: started.UTC(), Elapsed: elapsed.Round(time.Millisecond).String(), Rates: c.rates,
			Summaries: sums, Counters: counters, Violations: viol}, "", "  ")
		if err := os.WriteFile(c.jsonOut, b, 0o600); err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
	}
	for _, v := range viol {
		outln(stdout, "VƯỢT NGƯỠNG:", v)
	}
	if len(viol) > 0 {
		return 1
	}
	outln(stdout, "loadgen: đạt ngưỡng")
	return 0
}
