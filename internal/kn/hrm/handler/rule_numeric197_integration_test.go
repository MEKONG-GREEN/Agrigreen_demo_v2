package handler

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The wire contract accepts ordinary JSON strings and numbers, but must reject
// hostile numeric encodings before decimal allocation and without appending a rule.
func TestHRMRule197PercentWireBounds(t *testing.T) {
	f := newRecFixture(t, true)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	// recFixture's basic router intentionally omits idempotency middleware. Use
	// the real middleware and a distinct operation key for each boundary write.
	router := gin.New()
	sequence := 0
	router.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
		if c.Request.Method == http.MethodPost {
			sequence++
			c.Request.Header.Set(idempotency.Header, "rule-197-"+strconv.Itoa(sequence))
		}
	}, idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api")))
	Handler{Svc: f.svc}.RegisterApp(router.Group("/v1"))
	f.r = router
	const path = "/v1/hrm/rules/promotion"
	count := func() int { return len(f.call("hcth", http.MethodGet, path, nil, http.StatusOK)["versions"].([]any)) }
	before := count()
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		for _, bad := range []struct{ name, value string }{{"equivalent_exponent", "1e0"}, {"positive_exponent", "1e100000"}, {"negative_exponent", "1e-100000"}, {"long", strings.Repeat("9", 100000)}, {"nan", "NaN"}, {"infinity", "Infinity"}, {"precision", "0.001"}, {"over_max", "100.01"}} {
			for _, quoted := range []bool{true, false} {
				kind := "number"
				if quoted {
					kind = "string"
				}
				t.Run(key+"/"+bad.name+"/"+kind, func(t *testing.T) {
					body := promoRules("20")
					payload := body["payload"].(map[string]any)
					payload[key] = "REPLACE197"
					raw, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					value := bad.value
					if quoted {
						b, _ := json.Marshal(value)
						value = string(b)
					}
					wire := strings.Replace(string(raw), `"REPLACE197"`, value, 1)
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(wire))
					request.Header.Set("X-Test-As", "admin")
					response := httptest.NewRecorder()
					f.r.ServeHTTP(response, request)
					want := http.StatusUnprocessableEntity
					if response.Code != want {
						t.Fatalf("status %d want %d; body bytes=%d", response.Code, want, response.Body.Len())
					}
					if response.Body.Len() > 2048 {
						t.Fatalf("unbounded error response: %d", response.Body.Len())
					}
					if got := count(); got != before {
						t.Fatalf("rejected request appended: %d -> %d", before, got)
					}
				})
			}
		}
		for _, good := range []string{"0", "100", "12.34"} {
			for _, quoted := range []bool{true, false} {
				body := promoRules("20")
				var value any = json.Number(good)
				if quoted {
					value = good
				}
				body["payload"].(map[string]any)[key] = value
				f.call("admin", http.MethodPost, path, body, http.StatusCreated)
				before++
				if got := count(); got != before {
					t.Fatalf("ordinary value did not append once: %d want %d", got, before)
				}
			}
		}
	}
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		body := promoRules("20")
		payload := body["payload"].(map[string]any)
		delete(payload, key)
		payload[strings.ToUpper(key)] = "1e100000"
		f.call("admin", http.MethodPost, path, body, http.StatusUnprocessableEntity)
		if count() != before {
			t.Fatal("case-folded field appended a rule")
		}
	}
	for _, key := range []string{"nlth_quota_pct", "tnvk_start_pct", "tnvk_step_pct"} {
		body := promoRules("20")
		raw, err := json.Marshal(body["payload"])
		if err != nil {
			t.Fatal(err)
		}
		wire := `{"valid_from":"2026-06-01","payload":{"` + key + `":"1e100000",` + string(raw[1:]) + `}`
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(wire))
		request.Header.Set("X-Test-As", "admin")
		response := httptest.NewRecorder()
		f.r.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity || response.Body.Len() > 2048 {
			t.Fatalf("duplicate field status=%d bytes=%d", response.Code, response.Body.Len())
		}
		if count() != before {
			t.Fatal("overwritten duplicate field appended a rule")
		}
	}
	f.call("hcth", http.MethodPost, path, promoRules("20"), http.StatusForbidden)
	if count() != before {
		t.Fatal("HCTH changed a rule")
	}
	if got := f.call("other", http.MethodGet, path, nil, http.StatusOK)["versions"].([]any); len(got) != 0 {
		t.Fatal("cross-tenant rule history visible")
	}
}
