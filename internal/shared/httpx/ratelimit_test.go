package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func ipEngine(clk clock.Clock, perMinute, burst int) *gin.Engine {
	r := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	r.GET("/pub", httpx.RateLimitByIP(clk, perMinute, burst), func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

func from(r http.Handler, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/pub", nil)
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimitByIP(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC))
	r := ipEngine(clk, 60, 2) // 1 token/giây, dồn 2
	for i := range 2 {
		if w := from(r, "10.0.0.1:1000", nil); w.Code != http.StatusOK {
			t.Fatalf("yêu cầu %d trong burst: %d", i, w.Code)
		}
	}
	w := from(r, "10.0.0.1:1000", nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("vượt burst → 429 + Retry-After 1: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := from(r, "10.0.0.2:1000", nil); w.Code != http.StatusOK {
		t.Fatalf("IP khác có bucket riêng: %d", w.Code)
	}
	clk.Advance(time.Second)
	if w := from(r, "10.0.0.1:1000", nil); w.Code != http.StatusOK {
		t.Fatalf("sau 1 giây nạp lại 1 token: %d", w.Code)
	}
}

// Không khai báo proxy tin cậy → X-Forwarded-For bị bỏ qua: không giả IP để né giới hạn
// hay ghi IP giả vào sổ truy cập.
func TestClientIPIgnoresForwardedWithoutTrustedProxies(t *testing.T) {
	t.Setenv(httpx.TrustedProxiesEnv, "")
	r := ipEngine(clock.System{}, 60, 1)
	w := from(r, "203.0.113.7:4000", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if w.Code != http.StatusOK || w.Body.String() != "203.0.113.7" {
		t.Fatalf("ClientIP phải là địa chỉ kết nối: %d %q", w.Code, w.Body.String())
	}
	if w := from(r, "203.0.113.7:4000", map[string]string{"X-Forwarded-For": "5.6.7.8"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("đổi X-Forwarded-For không né được giới hạn: %d", w.Code)
	}
}

func TestClientIPFromTrustedProxy(t *testing.T) {
	t.Setenv(httpx.TrustedProxiesEnv, "10.42.0.0/16")
	r := ipEngine(clock.System{}, 60, 5)
	if w := from(r, "10.42.1.9:4000", map[string]string{"X-Forwarded-For": "198.51.100.20"}); w.Body.String() != "198.51.100.20" {
		t.Fatalf("qua proxy tin cậy → IP người gọi: %q", w.Body.String())
	}
	if w := from(r, "203.0.113.7:4000", map[string]string{"X-Forwarded-For": "198.51.100.20"}); w.Body.String() != "203.0.113.7" {
		t.Fatalf("nguồn ngoài dải tin cậy → bỏ X-Forwarded-For: %q", w.Body.String())
	}
}
