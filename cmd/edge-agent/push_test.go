package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

type vectorFile struct {
	PrivateSeedHex string `json:"private_seed_hex"`
	KeyID          string `json:"key_id"`
	Valid          []struct {
		Name          string          `json:"name"`
		JSON          json.RawMessage `json:"json"`
		Canonical     string          `json:"canonical"`
		PayloadSHAHex string          `json:"payload_sha256_hex"`
		Signature     string          `json:"signature"`
	} `json:"valid"`
}

// Chữ ký do agent tạo (khóa đọc từ tệp PEM, lô dựng từ WAL qua Sender) khớp bộ
// vector đóng băng contracts/edge/batch.v1.vectors.json (ADR-0019 §7).
func TestSignatureMatchesEdgewireVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "edge", "batch.v1.vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	seed, _ := hex.DecodeString(vf.PrivateSeedHex)
	priv := ed25519.NewKeyFromSeed(seed)
	for _, v := range vf.Valid {
		t.Run(v.Name, func(t *testing.T) {
			var want edgewire.Batch
			if err := json.Unmarshal(v.JSON, &want); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			cfg := testConfig(t, dir, 1<<20)
			cfg.DeviceID = want.DeviceID
			der, _ := x509.MarshalPKCS8PrivateKey(priv)
			if err := os.WriteFile(cfg.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
				t.Fatal(err)
			}
			key, err := LoadSourceKey(cfg.KeyFile)
			if err != nil {
				t.Fatal(err)
			}
			// Trạng thái: kỳ và mức xác nhận đặt để counter kế tiếp = counter_from của vector.
			if err := WriteState(dir, State{DeviceID: want.DeviceID, Epoch: want.Epoch, KeyID: vf.KeyID, LastCounter: want.CounterFrom - 1, AckedCounter: want.CounterFrom - 1}); err != nil {
				t.Fatal(err)
			}
			s := openStore(t, cfg, key)
			defer func() { _ = s.Close() }()
			for _, r := range want.Readings {
				if _, err := s.Append(r); err != nil {
					t.Fatal(err)
				}
			}
			ntp := NewNTPTracker(NTPConfig{MaxAge: Duration(time.Hour)}, clock.NewFixed(testT0), quietLog())
			if want.NTPOffsetMs != nil {
				ntp.Set(time.Duration(*want.NTPOffsetMs) * time.Millisecond)
			}
			snd, err := NewSender(cfg, s, key, ntp, NewMetrics(clock.NewFixed(testT0)), clock.NewFixed(testT0), http.DefaultClient, quietLog())
			if err != nil {
				t.Fatal(err)
			}
			p, err := snd.BuildBatch(true)
			if err != nil || p == nil {
				t.Fatalf("build: %v", err)
			}
			c, _ := p.batch.Canonical()
			if string(c) != v.Canonical {
				t.Fatalf("canonical khác vector:\n%s\n---\n%s", c, v.Canonical)
			}
			if hex.EncodeToString(p.sha[:]) != v.PayloadSHAHex || p.batch.Signature != v.Signature || p.batch.KeyID != vf.KeyID {
				t.Fatalf("sha/chữ ký/key_id khác vector")
			}
			if err := edgewire.Verify(priv.Public().(ed25519.PublicKey), p.batch); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func newTestSender(t *testing.T, cfg *Config, s *Store, k ed25519.PrivateKey, clk clock.Clock, client *http.Client) *Sender {
	t.Helper()
	ntp := NewNTPTracker(cfg.NTP, clk, quietLog())
	snd, err := NewSender(cfg, s, k, ntp, NewMetrics(clk), clk, client, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	snd.sleep = func(context.Context, time.Duration) error { return nil }
	return snd
}

// Ranh giới lô: ≤ max_readings, liên tiếp; lô chưa đầy chỉ đóng sau max_wait.
func TestBatchingBoundaries(t *testing.T) {
	cfg, k := enrolled(t, 1<<22)
	s := openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	clk := clock.NewFixed(testT0)
	snd := newTestSender(t, cfg, s, k, clk, http.DefaultClient)
	appendN(t, s, 2500)
	var got [][2]int64
	for {
		p, err := snd.BuildBatch(false)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if err := p.batch.Validate(); err != nil {
			t.Fatal(err)
		}
		got = append(got, [2]int64{p.batch.CounterFrom, p.batch.CounterTo})
		if err := s.Ack(p.batch.CounterTo); err != nil {
			t.Fatal(err)
		}
		if len(got) == 2 {
			// còn 500 số đo: chưa đầy, số đo cũ nhất (device_time = testT0 + …) chưa quá max_wait so với đồng hồ.
			clk.Advance(-time.Hour)
		}
	}
	if len(got) != 2 || got[0] != [2]int64{1, 1000} || got[1] != [2]int64{1001, 2000} {
		t.Fatalf("lô: %v", got)
	}
	clk.Advance(2 * time.Hour) // quá max_wait → đóng lô chưa đầy
	p, err := snd.BuildBatch(false)
	if err != nil || p == nil || p.batch.CounterFrom != 2001 || p.batch.CounterTo != 2500 {
		t.Fatalf("lô cuối: %+v %v", p, err)
	}
	cfg.Batch.MaxReadings = 1
	if err := s.Ack(2500); err != nil {
		t.Fatal(err)
	}
	appendN(t, s, 1)
	if p, _ := snd.BuildBatch(false); p == nil || p.batch.CounterFrom != 2501 || len(p.batch.Readings) != 1 {
		t.Fatal("lô 1 số đo")
	}
}

func TestBackoffExponentialWithJitter(t *testing.T) {
	cfg, k := enrolled(t, 1<<20)
	cfg.Ingest.BackoffBase = Duration(time.Second)
	cfg.Ingest.BackoffMax = Duration(time.Minute)
	s := openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	snd := newTestSender(t, cfg, s, k, clock.NewFixed(testT0), http.DefaultClient)
	seen := map[time.Duration]bool{}
	for attempt := 1; attempt <= 12; attempt++ {
		exp := min(time.Second<<(attempt-1), time.Minute)
		for range 50 {
			d := snd.Backoff(attempt)
			if d < exp/2 || d > exp {
				t.Fatalf("lần %d: %v ngoài [%v, %v]", attempt, d, exp/2, exp)
			}
			seen[d] = true
		}
	}
	if len(seen) < 100 {
		t.Fatal("không có jitter")
	}
}

// fakeIngest là ingest giả: xác minh chữ ký bằng khóa đã đăng ký, trả mã theo kịch bản.
type fakeIngest struct {
	mu     sync.Mutex
	pub    ed25519.PublicKey
	codes  []int // mã trả cho các yêu cầu đầu (rồi 200)
	bodies [][]byte
	acked  map[int64]string // counter_from → sha
	t      *testing.T
}

func (f *fakeIngest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/v1/edge/batches" || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != "scale-hub01-in" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.bodies = append(f.bodies, body)
	b, err := edgewire.Decode(body)
	if err == nil {
		err = edgewire.Verify(f.pub, b)
	}
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if len(f.codes) > 0 {
		c := f.codes[0]
		f.codes = f.codes[1:]
		if c != http.StatusOK {
			w.WriteHeader(c)
			return
		}
	}
	sha, _ := b.PayloadSHA256()
	hs := hex.EncodeToString(sha[:])
	if prev, ok := f.acked[b.CounterFrom]; ok && prev != hs {
		w.WriteHeader(http.StatusConflict)
		return
	}
	f.acked[b.CounterFrom] = hs
	_ = json.NewEncoder(w).Encode(map[string]any{"counter_to": b.CounterTo, "payload_sha256": hs})
}

func startIngest(t *testing.T, cfg *Config, k ed25519.PrivateKey, codes ...int) *fakeIngest {
	t.Helper()
	pki := newTestPKI(t, t.TempDir(), cfg.DeviceID)
	f := &fakeIngest{pub: k.Public().(ed25519.PublicKey), codes: codes, acked: map[int64]string{}, t: t}
	srv := httptest.NewUnstartedServer(f)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pki.Server}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.Pool, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	cfg.Ingest.URL = srv.URL + "/v1/edge/batches"
	cfg.Ingest.CAFile, cfg.Ingest.CertFile, cfg.Ingest.CertKeyFile = pki.CAFile, pki.CertFile, pki.KeyFile
	return f
}

// mTLS đẩy lô: lỗi tạm → thử lại đúng các byte cũ (idempotent); xác nhận → gỡ WAL.
func TestPushRetryIdempotentAndAckTrim(t *testing.T) {
	cfg, k := enrolled(t, 1<<22)
	f := startIngest(t, cfg, k, http.StatusServiceUnavailable, http.StatusBadGateway)
	s := openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	snd := newTestSender(t, cfg, s, k, clock.NewFixed(testT0.Add(time.Hour)), nil)
	appendN(t, s, 1500)
	ctx := context.Background()
	for range 3 {
		if _, err := snd.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s.Acked() != 1000 || len(f.bodies) != 3 {
		t.Fatalf("acked %d, yêu cầu %d", s.Acked(), len(f.bodies))
	}
	if !bytes.Equal(f.bodies[0], f.bodies[1]) || !bytes.Equal(f.bodies[1], f.bodies[2]) {
		t.Fatal("gửi lại phải đúng các byte cũ")
	}
	if snd.m.PushErrors.Load() != 2 {
		t.Fatalf("push errors %d", snd.m.PushErrors.Load())
	}
	if _, err := os.Stat(pendingPath(cfg.DataDir)); !os.IsNotExist(err) {
		t.Fatal("lô chờ chưa được gỡ sau xác nhận")
	}
	if first := s.WAL().First(); first == 1 {
		t.Fatal("WAL chưa gỡ đoạn đã xác nhận")
	}
	if _, err := snd.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Acked() != 1500 {
		t.Fatalf("acked %d", s.Acked())
	}
	st, _ := ReadState(cfg.DataDir)
	if st.AckedCounter != 1500 {
		t.Fatalf("state acked %d — xác nhận phải được fsync vào state.json", st.AckedCounter)
	}
}

// Sập sau khi gửi (ingest đã commit, xác nhận mất): khởi động lại gửi lại đúng lô
// đã lưu (cùng payload_sha256, dù độ lệch NTP đã đổi) → ingest trả idempotent, không 409.
func TestPendingResentAfterRestart(t *testing.T) {
	cfg, k := enrolled(t, 1<<22)
	f := startIngest(t, cfg, k)
	s := openStore(t, cfg, k)
	clk := clock.NewFixed(testT0.Add(time.Hour))
	snd := newTestSender(t, cfg, s, k, clk, nil)
	appendN(t, s, 10)
	p, err := snd.BuildBatch(false)
	if err != nil || p == nil {
		t.Fatal(err)
	}
	if err := s.SavePending(p.body); err != nil {
		t.Fatal(err)
	}
	// ingest nhận và commit, nhưng agent sập trước khi thấy xác nhận.
	if out, _, err := snd.Push(context.Background(), p); out != pushAcked {
		t.Fatal(out, err)
	}
	_ = s.Close()

	s = openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	snd = newTestSender(t, cfg, s, k, clk, nil)
	snd.ntp.Set(1234 * time.Millisecond) // lô dựng lại sẽ khác payload — không được dựng lại
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- snd.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for s.Acked() != 10 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.Acked() != 10 || snd.m.Alarms()[AlarmPayloadConflict] != "" {
		t.Fatalf("acked %d, alarms %v", s.Acked(), snd.m.Alarms())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 2 || !bytes.Equal(f.bodies[0], f.bodies[1]) {
		t.Fatalf("gửi lại %d lần, phải đúng các byte cũ", len(f.bodies))
	}
}

// 409 (cùng khóa khác payload) và 422 (sai hợp đồng) → báo động, KHÔNG gỡ đệm.
func TestPushRejectedRaisesAlarmKeepsBuffer(t *testing.T) {
	for code, alarm := range map[int]string{http.StatusConflict: AlarmPayloadConflict, http.StatusUnprocessableEntity: AlarmIngestRejected, http.StatusForbidden: AlarmIngestRejected} {
		cfg, k := enrolled(t, 1<<22)
		startIngest(t, cfg, k, code)
		s := openStore(t, cfg, k)
		snd := newTestSender(t, cfg, s, k, clock.NewFixed(testT0.Add(time.Hour)), nil)
		appendN(t, s, 5)
		wait, err := snd.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if s.Acked() != 0 || snd.m.Alarms()[alarm] == "" || wait != cfg.Ingest.BackoffMax.D() {
			t.Fatalf("%d: acked %d alarms %v wait %v", code, s.Acked(), snd.m.Alarms(), wait)
		}
		// Ingest hết lỗi → gửi lại, xác nhận, tắt báo động.
		if _, err := snd.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if s.Acked() != 5 || snd.m.Alarms()[alarm] != "" {
			t.Fatalf("%d: sau phục hồi acked %d alarms %v", code, s.Acked(), snd.m.Alarms())
		}
		_ = s.Close()
	}
}

// Không có chứng thư client → ingest (mTLS bắt buộc) từ chối bắt tay.
func TestMTLSRequiresClientCert(t *testing.T) {
	cfg, _ := enrolled(t, 1<<20)
	cfg.Ingest.URL = "https://127.0.0.1:1/v1/edge/batches"
	if _, err := NewMTLSClient(cfg.Ingest); err == nil {
		t.Fatal("mong lỗi thiếu chứng thư client")
	}
}
