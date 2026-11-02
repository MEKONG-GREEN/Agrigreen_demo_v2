package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKNLegacyWriteRouteMap(t *testing.T) {
	e := gin.New()
	e.HandleMethodNotAllowed = true
	Handler{}.RegisterApp(e.Group("/v1"))
	routes := map[string]bool{}
	for _, r := range e.Routes() {
		routes[r.Method+" "+r.Path] = true
	}
	for _, r := range []string{"GET /v1/kn/cooperatives/:ref/households", "GET /v1/kn/households/:ref/log", "GET /v1/cooperatives/:ref/households", "POST /v1/households", "POST /v1/households/copy", "POST /v1/households/:ref/membership"} {
		if !routes[r] {
			t.Errorf("read/native route removed: %s", r)
		}
	}
	for _, r := range []struct {
		method, path, pattern string
		status                int
	}{
		{"POST", "/v1/kn/cooperatives/00000000-0000-7000-8000-000000000001/households", "/v1/kn/cooperatives/:ref/households", http.StatusMethodNotAllowed},
		{"PUT", "/v1/kn/households/00000000-0000-7000-8000-000000000001", "/v1/kn/households/:ref", http.StatusNotFound},
	} {
		t.Run(r.method+r.pattern, func(t *testing.T) {
			if routes[r.method+" "+r.pattern] {
				t.Fatalf("legacy KN write still registered: %s %s", r.method, r.pattern)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(r.method, r.path, nil))
			if w.Code != r.status {
				t.Fatalf("retired route=%d,want%d", w.Code, r.status)
			}
		})
	}
}
