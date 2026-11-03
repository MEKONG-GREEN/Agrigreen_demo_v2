package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Invalid numeric selectors must fail before SQL or schema loading; scientific
// exponents and very long input cannot expand into unbounded numeric work.
func TestTemplateCatalogRejectsNumericExtremes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := Handler{}
	r.GET("/versions", h.templateVersions)
	r.GET("/versions/:version", h.templateVersion)
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 20000), "-1", "0.1"} {
		for _, path := range []string{"/versions/" + url.PathEscape(value), "/versions?before_version=" + url.QueryEscape(value), "/versions?limit=" + url.QueryEscape(value)} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"validation_failed"`) {
				t.Fatalf("numeric selector accepted: len=%d status=%d body=%s", len(value), w.Code, w.Body.String())
			}
		}
	}
}
