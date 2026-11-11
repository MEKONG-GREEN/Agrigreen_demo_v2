package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// KN-STANDALONE-5-TT-DATA-MAY (#306): máy móc HTX của KN (FN-XA-03.04) do chủ KN phục vụ từ kho KN — module CGH không còn
// đăng ký /v1/kn/machine-counts* (mọi phương thức trả 404 ở module này). Route của App CGH (/v1/mechanization/…) giữ nguyên.
func TestKNMachineCountRoutesRemoved(t *testing.T) {
	e := gin.New()
	e.HandleMethodNotAllowed = true
	mechanization.New(nil, clock.System{}, mechanization.Deps{}).RegisterApp(e.Group("/v1"))
	routes := map[string]bool{}
	for _, r := range e.Routes() {
		routes[r.Method+" "+r.Path] = true
		if strings.HasPrefix(r.Path, "/v1/kn/") {
			t.Errorf("route KN còn đăng ký ở module CGH: %s %s", r.Method, r.Path)
		}
	}
	ref := "00000000-0000-7000-8000-000000000001"
	for _, r := range []struct{ method, path string }{
		{"GET", "/v1/kn/machine-counts"}, {"GET", "/v1/kn/machine-counts/" + ref}, {"PUT", "/v1/kn/machine-counts/" + ref},
		{"GET", "/v1/kn/machine-counts/" + ref + "/log"},
	} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(r.method, r.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, muốn 404", r.method, r.path, w.Code)
		}
	}
	for _, r := range []string{"GET /v1/mechanization/machines", "GET /v1/mechanization/machines/:ref",
		"GET /v1/mechanization/cooperatives/:ref/machine-count", "GET /v1/mechanization/cooperatives/:ref/panel"} {
		if !routes[r] {
			t.Errorf("route App CGH bị mất: %s", r)
		}
	}
}
