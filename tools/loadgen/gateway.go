package main

// Kịch bản cổng tích hợp (W5.9, kiến trúc v4.1 §42, ADR-0033): N đối tác tổng hợp, mỗi đối tác một khóa API
// (cấp qua Admin API — gateway-provision hoặc bộ khung kiểm thử, KHÔNG BAO GIỜ khóa thật), tải trộn trên
//
//	feed       GET  /v1/feeds/{feed}?cursor=           (đọc tiếp theo next_cursor; hết trang → đọc lại từ đầu)
//	checksums  GET  /v1/feeds/{feed}/checksums
//	batch      POST /v1/batches/{contract}             (phong bì ingest_batch.v1, Idempotency-Key = batch_ref)
//	file       POST /v1/files/{contract}?contract_version=1  (NDJSON cùng lược đồ)
//
// Một (hoặc vài) đối tác "ồn" gửi gấp -gw-noisy-factor (mặc định 10) lần hạn mức requests_per_minute của gói.
// Tiêu chí §42: đối tác ồn nhận 429 + Retry-After; p95 của các đối tác KHÁC không tăng quá 10% so với lượt nền
// (cùng tải, không có đối tác ồn). Lượt nền và lượt ồn chạy xen kẽ -gw-rounds vòng (B, N, B, N, …) và gộp mẫu
// theo loại — khử trôi chậm của môi trường (DB dùng chung, máy ảo ồn) khỏi phép so sánh.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Lớp đối tác trong báo cáo.
const (
	ClassNormal = "normal"
	ClassNoisy  = "noisy"
)

// Thao tác của kịch bản cổng.
const (
	gwOpFeed      = "feed"
	gwOpChecksums = "checksums"
	gwOpBatch     = "batch"
	gwOpFile      = "file"
)

var gwOps = []string{gwOpFeed, gwOpChecksums, gwOpBatch, gwOpFile}

// GWPartner là một đối tác tổng hợp của kịch bản cổng.
type GWPartner struct {
	ID    string // mã khách hàng API (chỉ để đối chiếu, không gửi đi)
	Key   string // bí mật khóa API — không bao giờ in/ghi log
	Class string // normal | noisy

	mu     sync.Mutex
	cursor string
	n      atomic.Uint64
}

// LoadPartners đọc tệp TSV "partner_id<TAB>api_key[<TAB>class]" (dòng # là chú thích; class mặc định normal).
func LoadPartners(path string) ([]*GWPartner, error) {
	f, err := os.Open(path) //nolint:gosec // đường dẫn do người vận hành truyền
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []*GWPartner
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 2 || strings.TrimSpace(cols[0]) == "" || strings.TrimSpace(cols[1]) == "" {
			return nil, fmt.Errorf("%s:%d: cần 'partner_id<TAB>api_key[<TAB>class]'", path, n)
		}
		p := &GWPartner{ID: strings.TrimSpace(cols[0]), Key: strings.TrimSpace(cols[1]), Class: ClassNormal}
		if len(cols) > 2 && strings.TrimSpace(cols[2]) != "" {
			p.Class = strings.TrimSpace(cols[2])
		}
		if p.Class != ClassNormal && p.Class != ClassNoisy {
			return nil, fmt.Errorf("%s:%d: class phải là %s hoặc %s", path, n, ClassNormal, ClassNoisy)
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: không có đối tác", path)
	}
	return out, nil
}

// WritePartners ghi tệp TSV đối tác (quyền 0600 — tệp có bí mật khóa thử).
func WritePartners(path string, ps []*GWPartner) error {
	var b bytes.Buffer
	b.WriteString("# partner_id\tapi_key\tclass — khóa TỔNG HỢP của loadgen gateway-provision (W5.9), không phải khóa thật\n")
	for _, p := range ps {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", p.ID, p.Key, p.Class)
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}

// GWConfig là cấu hình kịch bản cổng.
type GWConfig struct {
	Base        string
	Feed        string
	Contract    string
	PartnerRPS  float64        // tốc độ mỗi đối tác thường (lượt/giây)
	QuotaRPM    int64          // requests_per_minute của gói đối tác (hạn mức mà đối tác ồn vượt)
	NoisyFactor float64        // đối tác ồn gửi NoisyFactor × QuotaRPM / 60 lượt/giây
	Mix         map[string]int // trọng số thao tác
	BatchItems  int            // bản ghi mỗi lô
	FileRecords int            // bản ghi mỗi tệp
	MaxInFlight int            // lượt đang bay tối đa mỗi đối tác
	Timeout     time.Duration
}

// Gateway phát tải tới cổng tích hợp.
type Gateway struct {
	cfg      GWConfig
	partners []*GWPartner
	http     *http.Client
	seq      []string // dãy thao tác theo trọng số
}

// NewGateway kiểm cấu hình và tạo máy phát.
func NewGateway(cfg GWConfig, partners []*GWPartner) (*Gateway, error) {
	u, err := url.Parse(cfg.Base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("địa chỉ cổng không hợp lệ: %q", cfg.Base)
	}
	if len(partners) == 0 {
		return nil, fmt.Errorf("không có đối tác")
	}
	if cfg.PartnerRPS <= 0 || cfg.QuotaRPM < 1 || cfg.NoisyFactor <= 0 {
		return nil, fmt.Errorf("cần tốc độ đối tác > 0, hạn mức ≥ 1 lượt/phút, hệ số ồn > 0")
	}
	if cfg.PartnerRPS*60 > float64(cfg.QuotaRPM) {
		return nil, fmt.Errorf("đối tác thường phải dưới hạn mức: %.2f lượt/giây × 60 > %d lượt/phút", cfg.PartnerRPS, cfg.QuotaRPM)
	}
	if cfg.BatchItems < 1 || cfg.BatchItems > 500 || cfg.FileRecords < 1 {
		return nil, fmt.Errorf("số bản ghi mỗi lô 1..500 (max_batch), mỗi tệp ≥ 1")
	}
	if cfg.MaxInFlight < 1 {
		cfg.MaxInFlight = 16
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	g := &Gateway{cfg: cfg, partners: partners}
	g.cfg.Base = strings.TrimRight(cfg.Base, "/")
	for _, op := range gwOps {
		for i := 0; i < cfg.Mix[op]; i++ {
			g.seq = append(g.seq, op)
		}
	}
	if len(g.seq) == 0 {
		return nil, fmt.Errorf("-gw-mix: cần ít nhất một thao tác có trọng số > 0 (%s)", strings.Join(gwOps, ", "))
	}
	// Xáo dãy cố định (hạt giống cố định): trộn đều thao tác theo thời gian, lặp lại được giữa các lần chạy.
	r := rand.New(rand.NewPCG(59, 59)) //nolint:gosec // không phải mật mã
	r.Shuffle(len(g.seq), func(i, j int) { g.seq[i], g.seq[j] = g.seq[j], g.seq[i] })
	// Mỗi đối tác bắt đầu ở một vị trí khác trong dãy trộn: ở MỌI lúc (kể cả lượt ngắn vài lượt mỗi đối tác) tỷ lệ
	// thao tác của cả nhóm gần đúng trọng số — p95 của lớp so được giữa lượt nền và lượt ồn.
	for i, p := range partners {
		p.n.Store(uint64(i)) //nolint:gosec // i ≥ 0
	}
	conns := len(partners) * 2
	g.http = &http.Client{Timeout: cfg.Timeout, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, MaxIdleConns: conns, MaxIdleConnsPerHost: conns, IdleConnTimeout: 90 * time.Second,
	}}
	return g, nil
}

// ParseMix đọc "feed=6,checksums=1,batch=2,file=1".
func ParseMix(s string) (map[string]int, error) {
	kv, err := parseKV(s)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for k, v := range kv {
		if !contains(gwOps, k) {
			return nil, fmt.Errorf("thao tác lạ %q (%s)", k, strings.Join(gwOps, ", "))
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100 {
			return nil, fmt.Errorf("%s: trọng số nguyên 0..100", k)
		}
		out[k] = n
	}
	return out, nil
}

func (g *Gateway) noisyRPS() float64 { return g.rpsAt(g.cfg.NoisyFactor) }

// rpsAt là tốc độ (lượt/giây) của đối tác gửi factor × hạn mức.
func (g *Gateway) rpsAt(factor float64) float64 { return factor * float64(g.cfg.QuotaRPM) / 60 }

func (g *Gateway) req(ctx context.Context, p *GWPartner, method, path, ctype string, body []byte, idem string) (int, http.Header, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.cfg.Base+path, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set(apiclientport.KeyHeader, p.Key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agrigreen-loadgen/1 (gateway)")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, resp.Header, b, err
}

// rollRecord là bản ghi roll_count_statement.v1 TỔNG HỢP (contracts/integration/examples).
func rollRecord(i int, day string) map[string]any {
	return map[string]any{"statement_ref": ids.New().String(), "schedule_ref": nil, "htx_ref": "01989e00-1111-7222-8333-944455566677",
		"plot_ref": "01989f00-aa11-7b22-8c33-d44e55f66a77", "season_ref": nil, "collected_on": day, "rolls": 1 + i%40, "revision": 1}
}

// do chạy một lượt của đối tác p (thao tác kế tiếp trong dãy trộn) và ghi vào rec.
func (g *Gateway) do(ctx context.Context, p *GWPartner, rec *Recorder) {
	op := g.seq[int(p.n.Add(1)-1)%len(g.seq)]
	var (
		st    int
		hdr   http.Header
		b     []byte
		err   error
		start time.Time
	)
	switch op {
	case gwOpFeed:
		p.mu.Lock()
		cur := p.cursor
		p.mu.Unlock()
		path := "/v1/feeds/" + url.PathEscape(g.cfg.Feed)
		if cur != "" {
			path += "?cursor=" + url.QueryEscape(cur)
		}
		start = time.Now()
		st, hdr, b, err = g.req(ctx, p, http.MethodGet, path, "", nil, "")
		if err == nil && st == http.StatusOK {
			var page struct {
				Items      []json.RawMessage `json:"items"`
				NextCursor string            `json:"next_cursor"`
			}
			if json.Unmarshal(b, &page) == nil {
				rec.Count(p.Class+".feed_items", int64(len(page.Items)))
				p.mu.Lock()
				if len(page.Items) == 0 {
					p.cursor = "" // đã theo kịp đầu feed → lượt sau đồng bộ lại từ đầu (giữ tải đọc ổn định)
				} else {
					p.cursor = page.NextCursor
				}
				p.mu.Unlock()
			}
		}
	case gwOpChecksums:
		start = time.Now()
		st, hdr, _, err = g.req(ctx, p, http.MethodGet, "/v1/feeds/"+url.PathEscape(g.cfg.Feed)+"/checksums", "", nil, "")
	case gwOpBatch:
		day := time.Now().In(vnZone).Format(time.DateOnly)
		items := make([]map[string]any, g.cfg.BatchItems)
		for i := range items {
			r := rollRecord(i, day)
			items[i] = map[string]any{"entity_key": r["statement_ref"], "record": r}
		}
		ref := ids.New().String()
		body, _ := json.Marshal(map[string]any{"contract": g.cfg.Contract, "contract_version": 1, "batch_ref": ref,
			"sent_at": time.Now().In(vnZone).Format(time.RFC3339), "items": items})
		start = time.Now()
		st, hdr, _, err = g.req(ctx, p, http.MethodPost, "/v1/batches/"+url.PathEscape(g.cfg.Contract), "application/json", body, ref)
		if err == nil && is2xx(st) {
			rec.Count(p.Class+".records_sent", int64(g.cfg.BatchItems))
		}
	case gwOpFile:
		day := time.Now().In(vnZone).Format(time.DateOnly)
		var body bytes.Buffer
		for i := 0; i < g.cfg.FileRecords; i++ {
			line, _ := json.Marshal(rollRecord(i, day))
			body.Write(line)
			body.WriteByte('\n')
		}
		start = time.Now()
		st, hdr, _, err = g.req(ctx, p, http.MethodPost, "/v1/files/"+url.PathEscape(g.cfg.Contract)+"?contract_version=1", "application/x-ndjson",
			body.Bytes(), "")
		if err == nil && is2xx(st) {
			rec.Count(p.Class+".records_sent", int64(g.cfg.FileRecords))
		}
	}
	d := time.Since(start)
	label, ok := strconv.Itoa(st), is2xx(st)
	if err != nil {
		label, ok = errLabel(err), false
	}
	if st == http.StatusTooManyRequests {
		rec.Count(p.Class+".429", 1)
		if ra, perr := strconv.Atoi(hdr.Get("Retry-After")); perr != nil || ra < 0 {
			rec.Count(p.Class+".429_without_retry_after", 1)
		} else if p.Class == ClassNoisy {
			ok = true // đối tác ồn: 429 + Retry-After là phản hồi ĐÚNG (áp lực ngược), không phải lỗi
		}
	}
	rec.Observe(p.Class+"/"+op, d, label, ok)
	rec.Observe(p.Class, d, label, ok)
}

// runPhase chạy mọi đối tác (đối tác ồn ở noisyFactor × hạn mức; 0 = không chạy) trong d, ghi vào rec; mỗi đối tác một nhịp vòng hở
// riêng, lệch pha ngẫu nhiên để không dồn cùng một thời điểm.
func (g *Gateway) runPhase(ctx context.Context, d time.Duration, noisyFactor float64, rec *Recorder) time.Duration {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	for _, p := range g.partners {
		rate := g.cfg.PartnerRPS
		if p.Class == ClassNoisy {
			if noisyFactor <= 0 {
				continue
			}
			rate = g.rpsAt(noisyFactor)
		}
		wg.Add(1)
		go func(p *GWPartner, rate float64) {
			defer wg.Done()
			off := time.Duration(rand.Float64() * float64(time.Second) / rate) //nolint:gosec // lệch pha, không phải mật mã
			select {
			case <-ctx.Done():
				return
			case <-time.After(off):
			}
			drive(ctx, Scenario{Name: p.Class, Rate: rate, Do: func(ctx context.Context, rec *Recorder) bool {
				g.do(ctx, p, rec)
				return true
			}}, g.cfg.MaxInFlight, rec)
		}(p, rate)
	}
	wg.Wait()
	return time.Since(start)
}

// GWPhase là kết quả gộp của một loại lượt (baseline | noisy).
type GWPhase struct {
	Name      string           `json:"name"`
	Rounds    int              `json:"rounds"`
	Elapsed   string           `json:"elapsed"`
	Summaries []Summary        `json:"summaries"`
	Counters  map[string]int64 `json:"counters"`
}

// GWIsolation so p95 của đối tác thường giữa lượt nền và lượt có đối tác ồn.
type GWIsolation struct {
	BaselineP95 time.Duration `json:"baseline_p95_ns"`
	NoisyP95    time.Duration `json:"noisy_p95_ns"`
	Increase    float64       `json:"increase"` // (noisy − baseline) / baseline
	MaxIncrease float64       `json:"max_increase"`
	Slack       time.Duration `json:"slack_ns"` // dung sai tuyệt đối cộng thêm (đo cục bộ); 0 ở lượt đo chính thức
	Pass        bool          `json:"pass"`
	PerOp       []GWOpDelta   `json:"per_op"`
}

// GWOpDelta là p95 từng thao tác của đối tác thường (tham khảo, không phải tiêu chí).
type GWOpDelta struct {
	Op          string        `json:"op"`
	BaselineP95 time.Duration `json:"baseline_p95_ns"`
	NoisyP95    time.Duration `json:"noisy_p95_ns"`
	Increase    float64       `json:"increase"`
}

// GWReport là báo cáo JSON của kịch bản cổng.
type GWReport struct {
	StartedAt  time.Time `json:"started_at"`
	Target     string    `json:"target"`
	Partners   int       `json:"partners"`
	Noisy      int       `json:"noisy_partners"`
	PartnerRPS float64   `json:"partner_rps"`
	QuotaRPM   int64     `json:"quota_rpm"`
	NoisyRPS   float64   `json:"noisy_rps"`
	// BaselineNoisyRPS: tốc độ đối tác ồn trong lượt nền (0 = vắng mặt).
	BaselineNoisyRPS float64      `json:"baseline_noisy_rps"`
	Mix              string       `json:"mix"`
	Phases           []GWPhase    `json:"phases"`
	Isolation        *GWIsolation `json:"isolation,omitempty"`
	Violations       []string     `json:"violations"`
}

// GWRun là tham số một lần chạy so sánh.
type GWRun struct {
	Warmup   time.Duration // lượt khởi động (không đo), không có đối tác ồn
	Baseline time.Duration // mỗi vòng; 0 = bỏ lượt nền (chỉ đo có đối tác ồn, không có tiêu chí cách ly)
	Noisy    time.Duration // mỗi vòng
	Rounds   int
	// BaselineNoisyFactor: đối tác ồn trong lượt nền gửi BaselineNoisyFactor × hạn mức. 0 (mặc định, đúng chữ §42) =
	// lượt nền không có đối tác ồn. 1 = đối tác ồn dùng ĐÚNG hạn mức — dùng ở quy mô nhỏ (20 đối tác), nơi tải hợp lệ
	// trong hạn mức của một đối tác đã là ~10% tổng tải và sẽ bị tính nhầm là ảnh hưởng của phần VƯỢT hạn mức.
	BaselineNoisyFactor float64
	MaxIncrease         float64
	Slack               time.Duration
	MaxErrRate          float64
}

func phaseOf(name string, rounds int, el time.Duration, rec *Recorder) GWPhase {
	return GWPhase{Name: name, Rounds: rounds, Elapsed: el.Round(time.Millisecond).String(), Summaries: rec.Summaries(el), Counters: rec.Counters()}
}

func findSummary(ph GWPhase, op string) (Summary, bool) {
	for _, s := range ph.Summaries {
		if s.Op == op {
			return s, true
		}
	}
	return Summary{}, false
}

// Compare chạy khởi động, rồi Rounds vòng (nền, ồn) xen kẽ, gộp mẫu theo loại và đánh giá tiêu chí §42.
func (g *Gateway) Compare(ctx context.Context, r GWRun) GWReport {
	if r.Rounds < 1 {
		r.Rounds = 1
	}
	noisy := 0
	for _, p := range g.partners {
		if p.Class == ClassNoisy {
			noisy++
		}
	}
	mix := make([]string, 0, len(gwOps))
	for _, op := range gwOps {
		mix = append(mix, fmt.Sprintf("%s=%d", op, g.cfg.Mix[op]))
	}
	rep := GWReport{StartedAt: time.Now().UTC(), Target: g.cfg.Base, Partners: len(g.partners), Noisy: noisy, PartnerRPS: g.cfg.PartnerRPS,
		QuotaRPM: g.cfg.QuotaRPM, NoisyRPS: g.noisyRPS(), BaselineNoisyRPS: g.rpsAt(r.BaselineNoisyFactor), Mix: strings.Join(mix, ","), Violations: []string{}}
	if r.Warmup > 0 {
		g.runPhase(ctx, r.Warmup, r.BaselineNoisyFactor, NewRecorder())
	}
	baseRec, noisyRec := NewRecorder(), NewRecorder()
	var baseEl, noisyEl time.Duration
	for i := 0; i < r.Rounds && ctx.Err() == nil; i++ {
		if r.Baseline > 0 {
			baseEl += g.runPhase(ctx, r.Baseline, r.BaselineNoisyFactor, baseRec)
		}
		if r.Noisy > 0 {
			noisyEl += g.runPhase(ctx, r.Noisy, g.cfg.NoisyFactor, noisyRec)
		}
	}
	var base, withNoisy *GWPhase
	if r.Baseline > 0 {
		ph := phaseOf("baseline", r.Rounds, baseEl, baseRec)
		rep.Phases = append(rep.Phases, ph)
		base = &rep.Phases[len(rep.Phases)-1]
	}
	if r.Noisy > 0 {
		ph := phaseOf("noisy", r.Rounds, noisyEl, noisyRec)
		rep.Phases = append(rep.Phases, ph)
		withNoisy = &rep.Phases[len(rep.Phases)-1]
	}
	rep.Violations = evaluate(&rep, base, withNoisy, r)
	return rep
}

// evaluate áp tiêu chí §42 (W5.9) lên kết quả.
func evaluate(rep *GWReport, base, withNoisy *GWPhase, r GWRun) []string {
	v := []string{}
	for _, ph := range []*GWPhase{base, withNoisy} {
		if ph == nil {
			continue
		}
		s, ok := findSummary(*ph, ClassNormal)
		switch {
		case !ok || s.Count == 0:
			v = append(v, ph.Name+": đối tác thường không có lượt nào hoàn tất")
		case s.ErrorRate > r.MaxErrRate:
			v = append(v, fmt.Sprintf("%s: tỷ lệ lỗi đối tác thường %.3f%% > %.3f%% (%v)", ph.Name, 100*s.ErrorRate, 100*r.MaxErrRate, s.ByStatus))
		}
		if n := ph.Counters[ClassNormal+".429"]; n > 0 {
			v = append(v, fmt.Sprintf("%s: đối tác thường (dưới hạn mức) nhận %d phản hồi 429", ph.Name, n))
		}
	}
	if withNoisy != nil && rep.Noisy > 0 {
		if withNoisy.Counters[ClassNoisy+".429"] == 0 {
			v = append(v, "noisy: đối tác ồn gửi gấp hạn mức nhưng không nhận 429")
		}
		if n := withNoisy.Counters[ClassNoisy+".429_without_retry_after"]; n > 0 {
			v = append(v, fmt.Sprintf("noisy: %d phản hồi 429 thiếu Retry-After", n))
		}
		if s, ok := findSummary(*withNoisy, ClassNoisy); ok && s.ErrorRate > r.MaxErrRate {
			v = append(v, fmt.Sprintf("noisy: lỗi khác 429 + Retry-After ở đối tác ồn %.3f%% (%v)", 100*s.ErrorRate, s.ByStatus))
		}
	}
	if base != nil && withNoisy != nil && rep.Noisy > 0 {
		b, okB := findSummary(*base, ClassNormal)
		n, okN := findSummary(*withNoisy, ClassNormal)
		if okB && okN && b.P95 > 0 {
			iso := &GWIsolation{BaselineP95: b.P95, NoisyP95: n.P95, MaxIncrease: r.MaxIncrease, Slack: r.Slack,
				Increase: float64(n.P95-b.P95) / float64(b.P95)}
			iso.Pass = float64(n.P95) <= float64(b.P95)*(1+r.MaxIncrease)+float64(r.Slack)
			for _, op := range gwOps {
				bo, ok1 := findSummary(*base, ClassNormal+"/"+op)
				no, ok2 := findSummary(*withNoisy, ClassNormal+"/"+op)
				if ok1 && ok2 && bo.P95 > 0 {
					iso.PerOp = append(iso.PerOp, GWOpDelta{Op: op, BaselineP95: bo.P95, NoisyP95: no.P95, Increase: float64(no.P95-bo.P95) / float64(bo.P95)})
				}
			}
			rep.Isolation = iso
			if !iso.Pass {
				v = append(v, fmt.Sprintf("cách ly: p95 đối tác thường %s → %s (+%.1f%%) vượt %.0f%% + %s", round(b.P95), round(n.P95),
					100*iso.Increase, 100*r.MaxIncrease, r.Slack))
			}
		}
	}
	return v
}

// Markdown trả báo cáo dạng bảng markdown (dán vào biên bản diễn tập).
func (rep GWReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Kiểm thử tải cổng tích hợp (W5.9, §42) — %s\n\n", rep.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Đích: `%s`; đối tác: %d (ồn: %d); tốc độ đối tác thường %.2f lượt/giây; hạn mức gói %d lượt/phút; đối tác ồn %.2f lượt/giây\n",
		rep.Target, rep.Partners, rep.Noisy, rep.PartnerRPS, rep.QuotaRPM, rep.NoisyRPS)
	fmt.Fprintf(&b, "- Lượt nền: đối tác ồn %.2f lượt/giây (0 = vắng mặt); trộn thao tác: `%s`\n\n", rep.BaselineNoisyRPS, rep.Mix)
	for _, ph := range rep.Phases {
		fmt.Fprintf(&b, "### Lượt %s (%d vòng, %s)\n\n", ph.Name, ph.Rounds, ph.Elapsed)
		b.WriteString("| lớp/thao tác | số lượt | rps | tỷ lệ lỗi | bỏ | 429 | p50 | p95 | p99 | max |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, s := range ph.Summaries {
			fmt.Fprintf(&b, "| %s | %d | %.1f | %.3f%% | %d | %d | %s | %s | %s | %s |\n", s.Op, s.Count, s.RPS, 100*s.ErrorRate, s.Dropped,
				s.ByStatus["429"], round(s.P50), round(s.P95), round(s.P99), round(s.Max))
		}
		keys := make([]string, 0, len(ph.Counters))
		for k := range ph.Counters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			b.WriteString("\n")
			for _, k := range keys {
				fmt.Fprintf(&b, "- `%s` = %d\n", k, ph.Counters[k])
			}
		}
		b.WriteString("\n")
	}
	if iso := rep.Isolation; iso != nil {
		verdict := "ĐẠT"
		if !iso.Pass {
			verdict = "KHÔNG ĐẠT"
		}
		fmt.Fprintf(&b, "### Cách ly: %s\n\np95 đối tác thường: nền %s → có đối tác ồn %s (%+.1f%%; ngưỡng +%.0f%%, dung sai tuyệt đối %s).\n\n",
			verdict, round(iso.BaselineP95), round(iso.NoisyP95), 100*iso.Increase, 100*iso.MaxIncrease, iso.Slack)
		if len(iso.PerOp) > 0 {
			b.WriteString("| thao tác (đối tác thường) | p95 nền | p95 có ồn | thay đổi |\n|---|---:|---:|---:|\n")
			for _, d := range iso.PerOp {
				fmt.Fprintf(&b, "| %s | %s | %s | %+.1f%% |\n", d.Op, round(d.BaselineP95), round(d.NoisyP95), 100*d.Increase)
			}
			b.WriteString("\n")
		}
	}
	if len(rep.Violations) == 0 {
		b.WriteString("**Kết luận: đạt tiêu chí §42.**\n")
	} else {
		b.WriteString("**Kết luận: KHÔNG đạt:**\n\n")
		for _, v := range rep.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	return b.String()
}
