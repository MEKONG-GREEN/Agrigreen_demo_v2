package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

func TestPercentileNearestRank(t *testing.T) {
	var xs []time.Duration
	for i := 1; i <= 100; i++ {
		xs = append(xs, time.Duration(i)*time.Millisecond)
	}
	for _, c := range []struct {
		p    float64
		want time.Duration
	}{{50, 50 * time.Millisecond}, {95, 95 * time.Millisecond}, {99, 99 * time.Millisecond}, {100, 100 * time.Millisecond}, {0.1, time.Millisecond}} {
		if got := Percentile(xs, c.p); got != c.want {
			t.Errorf("p%v = %s, muốn %s", c.p, got, c.want)
		}
	}
	if Percentile(nil, 95) != 0 {
		t.Error("rỗng phải 0")
	}
}

func TestSummaryErrorRateCountsDrops(t *testing.T) {
	r := NewRecorder()
	for i := 0; i < 97; i++ {
		r.Observe("read", time.Millisecond, "200", true)
	}
	r.Observe("read", time.Second, "503", false)
	r.Drop("read")
	r.Drop("read")
	s := r.Summaries(time.Second)[0]
	if s.Count != 98 || s.Errors != 1 || s.Dropped != 2 {
		t.Fatalf("%+v", s)
	}
	if s.ErrorRate < 0.0299 || s.ErrorRate > 0.0301 {
		t.Fatalf("tỷ lệ lỗi %v, muốn 3/100", s.ErrorRate)
	}
	if v := Check([]Summary{s}, map[string]time.Duration{"read": 2 * time.Second}, 0.01); len(v) != 1 {
		t.Fatalf("phải vượt ngưỡng lỗi: %v", v)
	}
}

func TestParseFlagsGuardsProduction(t *testing.T) {
	base := []string{"-api", "https://api.agrigreen.vn", "-accounts", "a.tsv", "-rate", "read=1"}
	if _, err := parseFlags(base, io.Discard); err == nil || !strings.Contains(err.Error(), "R-95") {
		t.Fatalf("URL không phải staging phải bị chặn: %v", err)
	}
	c, err := parseFlags([]string{"-api", "https://api.staging.agrigreen.vn", "-accounts", "a.tsv", "-rate", "read=5,sync_pull=2", "-max-p95", "read=500ms"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.rates["read"] != 5 || c.maxP95["read"] != 500*time.Millisecond || c.counterBase == 0 {
		t.Fatalf("%+v", c)
	}
	if _, err := parseFlags([]string{"-rate", "bogus=1"}, io.Discard); err == nil {
		t.Fatal("kịch bản lạ phải lỗi")
	}
}

// Kịch bản Application API trên máy chủ giả: header bắt buộc, idem_key đúng dạng sync-contract,
// con trỏ pull nối tiếp, tốc độ gần đúng.
func TestAPIScenariosAgainstFakeServer(t *testing.T) {
	idemRe := regexp.MustCompile(`^dev:([0-9a-fA-F-]{36}):([1-9][0-9]{0,18})$`)
	var pulls, pushes, reads atomic.Int64
	var mu sync.Mutex
	seenIdem := map[string]bool{}
	pulledBy := map[string]bool{}
	var failures []string
	fail := func(f string) { mu.Lock(); failures = append(failures, f); mu.Unlock() }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" || r.Header.Get("X-Device-Id") == "" {
			fail("thiếu Authorization/X-Device-Id")
		}
		switch {
		case r.URL.Path == "/v1/sync/pull":
			pulls.Add(1)
			mu.Lock()
			dev := r.Header.Get("X-Device-Id")
			if pulledBy[dev] && r.URL.Query().Get("cursor") != "c1" {
				failures = append(failures, "pull sau của cùng thiết bị không gửi cursor")
			}
			pulledBy[dev] = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"changes": []any{map[string]any{"entity": "plot"}}, "next_cursor": "c1", "has_more": false})
		case r.URL.Path == "/v1/sync/push" && r.Method == http.MethodPost:
			pushes.Add(1)
			if r.Header.Get("Idempotency-Key") == "" {
				fail("push thiếu Idempotency-Key")
			}
			var body struct {
				DeviceID string `json:"device_id"`
				Records  []struct {
					ID, IdemKey, Entity, Op string
				} `json:"records"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(bytes.ReplaceAll(b, []byte(`"idem_key"`), []byte(`"IdemKey"`)), &body)
			res := []map[string]string{}
			for _, rc := range body.Records {
				m := idemRe.FindStringSubmatch(rc.IdemKey)
				if m == nil || m[1] != body.DeviceID {
					fail("idem_key sai dạng: " + rc.IdemKey)
				}
				mu.Lock()
				if seenIdem[rc.IdemKey] {
					failures = append(failures, "idem_key lặp: "+rc.IdemKey)
				}
				seenIdem[rc.IdemKey] = true
				mu.Unlock()
				res = append(res, map[string]string{"id": rc.ID, "status": "accepted"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": res})
		default:
			reads.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	accFile := filepath.Join(dir, "a.tsv")
	if err := os.WriteFile(accFile, []byte("# thiết bị\ttoken\n0192a000-0000-7000-8000-000000000001\ttok-1\n0192a000-0000-7000-8000-000000000002\ttok-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	accs, err := LoadAccounts(accFile)
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewAPI(srv.URL, accs, "", 16, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	api.PushRecords = 3
	rec := NewRecorder()
	el := Run(context.Background(), []Scenario{
		{Name: "sync_pull", Rate: 40, Do: api.SyncPull},
		{Name: "sync_push", Rate: 20, Do: api.SyncPush},
		{Name: "read", Rate: 20, Do: api.Read([]string{"/v1/geo/standard-map?zoom=13"})},
	}, 700*time.Millisecond, 8, rec)
	for _, f := range failures {
		t.Error(f)
	}
	sums := rec.Summaries(el)
	if len(sums) != 3 {
		t.Fatalf("%+v", sums)
	}
	for _, s := range sums {
		if s.Count == 0 || s.Errors != 0 {
			t.Errorf("%s: %+v", s.Op, s)
		}
	}
	if pulls.Load() < 10 || pushes.Load() < 5 || reads.Load() < 5 {
		t.Errorf("tốc độ quá thấp: pull=%d push=%d read=%d", pulls.Load(), pushes.Load(), reads.Load())
	}
	if rec.Counters()["sync_push.record.accepted"] != 3*pushes.Load() {
		t.Errorf("đếm bản ghi accepted: %v", rec.Counters())
	}
}

// Lô ingest: chữ ký Ed25519 hợp lệ trên dạng chuẩn (edgewire.Verify), bộ đếm liên tiếp không chồng,
// mTLS bằng chứng thư client của thiết bị, mỗi thiết bị một lô đang bay.
func TestIngestBatchesSignedAndSequential(t *testing.T) {
	dir := t.TempDir()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-device-ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)
	devKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	devTpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "scale-hub01-a"}, DNSNames: []string{"scale-hub01-a"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	devDER, _ := x509.CreateCertificate(rand.Reader, devTpl, caCert, &devKey.PublicKey, caKey)
	write := func(name, typ string, der []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	devKeyDER, _ := x509.MarshalPKCS8PrivateKey(devKey)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	srcDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	spec := []DeviceSpec{{DeviceID: "scale-hub01-a", Epoch: 1, SourceKey: write("source.key", "PRIVATE KEY", srcDER),
		Cert: write("device.crt", "CERTIFICATE", devDER), Key: write("device.key", "PRIVATE KEY", devKeyDER)}}
	specFile := filepath.Join(dir, "devices.json")
	b, _ := json.Marshal(spec)
	if err := os.WriteFile(specFile, b, 0o600); err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	var mu sync.Mutex
	var last int64
	var inflight, maxInflight atomic.Int64
	var failures []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			m := maxInflight.Load()
			if n <= m || maxInflight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		body, _ := io.ReadAll(r.Body)
		bt, err := edgewire.Decode(body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err != nil:
			failures = append(failures, "decode: "+err.Error())
		case edgewire.Verify(pub, bt) != nil:
			failures = append(failures, "chữ ký sai")
		case r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != bt.DeviceID:
			failures = append(failures, "mTLS không khớp device_id")
		case last != 0 && bt.CounterFrom != last+1:
			failures = append(failures, "bộ đếm không liên tiếp")
		}
		if err == nil {
			last = bt.CounterTo
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	caFile := filepath.Join(dir, "server-ca.crt")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := NewIngest(srv.URL+"/v1/ingest/batches", specFile, caFile, "example.com", 50, 1_000, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecorder()
	el := Run(context.Background(), []Scenario{{Name: "ingest_batch", Rate: 50, Do: in.Batch}}, 500*time.Millisecond, 8, rec)
	for _, f := range failures {
		t.Error(f)
	}
	s := rec.Summaries(el)[0]
	if s.OK < 5 || s.Errors != 0 {
		t.Fatalf("%+v", s)
	}
	if maxInflight.Load() != 1 {
		t.Errorf("một thiết bị chỉ một lô đang bay, thấy %d", maxInflight.Load())
	}
	if rec.Counters()["ingest.readings"] != 50*s.OK {
		t.Errorf("số đo: %v", rec.Counters())
	}
	if last <= 1_000 {
		t.Errorf("bộ đếm phải bắt đầu sau counter-base: %d", last)
	}
}
