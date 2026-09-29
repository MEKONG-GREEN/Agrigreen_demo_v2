package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Healthcheck của compose (ảnh distroless): 200 → 0, còn lại → 1.
func TestHealthcheckExitCode(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	if got := healthcheck(srv.URL + "/readyz"); got != 0 {
		t.Fatalf("200 → mong 0, nhận %d", got)
	}
	status = http.StatusServiceUnavailable
	if got := healthcheck(srv.URL + "/readyz"); got != 1 {
		t.Fatalf("503 → mong 1, nhận %d", got)
	}
	if got := healthcheck("http://127.0.0.1:1/readyz"); got != 1 {
		t.Fatalf("không kết nối được → mong 1, nhận %d", got)
	}
}
