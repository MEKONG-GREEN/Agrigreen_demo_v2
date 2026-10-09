package assembly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

type legacyCall struct{ route, client string }

// legacyEngine dựng engine có middleware ngừng dùng và vài route mẫu; ?tenant=<uuid> giả lập xác thực.
func legacyEngine(cfg PartnerLegacy, calls *[]legacyCall) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(partnerLegacyMiddleware(cfg, func(_ context.Context, route, client string) {
		*calls = append(*calls, legacyCall{route, client})
	}))
	auth := func(c *gin.Context) {
		if t := c.Query("tenant"); t != "" {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{TenantID: uuid.MustParse(t)}))
			return
		}
		c.AbortWithStatus(http.StatusUnauthorized)
	}
	v1 := r.Group("/v1", auth)
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	v1.GET("/partner/plots/:ref", ok)
	v1.GET("/partner/grants", ok)
	v1.GET("/sync/plots", ok)
	return r
}

func get(r http.Handler, path string) *http.Response {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Result()
}

func TestPartnerLegacyHeadersAndMetric(t *testing.T) {
	sunset := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	var calls []legacyCall
	r := legacyEngine(PartnerLegacy{Sunset: sunset, SuccessorBase: "https://partner.example.vn"}, &calls)
	tid := uuid.MustParse("01920000-0000-7000-8000-000000000001")

	resp := get(r, "/v1/partner/plots/P1?tenant="+tid.String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Deprecation"), "@1791504000"; got != want { // 2026-10-09T00:00:00Z (RFC 9745)
		t.Fatalf("Deprecation = %q, cần %q", got, want)
	}
	if got, want := resp.Header.Get("Sunset"), "Wed, 31 Mar 2027 00:00:00 GMT"; got != want {
		t.Fatalf("Sunset = %q, cần %q", got, want)
	}
	if got, want := resp.Header.Get("Link"), `<https://partner.example.vn/v1/feeds/plot_directory>; rel="successor-version"`; got != want {
		t.Fatalf("Link = %q, cần %q", got, want)
	}

	// Chưa xác thực: vẫn có header (bên gọi biết lý do) và vẫn đếm (mọi bên còn gọi chặn gỡ W5.10).
	resp = get(r, "/v1/partner/grants")
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Deprecation") == "" {
		t.Fatalf("401 phải có Deprecation: %d %v", resp.StatusCode, resp.Header)
	}
	if resp.Header.Get("Link") != "" {
		t.Fatalf("grants không có kế nhiệm, không được có Link: %q", resp.Header.Get("Link"))
	}

	// Route khác và route /v1/partner lạ (404) không bị gắn header, không đếm.
	for _, p := range []string{"/v1/sync/plots?tenant=" + tid.String(), "/v1/partner/unknown", "/healthz"} {
		if resp := get(r, p); resp.Header.Get("Deprecation") != "" {
			t.Fatalf("%s không phải route đối tác cũ nhưng có Deprecation", p)
		}
	}
	want := []legacyCall{{"/v1/partner/plots/:ref", tid.String()}, {"/v1/partner/grants", "anonymous"}}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("metric = %+v, cần %+v", calls, want)
	}
}

func TestPartnerLegacyNoSunsetRelativeLink(t *testing.T) {
	var calls []legacyCall
	resp := get(legacyEngine(PartnerLegacy{}, &calls), "/v1/partner/plots/P1")
	if resp.Header.Get("Sunset") != "" {
		t.Fatalf("không cấu hình Sunset thì không phát: %q", resp.Header.Get("Sunset"))
	}
	if got := resp.Header.Get("Link"); got != `</v1/feeds/plot_directory>; rel="successor-version"` {
		t.Fatalf("Link tương đối = %q", got)
	}
}

func TestPartnerLegacyFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	p, err := PartnerLegacyFromEnv(env(nil))
	if err != nil || !p.Sunset.IsZero() || p.SuccessorBase != "" {
		t.Fatalf("mặc định: %+v %v", p, err)
	}
	p, err = PartnerLegacyFromEnv(env(map[string]string{"AG_ENV": "production",
		EnvPartnerSunset: "2027-03-31", EnvPartnerSuccessorBase: "https://partner.example.vn/"}))
	if err != nil || !p.Sunset.Equal(time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)) || p.SuccessorBase != "https://partner.example.vn" {
		t.Fatalf("hợp lệ: %+v %v", p, err)
	}
	if _, err := PartnerLegacyFromEnv(env(map[string]string{EnvPartnerSunset: "2027-03-31T07:00:00+07:00"})); err != nil {
		t.Fatalf("RFC 3339: %v", err)
	}
	bad := map[string]map[string]string{
		"ngày sai":              {EnvPartnerSunset: "31/03/2027"},
		"trước mốc ngừng dùng":  {EnvPartnerSunset: "2026-10-01"},
		"gốc không tuyệt đối":   {EnvPartnerSuccessorBase: "partner.example.vn"},
		"gốc có query":          {EnvPartnerSuccessorBase: "https://partner.example.vn?x=1"},
		"http ở production":     {"AG_ENV": "production", EnvPartnerSuccessorBase: "http://partner.example.vn"},
		"lược đồ không http(s)": {EnvPartnerSuccessorBase: "ftp://partner.example.vn"},
	}
	for name, m := range bad {
		if _, err := PartnerLegacyFromEnv(env(m)); err == nil || !strings.Contains(err.Error(), "AG_PARTNER_") {
			t.Errorf("%s: cần lỗi, có %v", name, err)
		}
	}
}
