package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Metric (cardinality thấp — R-91; không có mã khách hàng/webhook trong thuộc tính).
const (
	MetricDeliveries = "ag.apiclient.webhook.deliveries"        // outcome, error_class
	MetricDuration   = "ag.apiclient.webhook.delivery.duration" // giây
	MetricBreaker    = "ag.apiclient.webhook.breaker.transitions"
)

// Kết cục một lần giao (sổ webhook_delivery).
const (
	OutcomeDelivered = "delivered"
	OutcomeFailed    = "failed"
	OutcomeBlocked   = "blocked"
)

// Lớp lỗi giao.
const (
	ErrClassHTTPStatus  = "http_status"
	ErrClassRedirect    = "redirect"
	ErrClassTimeout     = "timeout"
	ErrClassDNS         = "dns"
	ErrClassTLS         = "tls"
	ErrClassConnect     = "connect"
	ErrClassBlockedAddr = "blocked_address"
	ErrClassURLRejected = "url_rejected"
)

// Hint là thân webhook — CHỈ báo cursor, không bao giờ có dữ liệu nghiệp vụ (ADR-0033 §4).
type Hint struct {
	Feed       string `json:"feed"`
	Cursor     string `json:"cursor"`
	OccurredAt string `json:"occurred_at"`
}

// Dispatcher giao chỉ báo cursor tới webhook đã đăng ký (job apiclient_webhook_dispatch).
type Dispatcher struct {
	DB             tenant.Beginner // pool bọc vai r_apiclient
	Keys           *Keyring
	Guard          Guard
	Client         *http.Client // nil → Guard.Client(ClientOptions{Timeout: Timeout, TLS: TLS})
	Timeout        time.Duration
	TLS            *tls.Config
	Policy         Policy
	OperatorTenant uuid.UUID
	Concurrency    int // số webhook giao song song (mặc định 8) — đầu nhận chậm không chặn đối tác khác
	Clock          clock.Clock
	Jitter         func() float64
	Log            *slog.Logger

	once     sync.Once
	client   *http.Client
	cDeliver metric.Int64Counter
	cBreaker metric.Int64Counter
	hDur     metric.Float64Histogram
}

func (d *Dispatcher) init() {
	d.once.Do(func() {
		d.client = d.Client
		if d.client == nil {
			d.client = d.Guard.Client(ClientOptions{Timeout: d.Timeout, TLS: d.TLS})
		}
		if d.Log == nil {
			d.Log = slog.Default()
		}
		if d.Clock == nil {
			d.Clock = clock.System{}
		}
		m := telemetry.Meter()
		d.cDeliver, _ = m.Int64Counter(MetricDeliveries, metric.WithDescription("Lần giao webhook theo kết cục"))
		d.cBreaker, _ = m.Int64Counter(MetricBreaker, metric.WithDescription("Chuyển trạng thái ngắt mạch webhook"))
		d.hDur, _ = m.Float64Histogram(MetricDuration, metric.WithDescription("Thời gian một lần giao webhook"), metric.WithUnit("s"))
	})
}

// RunResult tóm tắt một lượt giao.
type RunResult struct {
	Delivered, Failed, Blocked, Deferred int
	Opened, Closed                       int
}

func (r *RunResult) add(o RunResult) {
	r.Delivered += o.Delivered
	r.Failed += o.Failed
	r.Blocked += o.Blocked
	r.Deferred += o.Deferred
	r.Opened += o.Opened
	r.Closed += o.Closed
}

// RunOnce giao mọi chỉ báo đến hạn của tenant đơn vị vận hành.
func (d *Dispatcher) RunOnce(ctx context.Context) (RunResult, error) {
	d.init()
	var res RunResult
	if d.OperatorTenant == uuid.Nil || d.Keys == nil {
		return res, ErrNoKeyring
	}
	var all []pending
	if err := tenant.WithTenantTx(ctx, d.DB, d.OperatorTenant, func(tx pgx.Tx) error {
		var err error
		all, err = pendingAll(ctx, tx)
		return err
	}); err != nil {
		return res, err
	}
	groups := map[uuid.UUID][]pending{}
	var order []uuid.UUID
	for _, p := range all {
		if _, ok := groups[p.WebhookID]; !ok {
			order = append(order, p.WebhookID)
		}
		groups[p.WebhookID] = append(groups[p.WebhookID], p)
	}
	n := d.Concurrency
	if n <= 0 {
		n = 8
	}
	sem := make(chan struct{}, n)
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		first error
	)
	for _, id := range order {
		wg.Add(1)
		sem <- struct{}{}
		go func(id uuid.UUID, items []pending) {
			defer func() { <-sem; wg.Done() }()
			r, err := d.runWebhook(ctx, id, items)
			mu.Lock()
			defer mu.Unlock()
			res.add(r)
			if err != nil && first == nil {
				first = err
			}
		}(id, groups[id])
	}
	wg.Wait()
	return res, first
}

func (d *Dispatcher) runWebhook(ctx context.Context, id uuid.UUID, items []pending) (RunResult, error) {
	var res RunResult
	var reg *Registration
	if err := tenant.WithTenantTx(ctx, d.DB, d.OperatorTenant, func(tx pgx.Tx) error {
		var err error
		if reg, err = regByIDAny(ctx, tx, id, false); err != nil || reg == nil {
			return err
		}
		// Scope feed:<tên>:read bị thu hồi sau khi đăng ký → không báo feed đó nữa.
		scopes, err := store.ActiveScopes(ctx, tx, reg.ClientID)
		if err != nil {
			return err
		}
		kept := items[:0]
		for _, it := range items {
			if slices.Contains(scopes, port.FeedScope(it.Feed)) {
				kept = append(kept, it)
			}
		}
		items = kept
		return nil
	}); err != nil || reg == nil || reg.Status != StatusActive {
		return res, err
	}
	for i, it := range items {
		now := d.Clock.Now()
		probe := false
		switch reg.Breaker.Decide(now) {
		case Deny:
			res.Deferred += len(items) - i
			return res, nil
		case Probe:
			ok, err := d.startProbe(ctx, id, now)
			if err != nil || !ok {
				return res, err
			}
			probe = true
		default:
			if it.NextAttemptAt != nil && now.Before(*it.NextAttemptAt) {
				res.Deferred++
				continue
			}
		}
		out := d.deliver(ctx, reg, it, now)
		if out.outcome == "" {
			return res, nil
		}
		upd, r, err := d.record(ctx, id, it, probe, out)
		if err != nil {
			return res, err
		}
		res.add(r)
		if upd == nil {
			return res, nil
		}
		reg = upd
	}
	return res, nil
}

// startProbe chuyển mạch sang half_open dưới khóa dòng (một lần thử mỗi hạn).
func (d *Dispatcher) startProbe(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	ok := false
	err := tenant.WithTenantTx(ctx, d.DB, d.OperatorTenant, func(tx pgx.Tx) error {
		r, err := regByIDAny(ctx, tx, id, true)
		if err != nil || r == nil || r.Breaker.Decide(now) != Probe {
			return err
		}
		r.Breaker.StartProbe(now, d.Policy)
		ok = true
		return putBreaker(ctx, tx, r.TenantID, id, r.Breaker)
	})
	if ok {
		d.cBreaker.Add(ctx, 1, metric.WithAttributes(attribute.String("to", StateHalfOpen)))
	}
	return ok, err
}

// attemptOut là kết quả HTTP của một lần giao.
type attemptOut struct {
	id      uuid.UUID
	outcome string
	status  *int
	class   string
	latency time.Duration
	at      time.Time
}

func (d *Dispatcher) secrets(r *Registration, now time.Time) [][]byte {
	var out [][]byte
	if s, ok := d.Keys.Secret(r.KeyVersion, r.ID, r.SecretVersion, r.Nonce); ok {
		out = append(out, s)
	}
	if r.PrevNonce != nil && r.PrevValidUntil != nil && now.Before(*r.PrevValidUntil) {
		if s, ok := d.Keys.Secret(*r.PrevKeyVersion, r.ID, *r.PrevVersion, *r.PrevNonce); ok {
			out = append(out, s)
		}
	}
	return out
}

func (d *Dispatcher) deliver(ctx context.Context, r *Registration, it pending, now time.Time) attemptOut {
	out := attemptOut{id: ids.New(), at: now}
	if _, err := d.Guard.CheckStatic(r.URL); err != nil {
		out.outcome, out.class = OutcomeBlocked, ErrClassURLRejected
		return out
	}
	secrets := d.secrets(r, now)
	if len(secrets) == 0 {
		// Thiếu khóa chủ của phiên bản bí mật — lỗi vận hành phía nền tảng, không phải của đối tác.
		d.Log.ErrorContext(ctx, "webhook: thiếu khóa chủ để ký", "event", "apiclient.webhook_key_missing", "webhook_id", r.ID,
			"key_version", r.KeyVersion, "outcome", "error")
		return out // outcome rỗng: bỏ qua, không tính vào ngắt mạch
	}
	occurred := now
	if it.HeadAt != nil {
		occurred = *it.HeadAt
	}
	body, _ := json.Marshal(Hint{Feed: it.Feed, Cursor: domain.EncodeCursor(it.HeadSeq), OccurredAt: occurred.UTC().Format(time.RFC3339Nano)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(body))
	if err != nil {
		out.outcome, out.class = OutcomeBlocked, ErrClassURLRejected
		return out
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AgriGreen-Webhook/1")
	req.Header.Set(HeaderID, out.id.String())
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(HeaderSignature, Sign(secrets, now, body))
	start := time.Now()
	resp, err := d.client.Do(req)
	out.latency = time.Since(start)
	if err != nil {
		out.outcome, out.class = OutcomeFailed, classify(err)
		if out.class == ErrClassBlockedAddr {
			out.outcome = OutcomeBlocked
		}
		return out
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	st := resp.StatusCode
	out.status = &st
	switch {
	case st >= 200 && st <= 299:
		out.outcome = OutcomeDelivered
	case st >= 300 && st <= 399:
		out.outcome, out.class = OutcomeFailed, ErrClassRedirect
	default:
		out.outcome, out.class = OutcomeFailed, ErrClassHTTPStatus
	}
	return out
}

func classify(err error) string {
	if errors.Is(err, ErrBlockedAddress) {
		return ErrClassBlockedAddr
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrClassDNS
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrClassTimeout
	}
	var certErr *tls.CertificateVerificationError
	var recErr tls.RecordHeaderError
	if errors.As(err, &certErr) || errors.As(err, &recErr) {
		return ErrClassTLS
	}
	return ErrClassConnect
}

// record ghi lần giao: sổ webhook_delivery, cursor (thành công → seq đã báo; lỗi → lùi), ngắt mạch. Trả bảng chiếu
// mới (nil khi webhook không còn active).
func (d *Dispatcher) record(ctx context.Context, id uuid.UUID, it pending, probe bool, out attemptOut) (*Registration, RunResult, error) {
	var (
		res     RunResult
		reg     *Registration
		changed bool
	)
	err := tenant.WithTenantTx(ctx, d.DB, d.OperatorTenant, func(tx pgx.Tx) error {
		r, err := regByIDAny(ctx, tx, id, true)
		if err != nil || r == nil {
			return err
		}
		cur := Cursor{Feed: it.Feed, NotifiedSeq: it.NotifiedSeq, LastAttemptAt: &out.at}
		if out.outcome == OutcomeDelivered {
			cur.NotifiedSeq = it.HeadSeq
			changed = r.Breaker.Success(out.at)
		} else {
			cur.Attempts = it.Attempts + 1
			next := out.at.Add(d.Policy.Backoff(cur.Attempts, d.Jitter))
			cur.NextAttemptAt = &next
			changed = r.Breaker.Failure(out.at, out.class, r.Threshold, d.Policy)
		}
		ms := int(out.latency / time.Millisecond)
		if err := insertDelivery(ctx, tx, delivery{ID: out.id, TenantID: r.TenantID, WebhookID: id, ClientID: r.ClientID, Feed: it.Feed,
			CursorSeq: it.HeadSeq, Attempt: it.Attempts + 1, Probe: probe, Outcome: out.outcome, HTTPStatus: out.status, ErrorClass: out.class,
			LatencyMS: ms, BreakerState: r.Breaker.State, At: out.at}); err != nil {
			return err
		}
		if err := putCursor(ctx, tx, r.TenantID, id, cur); err != nil {
			return err
		}
		if err := putBreaker(ctx, tx, r.TenantID, id, r.Breaker); err != nil {
			return err
		}
		if r.Status == StatusActive {
			reg = r
		}
		return nil
	})
	if err != nil {
		return nil, res, err
	}
	attrs := []attribute.KeyValue{attribute.String("outcome", out.outcome)}
	if out.class != "" {
		attrs = append(attrs, attribute.String("error_class", out.class))
	}
	d.cDeliver.Add(ctx, 1, metric.WithAttributes(attrs...))
	d.hDur.Record(ctx, out.latency.Seconds(), metric.WithAttributes(attribute.String("outcome", out.outcome)))
	switch out.outcome {
	case OutcomeDelivered:
		res.Delivered++
	case OutcomeBlocked:
		res.Blocked++
	default:
		res.Failed++
	}
	if reg != nil && changed {
		d.cBreaker.Add(ctx, 1, metric.WithAttributes(attribute.String("to", reg.Breaker.State)))
		if reg.Breaker.State == StateOpen {
			res.Opened++
			// Báo đối tác qua kênh quản trị (ADR-0033 §4): sự kiện log cho cảnh báo + trạng thái ở Admin API và
			// GET /v1/webhooks. Không mất dữ liệu — đối tác đọc tiếp từ cursor của mình.
			d.Log.WarnContext(ctx, "webhook: ngắt mạch mở", "event", "apiclient.webhook_breaker_opened", "webhook_id", id,
				"client_id", reg.ClientID, "host", reg.Host, "failures", reg.Breaker.Failures, "error_class", out.class,
				"next_probe_at", reg.Breaker.NextProbeAt, "outcome", "degraded")
		} else {
			res.Closed++
			d.Log.InfoContext(ctx, "webhook: ngắt mạch đóng", "event", "apiclient.webhook_breaker_closed", "webhook_id", id,
				"client_id", reg.ClientID, "host", reg.Host, "outcome", "ok")
		}
	}
	return reg, res, nil
}
