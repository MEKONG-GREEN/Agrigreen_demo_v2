package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	partyhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/handler"
	"github.com/gin-gonic/gin"
)

// The registration map is the contract: an unauthorized status or validation error
// would still leave a KN write capability installed. Retired methods are404, or405 when a retained GET owns that path.
func TestKNLegacyWriteRouteMap(t *testing.T) {
	e := gin.New()
	e.HandleMethodNotAllowed = true
	partyhandler.KN{}.RegisterKNCollect(e.Group("/v1"))
	routes := map[string]bool{}
	for _, r := range e.Routes() {
		routes[r.Method+" "+r.Path] = true
	}
	for _, r := range []string{
		"GET /v1/kn/cooperatives", "GET /v1/kn/cooperatives/:ref", "GET /v1/kn/cooperatives/:ref/log",
	} {
		if !routes[r] {
			t.Errorf("read/native route removed: %s", r)
		}
	}
	for _, r := range []struct {
		method, path, pattern string
		status                int
	}{
		{"POST", "/v1/kn/cooperatives", "/v1/kn/cooperatives", http.StatusMethodNotAllowed},
		{"PUT", "/v1/kn/cooperatives/00000000-0000-7000-8000-000000000001", "/v1/kn/cooperatives/:ref", http.StatusMethodNotAllowed},
		{"POST", "/v1/kn/cooperatives/00000000-0000-7000-8000-000000000001/deactivate", "/v1/kn/cooperatives/:ref/deactivate", http.StatusNotFound},
		{"POST", "/v1/kn/cooperatives/00000000-0000-7000-8000-000000000001/reactivate", "/v1/kn/cooperatives/:ref/reactivate", http.StatusNotFound},
	} {
		t.Run(r.method+r.pattern, func(t *testing.T) {
			if routes[r.method+" "+r.pattern] {
				t.Fatalf("legacy KN write still registered: %s %s", r.method, r.pattern)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(r.method, r.path, nil))
			if w.Code != r.status {
				t.Fatalf("retired %s %s = %d, want%d", r.method, r.path, w.Code, r.status)
			}
		})
	}
}
