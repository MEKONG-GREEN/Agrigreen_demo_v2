package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Đầu-cuối: bộ mô phỏng → WAL → lô ký → mTLS → ingest giả xác minh chữ ký; bộ đếm
// liên tiếp từ 1, không trùng, không hở (mục 14; ADR-0019).
func TestAgentEndToEndSimulatorToIngest(t *testing.T) {
	cfg, k := enrolled(t, 1<<22)
	f := startIngest(t, cfg, k, http.StatusServiceUnavailable)
	cfg.Adapter.Simulator = &SimConfig{Seed: 5, RateHz: 0}
	if err := cfg.Adapter.normalize(); err != nil {
		t.Fatal(err)
	}
	cfg.Batch.MaxReadings = 100
	cfg.Batch.MaxWait = Duration(10 * time.Millisecond)
	a, err := NewAgent(cfg, clock.System{}, nil, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	deadline := time.Now().Add(30 * time.Second)
	for a.store.Acked() < 150 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.store.Acked() < 150 {
		t.Fatalf("chỉ xác nhận tới %d", a.store.Acked())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	next := int64(1)
	var prev []byte
	for _, body := range f.bodies {
		b, err := edgewire.Decode(body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(body, prev) { // lần gửi lại sau 503
			continue
		}
		prev = body
		if b.CounterFrom != next {
			t.Fatalf("lô bắt đầu ở %d, cần %d", b.CounterFrom, next)
		}
		next = b.CounterTo + 1
	}
	st, _ := ReadState(cfg.DataDir)
	if st.LastCounter < st.AckedCounter || st.AckedCounter != a.store.Acked() {
		t.Fatalf("state sau dừng: %+v", st)
	}
}

func TestHealthzAndMetrics(t *testing.T) {
	cfg, k := enrolled(t, 8192)
	s := openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	clk := clock.NewFixed(testT0)
	m := NewMetrics(clk)
	h := &Health{M: m, Store: s, NTP: NewNTPTracker(cfg.NTP, clk, quietLog()), Cfg: cfg, Version: "1.0.0", Start: clk.Now()}
	a := &Agent{cfg: cfg, clk: clk, log: quietLog(), store: s, m: m, sender: &Sender{wake: make(chan struct{}, 1)}, health: h}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()
	get := func(p string) (int, string) {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if err := a.ingestOne(Sample{ValueKg: "1.000", Unit: "kg"}); err != nil {
		t.Fatal(err)
	}
	if code, body := get("/healthz"); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("healthz %d %s", code, body)
	}
	// Làm đầy đệm → từ chối số đo, báo động, /healthz 503.
	for range 200 {
		if err := a.ingestOne(Sample{ValueKg: "1.000", Unit: "kg"}); err != nil {
			t.Fatal(err)
		}
	}
	if m.ReadingsRefused.Load() == 0 {
		t.Fatal("đệm chưa đầy")
	}
	code, body := get("/healthz")
	var hz map[string]any
	_ = json.Unmarshal([]byte(body), &hz)
	if code != 503 || hz["alarms"].(map[string]any)[AlarmBufferFull] == nil {
		t.Fatalf("healthz khi đầy %d %s", code, body)
	}
	_, mt := get("/metrics")
	for _, want := range []string{"edge_buffer_fill_ratio", "edge_readings_refused_total", `edge_alarm{reason="buffer_full"} 1`, `edge_alarm{reason="buffer_high"} 1`, "edge_last_push_success_timestamp_seconds", "edge_adapter_errors_total", "edge_ntp_synced 0"} {
		if !strings.Contains(mt, want) {
			t.Fatalf("/metrics thiếu %q:\n%s", want, mt)
		}
	}
	// Không có số đo quá stale_reading → adapter_stale.
	clk.Advance(2 * time.Minute)
	if ok, al := h.Evaluate(); ok || al[AlarmAdapterStale] == "" {
		t.Fatalf("mong adapter_stale: %v", al)
	}
	// Số đo sai hợp đồng bị đếm là lỗi bộ điều hợp, không ghi đệm.
	before := m.AdapterErrors.Load()
	_ = a.ingestOne(Sample{ValueKg: "1.5", Unit: "kg"})
	if m.AdapterErrors.Load() != before+1 {
		t.Fatal("số đo sai hợp đồng phải bị đếm lỗi")
	}
}

func TestCLIEnrollInfoVersion(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	b, _ := json.Marshal(map[string]any{"device_id": "scale-hub01-out", "data_dir": dir + "/data", "adapter": map[string]any{"type": "simulator"}})
	if err := writeNewFile(cfgPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"enroll", "-config", cfgPath, "-epoch", "1"}, &out, &errb); code != 0 {
		t.Fatalf("enroll %d: %s", code, errb.String())
	}
	for _, want := range []string{`"key_id"`, `"public_key_hex"`, "CERTIFICATE REQUEST", "CN=scale-hub01-out"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("enroll thiếu %s: %s", want, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"info", "-config", cfgPath}, &out, &errb); code != 0 || !strings.Contains(out.String(), `"epoch": 1`) {
		t.Fatalf("info: %s %s", out.String(), errb.String())
	}
	if code := run([]string{"enroll", "-config", cfgPath, "-epoch", "1"}, &out, &errb); code == 0 {
		t.Fatal("đăng ký lại phải lỗi")
	}
	out.Reset()
	if code := run([]string{"version"}, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "edge-agent ") {
		t.Fatal("version")
	}
}
