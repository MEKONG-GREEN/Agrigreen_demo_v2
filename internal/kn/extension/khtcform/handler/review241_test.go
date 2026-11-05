package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/app"
	"github.com/gin-gonic/gin"
)

func TestReview241JSONBoundaryBeforeService(t *testing.T) {
	const valid = `{"decision":"accepted","expected_submission_id":"11111111-1111-4111-8111-111111111111","base_attempt":1}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"valid-reaches-service", valid, 503},
		{"old-client-missing-target", `{"decision":"accepted"}`, 422},
		{"second-json-object", valid + ` {}`, 400},
		{"trailing-garbage", valid + ` invalid`, 400},
		{"oversized-trailing-whitespace", valid + strings.Repeat(" ", 17000), 400},
		{"oversized-note", strings.Replace(valid, `"decision":"accepted"`, `"decision":"accepted","note":"`+strings.Repeat("x", 17000)+`"`, 1), 400},
		{"large-exponent", strings.Replace(valid, `"base_attempt":1`, `"base_attempt":1e100000`, 1), 400},
		{"tiny-exponent", strings.Replace(valid, `"base_attempt":1`, `"base_attempt":1e-100000`, 1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			(Handler{Svc: &app.Service{}}).RegisterApp(r.Group("/v1"))
			req := httptest.NewRequest(http.MethodPost, "/v1/extension/khtc/periods/11111111-1111-4111-8111-111111111111/forms/trong_trot/review", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatalf("got %d want %d: %s", out.Code, tc.want, out.Body.String())
			}
		})
	}
}
