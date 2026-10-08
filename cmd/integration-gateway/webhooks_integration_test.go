package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// hookSink là đầu nhận webhook của đối tác (httptest TLS) — ghi lại từng lời gọi, trả mã do kiểm thử đặt.
type hookSink struct {
	mu     sync.Mutex
	status int
	calls  []hookCall
}

type hookCall struct {
	header http.Header
	body   []byte
}

func (h *hookSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, hookCall{header: r.Header.Clone(), body: b})
	w.WriteHeader(h.status)
}

func (h *hookSink) set(status int) { h.mu.Lock(); h.status = status; h.mu.Unlock() }

func (h *hookSink) n() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.calls) }

func (h *hookSink) last() hookCall { h.mu.Lock(); defer h.mu.Unlock(); return h.calls[len(h.calls)-1] }

// advanceFeed nối một mục retract vào feed (như job dựng) để đầu feed tiến — trả seq mới.
func (e *intEnv) advanceFeed(feed string) int64 {
	e.t.Helper()
	ctx := context.Background()
	var seq int64
	owner := e.pg.Pool(e.t, migrations.PF, dbadmin.Migrator)
	if err := tenant.WithTenantTx(ctx, owner, e.operator, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT last_seq + 1 FROM pf_apiclient.feed_head WHERE feed = $1 AND partition_key = '' FOR UPDATE`, feed).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.feed_entry (tenant_id, feed, partition_key, seq, event_id, entity_key, op,
		    contract_version, recorded_at) VALUES ($1, $2, '', $3, $4, $5, 'retract', 1, now())`, e.operator, feed, seq, ids.New(), "w57:"+strconv.FormatInt(seq, 10)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE pf_apiclient.feed_head SET last_seq = $2, last_recorded_at = now() WHERE feed = $1 AND partition_key = ''`, feed, seq)
		return err
	}); err != nil {
		e.t.Fatalf("nối mục feed: %v", err)
	}
	return seq
}

// W5.7 (R-INT-07, R-INT-09, R-INT-10; ADR-0033 §4) trên cổng THẬT + Admin API + DB thật:
//   - đăng ký qua Admin API: URL bị chặn SSRF (http, IP nội bộ) → 422; feed chưa có scope → 422; hợp lệ → 201 + bí mật
//     MỘT lần;
//   - feed tiến → job giao gửi thân CHỈ {feed, cursor, occurred_at} tới httptest TLS, chữ ký HMAC xác minh được, cursor
//     đúng next_cursor của GET /v1/feeds; đã báo → không gửi lại;
//   - đầu nhận lỗi: lùi rồi thử lại; N = 3 lỗi liên tiếp → mạch mở (GET /v1/webhooks thấy open), không gửi khi mở;
//     hết thời gian chờ → MỘT lần thử half-open thành công → đóng;
//   - xoay bí mật: chồng lấn ký cả hai; tắt → không giao;
//   - GET /v1/feeds/{feed}/checksums: số bản ghi + sha256 = tổng kiểm của phiên bản (ảnh chụp) trong trang feed.
func TestWebhookDeliveryBreakerChecksums_W57Integration(t *testing.T) {
	e := newIntEnv(t)
	e.seedCollection()
	e.buildFeeds()
	cid, key := e.onboardFeed()

	keys, err := apiclient.NewWebhookKeyring(1, map[int][]byte{1: []byte(strings.Repeat("w", 32))})
	if err != nil {
		t.Fatal(err)
	}
	devGuard := apiclient.WebhookGuard{AllowNets: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowAnyPort: true}
	e.mod.UseWebhooks(keys, devGuard)

	sink := &hookSink{status: http.StatusNoContent}
	srv := httptest.NewTLSServer(sink)
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	ad := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin)
	base := "/v1/api-clients/" + cid + "/webhooks"
	for _, c := range []struct{ url, code string }{
		{strings.Replace(srv.URL, "https://", "http://", 1) + "/hook", "apiclient.webhook_url_rejected"},
		{"https://10.0.0.7/hook", "apiclient.webhook_url_rejected"},
		{"https://169.254.169.254/latest/meta-data", "apiclient.webhook_url_rejected"},
	} {
		r := ad(http.MethodPost, base, map[string]any{"url": c.url, "feeds": []string{feedHP}})
		if r.status != http.StatusUnprocessableEntity || r.code() != c.code {
			t.Fatalf("%s → 422 %s: %d %s", c.url, c.code, r.status, r.raw)
		}
	}
	r := ad(http.MethodPost, base, map[string]any{"url": srv.URL + "/hook", "feeds": []string{"admin_units"}})
	if r.status != http.StatusUnprocessableEntity || r.code() != "apiclient.webhook_feed_not_subscribed" {
		t.Fatalf("feed chưa có scope → 422: %d %s", r.status, r.raw)
	}
	r = ad(http.MethodPost, base, map[string]any{"url": srv.URL + "/hook", "feeds": []string{feedHP}, "failure_threshold": 3})
	want(t, r, http.StatusCreated, "đăng ký webhook")
	secretStr, _ := r.body["secret"].(string)
	secret, err := apiclient.ParseWebhookSecret(secretStr)
	if err != nil || r.header.Get("Cache-Control") != "no-store" || strings.Contains(string(r.raw), "nonce") {
		t.Fatalf("bí mật whsec_ một lần, không lộ nonce: %v %s", err, r.raw)
	}
	whID := r.body["id"].(string)
	if br := r.body["breaker"].(map[string]any); br["state"] != "closed" {
		t.Fatalf("mạch ban đầu đóng: %s", r.raw)
	}

	clk := clock.NewFixed(time.Now().UTC())
	d := e.mod.WebhookDispatcher(apiclient.WebhookConfig{Keys: keys, Guard: devGuard, TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		Policy: apiclient.WebhookPolicy{Cooldown: time.Minute, RetryBase: time.Second, RetryMax: 4 * time.Second}, Jitter: func() float64 { return 0 }})
	d.Clock = clk
	run := func() apiclient.WebhookRunResult {
		t.Helper()
		res, err := d.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		return res
	}

	// 1. Chỉ báo đầu tiên: thân chỉ ba trường, chữ ký đúng, cursor = next_cursor của feed.
	if res := run(); res.Delivered != 1 || sink.n() != 1 {
		t.Fatalf("giao chỉ báo đầu: %+v, %d lời gọi", res, sink.n())
	}
	call := sink.last()
	var hint map[string]any
	if err := json.Unmarshal(call.body, &hint); err != nil || len(hint) != 3 || hint["feed"] != feedHP || hint["cursor"] == "" || hint["occurred_at"] == nil {
		t.Fatalf("thân webhook CHỈ {feed, cursor, occurred_at}: %s", call.body)
	}
	if err := apiclient.VerifyWebhookSignature(call.header.Get(apiclient.WebhookHeaderSignature), secret, call.body, clk.Now(), 0); err != nil {
		t.Fatalf("chữ ký: %v (%s)", err, call.header.Get(apiclient.WebhookHeaderSignature))
	}
	if _, err := uuid.Parse(call.header.Get(apiclient.WebhookHeaderID)); err != nil || call.header.Get(apiclient.WebhookHeaderTimestamp) == "" {
		t.Fatalf("header mã lần giao + dấu thời gian: %v", call.header)
	}
	dAllow := e.deps
	dAllow.AllowUnapproved = true
	feedSrv := httptest.NewServer(gateway.NewHandler(e.mod, dAllow, nil, e.log))
	t.Cleanup(feedSrv.Close)
	get := func(path string) intResp {
		return doReq(t, http.MethodGet, feedSrv.URL+path, map[string]string{apiclientport.KeyHeader: key}, nil)
	}
	page := get("/v1/feeds/" + feedHP)
	want(t, page, http.StatusOK, "đọc feed")
	if page.body["next_cursor"] != hint["cursor"] {
		t.Fatalf("cursor chỉ báo = next_cursor sau trang cuối: %v vs %s", hint["cursor"], page.raw)
	}
	if res := run(); res.Delivered+res.Failed != 0 || sink.n() != 1 {
		t.Fatalf("đã báo → không gửi lại: %+v", res)
	}

	// 2. Đầu nhận lỗi: lùi rồi thử lại; lỗi thứ 3 → mạch mở.
	sink.set(http.StatusServiceUnavailable)
	e.advanceFeed(feedHP)
	if res := run(); res.Failed != 1 {
		t.Fatalf("lỗi 1: %+v", res)
	}
	if res := run(); res.Failed != 0 || res.Deferred != 1 {
		t.Fatalf("chưa hết thời gian lùi → hoãn: %+v", res)
	}
	clk.Advance(5 * time.Second)
	run()
	clk.Advance(5 * time.Second)
	if res := run(); res.Failed != 1 || res.Opened != 1 {
		t.Fatalf("lỗi thứ N = 3 → mạch mở: %+v", res)
	}
	r = e.call(http.MethodGet, "/v1/webhooks", key, nil)
	want(t, r, http.StatusOK, "đối tác xem webhook")
	items := r.body["items"].([]any)
	br := items[0].(map[string]any)["breaker"].(map[string]any)
	if len(items) != 1 || br["state"] != "open" || br["consecutive_failures"] != float64(3) || br["last_error"] != "http_status" || br["next_probe_at"] == nil {
		t.Fatalf("trạng thái ngắt mạch qua API: %s", r.raw)
	}
	if strings.Contains(string(r.raw), "secret\"") || strings.Contains(string(r.raw), "nonce") {
		t.Fatalf("không lộ bí mật: %s", r.raw)
	}
	before := sink.n()
	clk.Advance(30 * time.Second)
	if res := run(); res.Deferred == 0 || sink.n() != before {
		t.Fatalf("mạch mở, chưa hết thời gian chờ → không gửi: %+v", res)
	}

	// 3. Hết thời gian chờ: một lần thử half-open thành công → đóng.
	sink.set(http.StatusOK)
	clk.Advance(31 * time.Second)
	if res := run(); res.Delivered != 1 || res.Closed != 1 || sink.n() != before+1 {
		t.Fatalf("thử half-open → đóng: %+v", res)
	}
	r = ad(http.MethodGet, base, nil)
	want(t, r, http.StatusOK, "Admin API: danh sách webhook")
	w0 := r.body["items"].([]any)[0].(map[string]any)
	ds := w0["recent_deliveries"].([]any)
	first := ds[0].(map[string]any)
	if w0["breaker"].(map[string]any)["state"] != "closed" || first["probe"] != true || first["outcome"] != "delivered" || len(ds) != 5 {
		t.Fatalf("sổ giao: thử half-open delivered + 3 lỗi + 1 đầu: %s", r.raw)
	}

	// 4. Xoay bí mật: chồng lấn ký cả hai.
	r = ad(http.MethodPost, base+"/"+whID+"/rotate-secret", nil)
	want(t, r, http.StatusOK, "xoay bí mật")
	secret2, err := apiclient.ParseWebhookSecret(r.body["secret"].(string))
	if err != nil || string(secret2) == string(secret) || r.body["previous_secret_valid_until"] == nil {
		t.Fatalf("bí mật mới + hạn bí mật cũ: %v %s", err, r.raw)
	}
	e.advanceFeed(feedHP)
	if res := run(); res.Delivered != 1 {
		t.Fatalf("giao sau xoay: %+v", res)
	}
	call = sink.last()
	sig := call.header.Get(apiclient.WebhookHeaderSignature)
	if strings.Count(sig, "v1=") != 2 || apiclient.VerifyWebhookSignature(sig, secret2, call.body, clk.Now(), 0) != nil ||
		apiclient.VerifyWebhookSignature(sig, secret, call.body, clk.Now(), 0) != nil {
		t.Fatalf("chồng lấn: hai chữ ký, cả hai bí mật xác minh được: %s", sig)
	}

	// 5. Tắt → không giao; thiếu lý do → 422.
	if r = ad(http.MethodPost, base+"/"+whID+"/disable", map[string]any{"reason": "x"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("tắt thiếu lý do: %d %s", r.status, r.raw)
	}
	want(t, ad(http.MethodPost, base+"/"+whID+"/disable", map[string]any{"reason": "Đối tác bảo trì đầu nhận webhook"}), http.StatusOK, "tắt webhook")
	e.advanceFeed(feedHP)
	n := sink.n()
	if res := run(); res.Delivered+res.Failed != 0 || sink.n() != n {
		t.Fatalf("webhook tắt → không giao: %+v", res)
	}
	if c := e.count(e.operator, `SELECT count(*) FROM pf_apiclient.webhook_event WHERE webhook_id = $1::uuid`, uuid.MustParse(whID)); c != 3 {
		t.Fatalf("sổ đăng ký: registered + secret_rotated + disabled, nhận %d", c)
	}

	// 6. Đối soát: tổng kiểm theo phiên bản = tổng kiểm của trang feed (sha256 ảnh chụp).
	r = get("/v1/feeds/" + feedHP + "/checksums")
	want(t, r, http.StatusOK, "tổng kiểm feed")
	vs := r.body["versions"].([]any)
	v1 := vs[0].(map[string]any)
	ck := page.body["dataset"].(map[string]any)["checksum"].(map[string]any)
	if r.body["kind"] != "dataset_version" || len(vs) != 1 || v1["version"] != float64(1) || v1["sha256"] != ck["sha256"] ||
		v1["record_count"] != ck["record_count"] || r.body["next_from_version"] != nil {
		t.Fatalf("tổng kiểm phiên bản khớp trang feed: %s vs %v", r.raw, ck)
	}
	if r = get("/v1/feeds/" + feedHP + "/checksums?limit=0"); r.status != http.StatusBadRequest && r.status != http.StatusUnprocessableEntity {
		t.Fatalf("limit sai: %d %s", r.status, r.raw)
	}
	if r = get("/v1/feeds/no_such_feed/checksums"); r.status != http.StatusNotFound {
		t.Fatalf("feed lạ → 404: %d", r.status)
	}
	_, _, otherKey := e.onboard(100)
	if r = doReq(t, http.MethodGet, feedSrv.URL+"/v1/feeds/"+feedHP+"/checksums", map[string]string{apiclientport.KeyHeader: otherKey}, nil); r.status != http.StatusForbidden {
		t.Fatalf("thiếu scope feed → 403: %d %s", r.status, r.raw)
	}
	r = e.call(http.MethodGet, "/v1/webhooks", otherKey, nil)
	if r.status != http.StatusOK || len(r.body["items"].([]any)) != 0 {
		t.Fatalf("khách hàng khác không thấy webhook: %s", r.raw)
	}
}

// W5.7 (R-INT-10, ADR-0034): tổng kiểm theo NGÀY của luồng song phương (tạm trên sổ đệm tới W5.6): số bản ghi + băm
// theo công thức bilateral_day.v1 của chính bên gửi; đối tác khác không thấy; khoảng > 31 ngày → 400.
func TestBilateralDailyChecksums_W57Integration(t *testing.T) {
	e := newIngestEnv(t, true, 0, nil)
	partner, other := ids.New(), ids.New()
	_, key := e.onboardIngest(partner, "roll_count_statement")
	_, key2 := e.onboardIngest(other, "roll_count_statement")
	refA, refB, refC := ids.New().String(), ids.New().String(), ids.New().String()
	want(t, e.post(key, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(),
		item(refA, rollRecord(refA, 1, 46)), item(refB, rollRecord(refB, 1, 12)))), http.StatusAccepted, "lô đối tác 1")
	want(t, e.post(key2, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(),
		item(refC, rollRecord(refC, 1, 9)))), http.StatusAccepted, "lô đối tác 2")
	e.drain()

	today := clock.BusinessDate(time.Now()).String()
	r := e.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?from="+today+"&to="+today, key, nil)
	want(t, r, http.StatusOK, "tổng kiểm ngày")
	days := r.body["days"].([]any)
	day := days[0].(map[string]any)
	// Tính lại từ sổ đệm theo đúng công thức (bên kia của cặp làm cùng phép tính trên bản ghi của mình).
	var lines []string
	owner := e.pg.Pool(t, migrations.PF, dbadmin.Migrator)
	if err := tenant.WithTenantTx(context.Background(), owner, partner, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT business_key, contract_version, payload_sha256 FROM pf_apiclient.ingest_staging`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var bk, sum string
			var v int
			if err := rows.Scan(&bk, &v, &sum); err != nil {
				return err
			}
			lines = append(lines, bk+"\t"+strconv.Itoa(v)+"\t"+sum+"\n")
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "")))
	if r.body["kind"] != "bilateral_day" || r.body["algorithm"] != "bilateral_day.v1" || len(days) != 1 || day["date"] != today ||
		day["record_count"] != float64(2) || day["sha256"] != hex.EncodeToString(h[:]) {
		t.Fatalf("tổng kiểm ngày của bên gửi (2 bản ghi, không gồm đối tác khác): %s", r.raw)
	}
	if r = e.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?from=2026-01-01&to=2026-03-01", key, nil); r.status != http.StatusUnprocessableEntity && r.status != http.StatusBadRequest {
		t.Fatalf("> 31 ngày → lỗi kiểm tra: %d %s", r.status, r.raw)
	}
	if r = e.call(http.MethodGet, "/v1/feeds/collection_schedule/checksums", key, nil); r.status != http.StatusForbidden {
		t.Fatalf("thiếu scope hợp đồng → 403: %d %s", r.status, r.raw)
	}
}
