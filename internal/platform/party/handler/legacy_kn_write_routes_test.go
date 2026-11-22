package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// KN-STANDALONE-5-TT-DATA (#271): KN không còn đọc HTX nền tảng qua /v1/kn/cooperatives* — bộ chọn HTX của KN đọc hồ sơ
// KN riêng (/v1/kn/producer-records). Bản đồ route của module là hợp đồng: mọi phương thức trên họ đường này trả 404.
// Route của App HTX (/v1/cooperatives…) do module khác giữ, không thuộc kiểm này.
func TestKNCooperativeRoutesRemoved(t *testing.T) {
	e := gin.New()
	e.HandleMethodNotAllowed = true
	party.New(nil, clock.System{}).RegisterApp(e.Group("/v1"))
	for _, r := range e.Routes() {
		if strings.HasPrefix(r.Path, "/v1/kn/cooperatives") {
			t.Errorf("route KN đọc HTX nền tảng còn đăng ký: %s %s", r.Method, r.Path)
		}
	}
	ref := "00000000-0000-7000-8000-000000000001"
	for _, r := range []struct{ method, path string }{
		{"GET", "/v1/kn/cooperatives"}, {"GET", "/v1/kn/cooperatives/" + ref}, {"GET", "/v1/kn/cooperatives/" + ref + "/log"},
		{"POST", "/v1/kn/cooperatives"}, {"PUT", "/v1/kn/cooperatives/" + ref},
		{"POST", "/v1/kn/cooperatives/" + ref + "/deactivate"}, {"POST", "/v1/kn/cooperatives/" + ref + "/reactivate"},
	} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(r.method, r.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, muốn 404", r.method, r.path, w.Code)
		}
	}
}
