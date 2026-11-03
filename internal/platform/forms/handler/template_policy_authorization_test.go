package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/gin-gonic/gin"
)

func TestTemplateDefinitionGuardBeforeCachedSuccess(t *testing.T) {
	profile := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}}
	svc, err := app.NewService(nil, nil, nil, []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	principal := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{"admin"}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), principal))
	})
	r.Use((Handler{Svc: svc}).TemplateDefinitionAuthorization())
	cacheHits := 0
	body := `{"profile":"fx","code":"GUARD-191","title":"Schema","fields":[{"key":"value","label":"Value","type":"text"}],"base_version":0}`
	r.POST("/v1/forms/templates", func(c *gin.Context) {
		cacheHits++
		actual, err := io.ReadAll(c.Request.Body)
		if err != nil || string(actual) != body {
			t.Error("guard did not restore request body")
		}
		c.JSON(http.StatusCreated, gin.H{"id": "saved-cache-response"})
	})
	call := func(payload string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/forms/templates", bytes.NewBufferString(payload))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("status%d want%d: %s", w.Code, want, w.Body.String())
		}
	}
	call(body, http.StatusCreated)
	if cacheHits != 1 {
		t.Fatal("legacy permission was not compatible")
	}
	principal.Roles = nil
	call(body, http.StatusForbidden)
	principal.Roles = []string{"admin"}
	svc.RequiredDefinitionPolicies = map[string]bool{"fx": true}
	call(body, http.StatusForbidden)
	if cacheHits != 1 {
		t.Fatal("cached success bypassed revoked or missing mandatory policy")
	}
	svc.RequiredDefinitionPolicies = nil
	call(strings.Replace(body, `"base_version":0`, `"base_version":-1`, 1), http.StatusUnprocessableEntity)
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 20000)} {
		call(strings.Replace(body, `"base_version":0`, `"base_version":`+value, 1), http.StatusBadRequest)
	}
	if cacheHits != 1 {
		t.Fatal("invalid base reached cached success")
	}
}
