package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Healthcheck của compose (ảnh distroless): 200 → 0, còn lại → 1.
func TestHealthcheckExitCode(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	if got := healthcheck(srv.URL + "/readyz"); got != 0 {
		t.Fatalf("200 → 0, nhận %d", got)
	}
	status = http.StatusServiceUnavailable
	if got := healthcheck(srv.URL + "/readyz"); got != 1 {
		t.Fatalf("503 → 1, nhận %d", got)
	}
}

// /readyz khi chưa cấu hình cụm ts (chế độ G0) → 503, /healthz → 200.
func TestOpsRouterNotConfigured(t *testing.T) {
	h := opsRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 503} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d, mong %d", path, w.Code, want)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	c, err := configFromEnv()
	if err != nil || c.ratePerMin != 120 || c.burst != 60 || c.skewMs != 2000 || c.interval != 15*time.Second || c.stuckAfter != 30*time.Minute {
		t.Fatalf("mặc định: %+v %v", c, err)
	}
	t.Setenv("AG_INGEST_RATE_PER_MIN", "-1")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("số âm phải lỗi")
	}
	t.Setenv("AG_INGEST_RATE_PER_MIN", "10")
	t.Setenv("AG_INGEST_INTEGRATE_INTERVAL", "0s")
	if _, err := configFromEnv(); err == nil {
		t.Fatal("khoảng 0 phải lỗi")
	}
}
