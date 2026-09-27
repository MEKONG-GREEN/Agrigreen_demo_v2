package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func corsEngine(t *testing.T, origins string) *gin.Engine {
	t.Helper()
	list, err := httpx.ParseCORSOrigins(origins)
	if err != nil {
		t.Fatal(err)
	}
	r := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	r.Use(httpx.CORS(list))
	r.GET("/v1/x", func(c *gin.Context) { c.Header("Location", "/v1/x/1"); c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.POST("/v1/x", func(c *gin.Context) { c.JSON(http.StatusCreated, gin.H{"ok": true}) })
	return r
}

func serve(r http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestParseCORSOrigins(t *testing.T) {
	got, err := httpx.ParseCORSOrigins(" http://localhost:3000, https://Ops.AgriGreen.vn ,,http://localhost:3000")
	if err != nil || !slices.Equal(got, []string{"http://localhost:3000", "https://ops.agrigreen.vn"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := httpx.ParseCORSOrigins(""); err != nil || len(got) != 0 {
		t.Fatalf("rỗng: %v %v", got, err)
	}
	for _, bad := range []string{"*", "https://*.agrigreen.vn", "localhost:3000", "http://localhost:3000/", "http://a.vn/path", "ftp://a.vn", "http://u:p@a.vn", "http://a.vn?x=1"} {
		if _, err := httpx.ParseCORSOrigins(bad); err == nil {
			t.Fatalf("phải từ chối %q", bad)
		}
	}
}

func TestCORSPreflightAllowed(t *testing.T) {
	r := corsEngine(t, "http://localhost:3000")
	// POST route chỉ có POST/GET: preflight OPTIONS phải được CORS trả 204 trước nhánh 405.
	w := serve(r, http.MethodOptions, "/v1/x", map[string]string{
		"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST",
		"Access-Control-Request-Headers": "authorization, content-type, idempotency-key",
	})
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: %d %s", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "http://localhost:3000" || h.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("Allow-Origin phải đúng origin, không credentials: %v", h)
	}
	for _, want := range []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Device-Id", "X-Purpose", "traceparent"} {
		if !strings.Contains(h.Get("Access-Control-Allow-Headers"), want) {
			t.Fatalf("Allow-Headers thiếu %s: %q", want, h.Get("Access-Control-Allow-Headers"))
		}
	}
	if !strings.Contains(h.Get("Access-Control-Allow-Methods"), "POST") || h.Get("Access-Control-Max-Age") == "" || !slices.Contains(h.Values("Vary"), "Origin") {
		t.Fatalf("header preflight: %v", h)
	}
	// Preflight tới đường không tồn tại vẫn được trả lời (nhánh 404 của Gin cũng qua middleware).
	if w := serve(r, http.MethodOptions, "/v1/khong-co", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "GET"}); w.Code != http.StatusNoContent {
		t.Fatalf("preflight đường lạ: %d", w.Code)
	}
}

func TestCORSPreflightRejected(t *testing.T) {
	r := corsEngine(t, "http://localhost:3000")
	for _, origin := range []string{"http://evil.example", "http://localhost:3001", "https://localhost:3000", "null"} {
		w := serve(r, http.MethodOptions, "/v1/x", map[string]string{"Origin": origin, "Access-Control-Request-Method": "POST"})
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %s phải bị chặn: %d %v", origin, w.Code, w.Header())
		}
	}
	// Không cấu hình origin nào → mọi preflight bị chặn.
	if w := serve(corsEngine(t, ""), http.MethodOptions, "/v1/x", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "GET"}); w.Code != http.StatusForbidden {
		t.Fatalf("danh sách rỗng: %d", w.Code)
	}
}

func TestCORSActualRequest(t *testing.T) {
	r := corsEngine(t, "http://localhost:3000,https://ops.agrigreen.vn")
	w := serve(r, http.MethodGet, "/v1/x", map[string]string{"Origin": "https://ops.agrigreen.vn"})
	h := w.Header()
	if w.Code != http.StatusOK || h.Get("Access-Control-Allow-Origin") != "https://ops.agrigreen.vn" || !slices.Contains(h.Values("Vary"), "Origin") {
		t.Fatalf("request thường: %d %v", w.Code, h)
	}
	for _, want := range []string{"X-Trace-Id", "Idempotent-Replayed", "Location"} {
		if !strings.Contains(h.Get("Access-Control-Expose-Headers"), want) {
			t.Fatalf("Expose-Headers thiếu %s: %q", want, h.Get("Access-Control-Expose-Headers"))
		}
	}
	// Origin lạ: request vẫn chạy (không phải cơ chế phân quyền) nhưng không có header CORS.
	w = serve(r, http.MethodGet, "/v1/x", map[string]string{"Origin": "http://evil.example"})
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Expose-Headers") != "" {
		t.Fatalf("origin lạ không được có header CORS: %v", w.Header())
	}
	// Không có Origin (app di động, server-to-server): không đụng tới.
	w = serve(r, http.MethodGet, "/v1/x", nil)
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" || slices.Contains(w.Header().Values("Vary"), "Origin") {
		t.Fatalf("không Origin: %v", w.Header())
	}
	// OPTIONS không phải preflight (thiếu Access-Control-Request-Method) → như request thường (405).
	if w := serve(r, http.MethodOptions, "/v1/x", map[string]string{"Origin": "http://localhost:3000"}); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("OPTIONS thường: %d", w.Code)
	}
}
