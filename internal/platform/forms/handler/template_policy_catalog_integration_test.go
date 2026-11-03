package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

func TestTemplateCatalogHTTPScopedExactAndHistory(t *testing.T) {
	env := pgtest.Get(t)
	type subjectKey struct{}
	profile := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}}
	profile.ResolveActor = func(ctx context.Context, _ authz.Principal, _ port.Profile) (port.ResolvedActor, bool, error) {
		a, ok := ctx.Value(subjectKey{}).(port.ResolvedActor)
		return a, ok, nil
	}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)), nil, []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	tid := ids.New()
	adminCtx := context.WithValue(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid}), subjectKey{}, port.ResolvedActor{Role: "admin", Scope: authz.Scope{All: true}})
	in := app.TemplateInput{Profile: "fx", Code: "CUSTOM-191", Title: "Original", Fields: domain.Fixtures()[0].Fields}
	first, err := svc.DefineTemplate(adminCtx, in, "http-catalog-first")
	if err != nil {
		t.Fatal(err)
	}
	in.Title = "Current"
	second, err := svc.DefineTemplate(adminCtx, in, "http-catalog-next")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		tenant := tid
		if c.GetHeader("X-Test-Mode") == "foreign" {
			tenant = ids.New()
		}
		subject := port.ResolvedActor{Role: "tinh", Scope: authz.Scope{All: true}}
		if c.GetHeader("X-Test-Mode") == "blocked" {
			subject.AllowsTemplate = func(string) bool { return false }
		}
		ctx := context.WithValue(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: ids.New(), TenantID: tenant}), subjectKey{}, subject)
		c.Request = c.Request.WithContext(ctx)
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(path, mode string, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Test-Mode", mode)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s mode%s status%d want%d: %s", path, mode, w.Code, want, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	history := call("/v1/forms/template-versions?profile=fx&code=CUSTOM-191&limit=1", "", http.StatusOK)["items"].([]any)
	if len(history) != 1 || history[0].(map[string]any)["id"] != second.ID.String() {
		t.Fatalf("history current: %+v", history)
	}
	old := call("/v1/forms/template-versions/1?profile=fx&code=CUSTOM-191", "", http.StatusOK)
	if old["id"] != first.ID.String() || old["title"] != "Original" {
		t.Fatalf("exact version: %+v", old)
	}
	previous := call("/v1/forms/template-versions?profile=fx&code=CUSTOM-191&before_version=2", "", http.StatusOK)["items"].([]any)
	if len(previous) != 1 || previous[0].(map[string]any)["id"] != first.ID.String() {
		t.Fatal("cursor did not select old version")
	}
	call("/v1/forms/template-versions/1?profile=fx&code=CUSTOM-191", "blocked", http.StatusNotFound)
	call("/v1/forms/template-versions?profile=fx&code=CUSTOM-191", "blocked", http.StatusNotFound)
	call("/v1/forms/template-versions/1?profile=fx&code=CUSTOM-191", "foreign", http.StatusNotFound)
	foreign := call("/v1/forms/template-versions?profile=fx&code=CUSTOM-191", "foreign", http.StatusOK)["items"].([]any)
	if len(foreign) != 0 {
		t.Fatal("foreign tenant history leaked")
	}
	call("/v1/forms/template-versions?profile=fx&code=", "", http.StatusUnprocessableEntity)
	call("/v1/forms/template-versions/0?profile=fx&code=CUSTOM-191", "", http.StatusUnprocessableEntity)
}
