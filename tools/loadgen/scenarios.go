package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

var vnZone = time.FixedZone("ICT", 7*3600)

// ---------------------------------------------------------------- tài khoản Application API

// Account là một người dùng tổng hợp + thiết bị đã đăng ký (sync-contract: mọi lời gọi sync mang X-Device-Id).
type Account struct {
	DeviceID string
	token    atomic.Value // string
	mu       sync.Mutex
	cursor   string
	seq      int64
}

// Token trả bearer token hiện hành.
func (a *Account) Token() string { v, _ := a.token.Load().(string); return v }

// LoadAccounts đọc tệp TSV "device_id<TAB>token" (dòng # là chú thích). Token có thể được làm mới bằng
// cách ghi đè tệp — ReloadTokens đọc lại theo chu kỳ.
func LoadAccounts(path string) ([]*Account, error) {
	rows, err := readTSV(path)
	if err != nil {
		return nil, err
	}
	out := make([]*Account, 0, len(rows))
	for _, r := range rows {
		a := &Account{DeviceID: r[0]}
		a.token.Store(r[1])
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: không có tài khoản", path)
	}
	return out, nil
}

// ReloadTokens cập nhật token theo device_id từ tệp.
func ReloadTokens(path string, accs []*Account) error {
	rows, err := readTSV(path)
	if err != nil {
		return err
	}
	byDev := map[string]string{}
	for _, r := range rows {
		byDev[r[0]] = r[1]
	}
	for _, a := range accs {
		if t, ok := byDev[a.DeviceID]; ok {
			a.token.Store(t)
		}
	}
	return nil
}

func readTSV(path string) ([][2]string, error) {
	f, err := os.Open(path) //nolint:gosec // đường dẫn do người vận hành truyền
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][2]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dev, tok, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(dev) == "" || strings.TrimSpace(tok) == "" {
			return nil, fmt.Errorf("%s:%d: cần 'device_id<TAB>token'", path, n)
		}
		out = append(out, [2]string{strings.TrimSpace(dev), strings.TrimSpace(tok)})
	}
	return out, sc.Err()
}

// API là client Application API (vùng nền tảng).
type API struct {
	Base   string
	HTTP   *http.Client
	accs   []*Account
	next   atomic.Uint64
	record struct {
		entity  string
		payload json.RawMessage
	}
	PushRecords int
	PullLimit   int
}

// NewAPI tạo client. payloadFile rỗng → bản ghi field_report mẫu (apps/field-app/test/fixtures/contract).
func NewAPI(base string, accs []*Account, payloadFile string, maxConns int, timeout time.Duration) (*API, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("địa chỉ API không hợp lệ: %q", base)
	}
	a := &API{Base: strings.TrimRight(base, "/"), accs: accs, PushRecords: 20, PullLimit: 500}
	a.HTTP = &http.Client{Timeout: timeout, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, MaxIdleConns: maxConns, MaxIdleConnsPerHost: maxConns,
		IdleConnTimeout: 90 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}}
	a.record.entity = "field_report"
	a.record.payload = json.RawMessage(defaultFieldReport)
	if payloadFile != "" {
		b, err := os.ReadFile(payloadFile) //nolint:gosec // tệp do người vận hành truyền
		if err != nil {
			return nil, err
		}
		var tpl struct {
			Entity  string          `json:"entity"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(b, &tpl); err != nil || tpl.Entity == "" || len(tpl.Payload) == 0 {
			return nil, fmt.Errorf("%s: cần {\"entity\": ..., \"payload\": {...}}", payloadFile)
		}
		a.record.entity, a.record.payload = tpl.Entity, tpl.Payload
	}
	return a, nil
}

// defaultFieldReport = apps/field-app/test/fixtures/contract/field_report_growth_pest.json (dữ liệu tổng hợp).
const defaultFieldReport = `{"kind":"growth_pest","commune_code":"31117","cooperative_ref":null,"period":"2026-12","season_code":"","variety_code":"","sown_area_ha":null,"sown_on":null,"growth_stage":"de_nhanh","pest_code":"RAY_NAU","severity":"trung_binh","affected_area_ha":"3.25","urgent":true,"note":""}`

func (a *API) pick() *Account { return a.accs[int(a.next.Add(1)-1)%len(a.accs)] }

func (a *API) do(ctx context.Context, acc *Account, method, path string, body []byte, idem string) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+acc.Token())
	req.Header.Set("X-Device-Id", acc.DeviceID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agrigreen-loadgen/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, b, err
}

// Register đăng ký thiết bị của mọi tài khoản (POST /v1/devices, idempotent theo device_id).
func (a *API) Register(ctx context.Context) error {
	for _, acc := range a.accs {
		body, _ := json.Marshal(map[string]string{"device_id": acc.DeviceID, "platform": "android", "app_version": "loadgen"})
		st, b, err := a.do(ctx, acc, http.MethodPost, "/v1/devices", body, "loadgen-register-"+acc.DeviceID)
		if err != nil {
			return err
		}
		if st != http.StatusOK && st != http.StatusCreated {
			return fmt.Errorf("đăng ký thiết bị %s: HTTP %d %s", acc.DeviceID, st, truncate(b))
		}
	}
	return nil
}

func observeHTTP(rec *Recorder, op string, start time.Time, st int, err error, okFn func(int) bool) bool {
	d := time.Since(start)
	switch {
	case err != nil:
		rec.Observe(op, d, errLabel(err), false)
		return false
	default:
		ok := okFn(st)
		rec.Observe(op, d, strconv.Itoa(st), ok)
		return ok
	}
}

func errLabel(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "net"
}

func is2xx(st int) bool { return st >= 200 && st < 300 }

// SyncPull: GET /v1/sync/pull theo con trỏ của tài khoản (lượt sau nối tiếp has_more).
func (a *API) SyncPull(ctx context.Context, rec *Recorder) bool {
	acc := a.pick()
	if !acc.mu.TryLock() {
		return false // tài khoản đang có lượt khác — một thiết bị không đồng bộ song song
	}
	defer acc.mu.Unlock()
	q := url.Values{"limit": {strconv.Itoa(a.PullLimit)}}
	if acc.cursor != "" {
		q.Set("cursor", acc.cursor)
	}
	start := time.Now()
	st, b, err := a.do(ctx, acc, http.MethodGet, "/v1/sync/pull?"+q.Encode(), nil, "")
	ok := observeHTTP(rec, "sync_pull", start, st, err, func(s int) bool { return s == http.StatusOK })
	if ok {
		var page struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
			Changes    []json.RawMessage
		}
		if json.Unmarshal(b, &page) == nil {
			rec.Count("sync_pull.changes", int64(len(page.Changes)))
			acc.cursor = page.NextCursor
		}
	} else if st == http.StatusBadRequest {
		acc.cursor = "" // sync.cursor_invalid → pull lại từ đầu (sync-contract)
	}
	return true
}

// SyncPush: POST /v1/sync/push với PushRecords bản ghi mới (UUIDv7, idem_key dev:<thiết bị>:<seq>).
func (a *API) SyncPush(ctx context.Context, rec *Recorder) bool {
	acc := a.pick()
	if !acc.mu.TryLock() {
		return false
	}
	defer acc.mu.Unlock()
	type record struct {
		ID               string          `json:"id"`
		IdemKey          string          `json:"idem_key"`
		Entity           string          `json:"entity"`
		Op               string          `json:"op"`
		EventTime        string          `json:"event_time"`
		SupersedesID     *string         `json:"supersedes_id"`
		CorrectionReason *string         `json:"correction_reason"`
		Payload          json.RawMessage `json:"payload"`
	}
	now := time.Now().In(vnZone).Format(time.RFC3339)
	recs := make([]record, a.PushRecords)
	if acc.seq == 0 {
		// Dãy idem_key tăng dần, không trùng lần diễn tập trước (sync-contract: dev:<device_id>:<số>).
		acc.seq = time.Now().UnixMicro()
	}
	for i := range recs {
		acc.seq++
		recs[i] = record{ID: ids.New().String(), IdemKey: fmt.Sprintf("dev:%s:%d", acc.DeviceID, acc.seq),
			Entity: a.record.entity, Op: "create", EventTime: now, Payload: a.record.payload}
	}
	body, _ := json.Marshal(map[string]any{"device_id": acc.DeviceID, "records": recs})
	start := time.Now()
	st, b, err := a.do(ctx, acc, http.MethodPost, "/v1/sync/push", body, ids.New().String())
	if observeHTTP(rec, "sync_push", start, st, err, func(s int) bool { return s == http.StatusOK }) {
		var res struct {
			Results []struct {
				Status string `json:"status"`
			} `json:"results"`
		}
		if json.Unmarshal(b, &res) == nil {
			for _, r := range res.Results {
				rec.Count("sync_push.record."+r.Status, 1)
			}
		}
	}
	return true
}

// Read: GET một trong các đường dẫn đọc (vòng tròn).
func (a *API) Read(paths []string) func(context.Context, *Recorder) bool {
	var n atomic.Uint64
	return func(ctx context.Context, rec *Recorder) bool {
		p := paths[int(n.Add(1)-1)%len(paths)]
		acc := a.pick()
		start := time.Now()
		st, _, err := a.do(ctx, acc, http.MethodGet, p, nil, "")
		observeHTTP(rec, "read", start, st, err, is2xx)
		return true
	}
}

func truncate(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

// ---------------------------------------------------------------- ingest (edge wire, mTLS)

// DeviceSpec là một thiết bị tổng hợp ĐÃ DUYỆT trong sổ thiết bị (ingest.md mục 3): tệp do
// `edge-agent enroll` sinh + chứng thư do CA thiết bị (staging) ký.
type DeviceSpec struct {
	DeviceID     string `json:"device_id"`
	Epoch        int    `json:"epoch"`
	SourceKey    string `json:"source_key"` // PEM PKCS#8 Ed25519 (source.key)
	Cert         string `json:"cert"`       // chứng thư client mTLS
	Key          string `json:"key"`        // khóa client mTLS
	CounterStart int64  `json:"counter_start,omitempty"`
}

type device struct {
	spec    DeviceSpec
	priv    ed25519.PrivateKey
	keyID   string
	client  *http.Client
	mu      sync.Mutex
	counter int64 // bộ đếm đã dùng cuối cùng
	value   int64 // gam, trôi dần (số đo tổng hợp)
}

// Ingest phát lô số đo có chữ ký nguồn tới POST /v1/ingest/batches (contracts/edge/batch.v1.json).
type Ingest struct {
	URL       string
	BatchSize int
	devs      []*device
	next      atomic.Uint64
}

// NewIngest nạp thiết bị. counterBase > 0 → bộ đếm mỗi thiết bị bắt đầu sau counterBase (vd Unix ms ×
// 1000, luôn lớn hơn lần diễn tập trước — ingest từ chối bộ đếm lùi, ADR-0019).
func NewIngest(target string, specsFile, serverCA, serverName string, batchSize int, counterBase int64, timeout time.Duration) (*Ingest, error) {
	if batchSize < 1 || batchSize > edgewire.MaxReadings {
		return nil, fmt.Errorf("kích thước lô phải 1..%d", edgewire.MaxReadings)
	}
	b, err := os.ReadFile(specsFile) //nolint:gosec // tệp do người vận hành truyền
	if err != nil {
		return nil, err
	}
	var specs []DeviceSpec
	if err := json.Unmarshal(b, &specs); err != nil {
		return nil, fmt.Errorf("%s: %w", specsFile, err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%s: không có thiết bị", specsFile)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if serverCA != "" {
		ca, err := os.ReadFile(serverCA) //nolint:gosec // tệp do người vận hành truyền
		if err != nil {
			return nil, err
		}
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("%s: không có chứng thư CA", serverCA)
		}
	}
	in := &Ingest{URL: target, BatchSize: batchSize}
	for _, s := range specs {
		priv, err := loadEd25519(s.SourceKey)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.DeviceID, err)
		}
		cert, err := tls.LoadX509KeyPair(s.Cert, s.Key)
		if err != nil {
			return nil, fmt.Errorf("%s: chứng thư mTLS: %w", s.DeviceID, err)
		}
		start := s.CounterStart
		if start <= 0 {
			start = counterBase
		}
		d := &device{spec: s, priv: priv, keyID: edgewire.KeyID(priv.Public().(ed25519.PublicKey)), counter: start, value: 1_000_000}
		d.client = &http.Client{Timeout: timeout, Transport: &http.Transport{
			MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, Certificates: []tls.Certificate{cert}, ServerName: serverName},
		}}
		in.devs = append(in.devs, d)
	}
	return in, nil
}

func loadEd25519(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path) //nolint:gosec // tệp do người vận hành truyền
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return nil, errors.New("khóa nguồn phải là PEM \"PRIVATE KEY\" (PKCS#8) như edge-agent")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("khóa nguồn không phải Ed25519")
	}
	return priv, nil
}

// BuildBatch dựng và ký lô kế tiếp của thiết bị (gọi khi đang giữ d.mu).
func (d *device) BuildBatch(n int, now time.Time) (edgewire.Batch, error) {
	zero := 0
	b := edgewire.Batch{
		Format: "agrigreen.edge.batch.v1", DeviceID: d.spec.DeviceID, Epoch: d.spec.Epoch,
		CounterFrom: d.counter + 1, CounterTo: d.counter + int64(n), KeyID: d.keyID, NTPOffsetMs: &zero,
	}
	b.Readings = make([]edgewire.Reading, n)
	for i := range b.Readings {
		d.value += int64((i%7)-3) * 5 // trôi ±15 g, không float (R-10)
		b.Readings[i] = edgewire.Reading{
			Counter:    b.CounterFrom + int64(i),
			DeviceTime: edgewire.FormatDeviceTime(now.Add(time.Duration(i) * 10 * time.Millisecond)),
			ValueKg:    fmt.Sprintf("%d.%03d", d.value/1000, d.value%1000),
			Stable:     true, Unit: edgewire.UnitKg,
		}
	}
	if err := edgewire.Sign(d.priv, &b); err != nil {
		return b, err
	}
	return b, b.Validate()
}

// Batch là một lượt ingest: chọn thiết bị rảnh (mỗi thiết bị một lô đang bay — bộ đếm phải tăng dần).
func (in *Ingest) Batch(ctx context.Context, rec *Recorder) bool {
	var d *device
	for range in.devs {
		c := in.devs[int(in.next.Add(1)-1)%len(in.devs)]
		if c.mu.TryLock() {
			d = c
			break
		}
	}
	if d == nil {
		return false
	}
	defer d.mu.Unlock()
	b, err := d.BuildBatch(in.BatchSize, time.Now())
	if err != nil {
		rec.Observe("ingest_batch", 0, "build", false)
		return true
	}
	body, _ := json.Marshal(b)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.URL, bytes.NewReader(body))
	if err != nil {
		rec.Observe("ingest_batch", 0, "build", false)
		return true
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := d.client.Do(req)
	st := 0
	if err == nil {
		st = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}
	if observeHTTP(rec, "ingest_batch", start, st, err, func(s int) bool { return s == http.StatusOK }) {
		d.counter = b.CounterTo
		rec.Count("ingest.readings", int64(len(b.Readings)))
	} else if st == http.StatusConflict {
		// Lô trùng/lùi bộ đếm: nhảy qua để lượt sau không lặp lại lỗi.
		d.counter = b.CounterTo
	}
	return true
}
