package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Giao thức đẩy lô tới cmd/ingest (đề xuất của agent; cmd/ingest phải khớp):
//
//	POST <ingest.url>   (mặc định đường dẫn /v1/ingest/batches), mTLS, Content-Type: application/json
//	thân: một lô agrigreen.edge.batch.v1 (contracts/edge/batch.v1.json)
//
//	200/201 → lô đã commit (kể cả gửi lặp idempotent). Thân JSON TÙY CHỌN; nếu có
//	          "counter_to" / "payload_sha256" thì phải khớp lô đã gửi, không khớp → báo động.
//	409     → cùng khóa (device_id, epoch, counter_from) khác payload, hoặc kỳ bị thay → báo động, không gỡ đệm.
//	400/413/422 → lô sai hợp đồng/chữ ký → báo động, không gỡ đệm.
//	401/403 → chứng thư/thiết bị chưa duyệt hoặc bị thu hồi → báo động.
//	408/425/429/5xx, lỗi mạng → thử lại theo lùi lũy thừa có jitter (tôn trọng Retry-After).
//
// Không bao giờ bỏ qua một lô: mọi lỗi đều thử lại (lỗi không tạm thời thử ở nhịp
// backoff_max) cho tới khi được xác nhận, để không có khoảng trống bộ đếm.

// pushOutcome là kết quả một lần đẩy.
type pushOutcome int

const (
	pushAcked pushOutcome = iota
	pushRetry
	pushRejected
	pushConflict
)

// ackBody là thân phản hồi tùy chọn của ingest.
type ackBody struct {
	CounterTo     *int64 `json:"counter_to"`
	PayloadSHA256 string `json:"payload_sha256"`
}

// pendingBatch là lô đã ký chờ xác nhận.
type pendingBatch struct {
	batch edgewire.Batch
	body  []byte
	sha   [32]byte
}

// Sender đóng lô từ WAL, ký, đẩy, xử lý xác nhận.
type Sender struct {
	cfg    *Config
	store  *Store
	key    ed25519.PrivateKey
	ntp    *NTPTracker
	m      *Metrics
	clk    clock.Clock
	client *http.Client
	log    *slog.Logger
	rng    *rand.Rand
	// sleep chờ d hoặc ctx hủy (thay được trong kiểm thử).
	sleep   func(ctx context.Context, d time.Duration) error
	wake    chan struct{}
	pending *pendingBatch
	attempt int
}

// NewSender tạo bộ gửi. client nil → dựng client mTLS từ cfg.Ingest.
func NewSender(cfg *Config, store *Store, key ed25519.PrivateKey, ntp *NTPTracker, m *Metrics, clk clock.Clock, client *http.Client, log *slog.Logger) (*Sender, error) {
	if client == nil {
		var err error
		if client, err = NewMTLSClient(cfg.Ingest); err != nil {
			return nil, err
		}
	}
	var seed [32]byte
	copy(seed[:], key.Seed()) // jitter khác nhau giữa các thiết bị, không cần mật mã
	return &Sender{
		cfg: cfg, store: store, key: key, ntp: ntp, m: m, clk: clk, client: client, log: log,
		rng:   rand.New(rand.NewChaCha8(sha256.Sum256(seed[:]))), //nolint:gosec // jitter, không dùng cho mật mã
		sleep: sleepCtx, wake: make(chan struct{}, 1),
	}, nil
}

// NewMTLSClient dựng HTTP client mTLS (TLS 1.3, CA nội bộ, chứng thư client).
func NewMTLSClient(in IngestConfig) (*http.Client, error) {
	if in.URL == "" {
		return nil, errors.New("thiếu ingest.url")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: in.ServerName}
	if in.CAFile != "" {
		pemb, err := readFile(in.CAFile)
		if err != nil {
			return nil, fmt.Errorf("ingest.ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemb) {
			return nil, errors.New("ingest.ca_file không có chứng thư PEM")
		}
		tc.RootCAs = pool
	}
	if in.CertFile == "" || in.CertKeyFile == "" {
		return nil, errors.New("ingest.cert_file và ingest.cert_key_file bắt buộc (mTLS, ADR-0019)")
	}
	if err := checkKeyPerm(in.CertKeyFile); err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(in.CertFile, in.CertKeyFile)
	if err != nil {
		return nil, fmt.Errorf("chứng thư client mTLS: %w", err)
	}
	tc.Certificates = []tls.Certificate{cert}
	tr := &http.Transport{TLSClientConfig: tc, ForceAttemptHTTP2: true, MaxIdleConns: 2, IdleConnTimeout: 90 * time.Second}
	return &http.Client{Transport: tr, Timeout: in.Timeout.D()}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Notify báo có số đo mới (không chặn).
func (s *Sender) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Backoff trả thời gian chờ cho lần thử thứ attempt (≥1): "equal jitter" —
// exp = min(max, base·2^(attempt−1)); chờ ∈ [exp/2, exp].
func (s *Sender) Backoff(attempt int) time.Duration {
	base, maxD := s.cfg.Ingest.BackoffBase.D(), s.cfg.Ingest.BackoffMax.D()
	exp := base
	for i := 1; i < attempt && exp < maxD; i++ {
		exp *= 2
	}
	exp = min(exp, maxD)
	half := exp / 2
	return half + time.Duration(s.rng.Int64N(int64(exp-half)+1))
}

// BuildBatch đóng lô kế tiếp từ WAL nếu đủ điều kiện (đủ max_readings, hoặc số đo
// cũ nhất đã chờ quá max_wait, hoặc force). nil nếu chưa có gì để gửi.
func (s *Sender) BuildBatch(force bool) (*pendingBatch, error) {
	from := s.store.Acked() + 1
	avail := s.store.LastCounter() - from + 1
	if avail <= 0 {
		return nil, nil
	}
	n := min(int64(s.cfg.Batch.MaxReadings), avail)
	rs, err := s.store.WAL().ReadFrom(from, int(n))
	if err != nil {
		return nil, err
	}
	if int64(len(rs)) != n {
		return nil, fmt.Errorf("%w: đọc được %d/%d số đo từ %d", ErrCounterGap, len(rs), n, from)
	}
	if !force && n < int64(s.cfg.Batch.MaxReadings) {
		t0, err := time.Parse(edgewire.DeviceTimeLayout, rs[0].DeviceTime)
		if err == nil && s.clk.Now().Sub(t0) < s.cfg.Batch.MaxWait.D() {
			return nil, nil
		}
	}
	st := s.store.State()
	b := edgewire.Batch{
		Format: edgewire.Format, DeviceID: st.DeviceID, Epoch: st.Epoch,
		CounterFrom: rs[0].Counter, CounterTo: rs[len(rs)-1].Counter,
		NTPOffsetMs: s.ntp.OffsetMs(), Readings: rs,
	}
	if err := edgewire.Sign(s.key, &b); err != nil {
		return nil, fmt.Errorf("ký lô: %w", err)
	}
	return newPending(b)
}

func newPending(b edgewire.Batch) (*pendingBatch, error) {
	body, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	sha, err := b.PayloadSHA256()
	if err != nil {
		return nil, err
	}
	return &pendingBatch{batch: b, body: body, sha: sha}, nil
}

// Push gửi một lô và phân loại phản hồi.
func (s *Sender) Push(ctx context.Context, p *pendingBatch) (pushOutcome, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Ingest.URL, bytes.NewReader(p.body))
	if err != nil {
		return pushRejected, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("%s/%d/%d", p.batch.DeviceID, p.batch.Epoch, p.batch.CounterFrom))
	resp, err := s.client.Do(req)
	if err != nil {
		return pushRetry, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	retryAfter := time.Duration(0)
	if ra, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && ra > 0 {
		retryAfter = time.Duration(ra) * time.Second
	}
	switch c := resp.StatusCode; {
	case c == http.StatusOK || c == http.StatusCreated:
		var a ackBody
		if len(bytes.TrimSpace(body)) > 0 && json.Unmarshal(body, &a) == nil {
			if a.CounterTo != nil && *a.CounterTo != p.batch.CounterTo {
				return pushConflict, 0, fmt.Errorf("ingest xác nhận counter_to %d, lô là %d", *a.CounterTo, p.batch.CounterTo)
			}
			if a.PayloadSHA256 != "" && a.PayloadSHA256 != hex.EncodeToString(p.sha[:]) {
				return pushConflict, 0, errors.New("ingest xác nhận payload_sha256 khác lô đã gửi")
			}
		}
		return pushAcked, 0, nil
	case c == http.StatusConflict:
		return pushConflict, 0, fmt.Errorf("ingest 409: %s", truncate(body))
	case c == http.StatusRequestTimeout || c == http.StatusTooEarly || c == http.StatusTooManyRequests || c >= 500:
		return pushRetry, retryAfter, fmt.Errorf("ingest %d: %s", c, truncate(body))
	default:
		return pushRejected, retryAfter, fmt.Errorf("ingest %d: %s", c, truncate(body))
	}
}

func truncate(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(bytes.TrimSpace(b))
}

// Step làm một vòng: đóng lô (nếu chưa có), đẩy, xử lý kết quả. Trả thời gian nên chờ trước vòng sau.
func (s *Sender) Step(ctx context.Context) (time.Duration, error) {
	if s.pending == nil {
		p, err := s.BuildBatch(false)
		if err != nil {
			return 0, err
		}
		if p == nil {
			return 200 * time.Millisecond, nil
		}
		// Lưu trước lần gửi đầu: sau sập gửi lại đúng các byte này (cùng payload_sha256).
		if err := s.store.SavePending(p.body); err != nil {
			return 0, fmt.Errorf("lưu lô chờ: %w", err)
		}
		s.pending, s.attempt = p, 0
	}
	p := s.pending
	out, retryAfter, err := s.Push(ctx, p)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if out == pushAcked {
		if err := s.store.Ack(p.batch.CounterTo); err != nil {
			return 0, fmt.Errorf("ghi xác nhận: %w", err)
		}
		if err := s.store.ClearPending(); err != nil {
			return 0, err
		}
		s.pending, s.attempt = nil, 0
		s.m.PushOK.Add(1)
		s.m.BatchesAcked.Add(1)
		s.m.MarkPushOK()
		s.m.Clear(AlarmIngestRejected)
		s.m.Clear(AlarmPayloadConflict)
		s.log.Info("lô đã xác nhận", "counter_from", p.batch.CounterFrom, "counter_to", p.batch.CounterTo)
		return 0, nil
	}
	s.attempt++
	s.m.PushErrors.Add(1)
	wait := s.Backoff(s.attempt)
	switch out {
	case pushConflict:
		s.m.Alarm(AlarmPayloadConflict, err.Error())
		wait = s.cfg.Ingest.BackoffMax.D()
	case pushRejected:
		s.m.Alarm(AlarmIngestRejected, err.Error())
		wait = s.cfg.Ingest.BackoffMax.D()
	}
	wait = max(wait, retryAfter)
	s.log.Warn("đẩy lô lỗi", "counter_from", p.batch.CounterFrom, "attempt", s.attempt, "wait", wait.String(), "err", fmt.Sprint(err))
	return wait, nil
}

// Run chạy vòng gửi tới khi ctx hủy.
func (s *Sender) Run(ctx context.Context) error {
	pb, raw, err := s.store.LoadPending()
	if err != nil {
		return err
	}
	if pb != nil {
		if s.pending, err = newPending(*pb); err != nil {
			return err
		}
		s.pending.body = raw // đúng các byte đã gửi lần đầu
		s.log.Info("gửi lại lô chờ sau khởi động", "counter_from", pb.CounterFrom, "counter_to", pb.CounterTo)
	}
	for {
		wait, err := s.Step(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if wait <= 0 {
			continue
		}
		if s.pending == nil { // chờ số đo mới hoặc hết nhịp
			select {
			case <-ctx.Done():
				return nil
			case <-s.wake:
			case <-time.After(wait):
			}
			continue
		}
		if err := s.sleep(ctx, wait); err != nil {
			return nil
		}
	}
}
