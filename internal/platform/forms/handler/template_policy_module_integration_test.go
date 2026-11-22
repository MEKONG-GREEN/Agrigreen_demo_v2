package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

func TestTemplateDefinitionModuleRESTPublisherAndRealCache(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	tid, actorRef, node := ids.New(), ids.New(), ids.New()
	principal := authz.Principal{ActorRef: actorRef, TenantID: tid, Roles: []string{"admin"}}
	subject := port.ResolvedActor{Role: "tinh", Department: "owner_department", Node: node, Scope: authz.Scope{All: true}}
	type subjectKey struct{}
	profile := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, RequireDefinitionPolicy: true}
	profile.ResolveActor = func(ctx context.Context, _ authz.Principal, _ port.Profile) (port.ResolvedActor, bool, error) {
		a, ok := ctx.Value(subjectKey{}).(port.ResolvedActor)
		return a, ok, nil
	}
	locked := map[string]bool{}
	var ownerError error
	zeroDecision := false
	policy := func(_ context.Context, r port.TemplateDefinitionRequest) (port.TemplateDefinitionDecision, error) {
		if ownerError != nil {
			return port.TemplateDefinitionDecision{Allowed: true}, ownerError
		}
		if zeroDecision {
			return port.TemplateDefinitionDecision{}, nil
		}
		return port.TemplateDefinitionDecision{Allowed: r.Principal.TenantID == tid && r.Principal.ActorRef == actorRef && r.Actor.Role == "tinh" && r.Actor.Department == "owner_department" && !locked[r.Proposed.Code]}, nil
	}
	mod, err := forms.New(pool, clock.NewFixed(time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)), forms.Deps{Profiles: []port.Profile{profile}, DefinitionPolicies: map[string]port.TemplateDefinitionPolicy{"fx": policy}})
	if err != nil {
		t.Fatal(err)
	}
	scopeBinding := []byte("selected-owner-session-A")
	currentContext := func() context.Context {
		ctx := context.WithValue(authz.WithPrincipal(context.Background(), principal), subjectKey{}, subject)
		return idempotency.WithAuthorizationScope(ctx, scopeBinding)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(currentContext()) })
	r.Use(mod.Middleware(), idempotency.Middleware(db.ForModule(pool, db.Ops)))
	mod.RegisterApp(r.Group("/v1"))
	fields := domain.Fixtures()[0].Fields
	rawFields, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	body := func(code, title string, base int64) []byte {
		t.Helper()
		raw, e := json.Marshal(map[string]any{"profile": "fx", "code": code, "title": title, "fields": fields, "base_version": base})
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	call := func(method, path, key string, raw []byte, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set(idempotency.Header, key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s status%d want%d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		decoder := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
		decoder.UseNumber()
		if e := decoder.Decode(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	for i, hostile := range []string{"1e100000", "1e-100000", strings.Repeat("9", 20000), `"` + strings.Repeat("9", 20000) + `"`} {
		raw := bytes.Replace(body("HOSTILE-191", "Hostile", 0), []byte(`"base_version":0`), []byte(`"base_version":`+hostile), 1)
		call(http.MethodPost, "/v1/forms/templates", "definition191-hostile-"+string(rune('a'+i)), raw, http.StatusBadRequest)
	}
	missingBase := map[string]any{"profile": "fx", "code": "MISSING-191", "title": "Missing base", "fields": fields}
	missingBody, err := json.Marshal(missingBase)
	if err != nil {
		t.Fatal(err)
	}
	call(http.MethodPost, "/v1/forms/templates", "definition191-missing-base", missingBody, http.StatusUnprocessableEntity)
	if _, err := mod.Publisher().DefineTemplate(currentContext(), port.TemplateDef{Profile: "fx", Code: "MISSING-191", Title: "Missing base", Fields: rawFields}, "definition191-missing-base"); err == nil {
		t.Fatal("Publisher accepted missing base_version")
	}
	originalBody := body("CUSTOM-191", "Original", 0)
	first := call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusCreated)
	base := int64(0)
	def := port.TemplateDef{Profile: "fx", Code: "CUSTOM-191", Title: "Original", Fields: rawFields, BaseVersion: &base}
	replay, err := mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original")
	if err != nil || replay.ID.String() != first["id"] || replay.Version != 1 {
		t.Fatalf("REST/Publisher durable replay: %+v %v", replay, err)
	}
	second := call(http.MethodPost, "/v1/forms/templates", "definition191-next", body("CUSTOM-191", "Next", 1), http.StatusCreated)
	if second["id"] == first["id"] || second["version"] != json.Number("2") {
		t.Fatal("second version not appended")
	}
	replay, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original")
	if err != nil || replay.ID.String() != first["id"] {
		t.Fatalf("durable replay after latest advanced: %+v %v", replay, err)
	}
	// Realm roles, identity, header, raw body and cache hash stay unchanged. Only the
	// freshly resolved owner policy changes, so this exercises the pre-cache seam.
	locked["CUSTOM-191"] = true
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusForbidden)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); err == nil {
		t.Fatal("Publisher bypassed structural lock")
	}
	delete(locked, "CUSTOM-191")
	zeroDecision = true
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusForbidden)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); err == nil {
		t.Fatal("Publisher accepted zero decision")
	}
	zeroDecision = false
	ownerError = errors.New("owner unavailable")
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusInternalServerError)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); !errors.Is(err, ownerError) {
		t.Fatalf("Publisher swallowed owner failure: %v", err)
	}
	ownerError = nil
	subject.Department = "different_department"
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusForbidden)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); err == nil {
		t.Fatal("Publisher bypassed department restriction")
	}
	subject.Department = "owner_department"
	subject.Role = "readonly"
	call(http.MethodPost, "/v1/forms/templates", "definition191-role-denied", body("DENIED-191", "Denied", 0), http.StatusForbidden)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); err == nil {
		t.Fatal("Publisher fell back to realm admin")
	}
	subject.Role = "tinh"
	cached := call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusCreated)
	if cached["id"] != first["id"] {
		t.Fatal("permitted cache retry did not keep original ID")
	}
	// Owner middleware must preserve native selected-session, MFA, realm-role and
	// organization bindings already installed by upstream authentication.
	scopeBinding = []byte("selected-owner-session-B")
	mismatch := call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusUnprocessableEntity)
	if mismatch["code"] != "idempotency_key_reused" {
		t.Fatal("selected-session binding was overwritten")
	}
	scopeBinding = []byte("selected-owner-session-A")
	principal.MFA = true
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusUnprocessableEntity)
	principal.MFA = false
	principal.Roles = []string{"admin", "extra-role"}
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusUnprocessableEntity)
	principal.Roles = []string{"admin"}
	originalOrg := principal.OrgRef
	principal.OrgRef = ids.New()
	call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusUnprocessableEntity)
	principal.OrgRef = originalOrg
	restored := call(http.MethodPost, "/v1/forms/templates", "definition191-original", originalBody, http.StatusCreated)
	if restored["id"] != first["id"] {
		t.Fatal("native bindings could not restore original retry")
	}
	// Permission does not hide old versions or the current catalog from readers.
	locked["CUSTOM-191"] = true
	old := call(http.MethodGet, "/v1/forms/template-versions/1?profile=fx&code=CUSTOM-191", "", nil, http.StatusOK)
	if old["id"] != first["id"] {
		t.Fatal("catalog exact version changed")
	}
	entries, err := mod.TemplateCatalog().ListTemplates(currentContext(), "fx")
	if err != nil || len(entries) != 1 || entries[0].ID.String() != second["id"] {
		t.Fatalf("module catalog: %+v %v", entries, err)
	}
	delete(locked, "CUSTOM-191")
	originalTenant := principal.TenantID
	principal.TenantID = ids.New()
	call(http.MethodGet, "/v1/forms/template-versions/1?profile=fx&code=CUSTOM-191", "", nil, http.StatusNotFound)
	if _, err = mod.Publisher().DefineTemplate(currentContext(), def, "definition191-original"); err == nil {
		t.Fatal("Publisher allowed foreign tenant intent")
	}
	principal.TenantID = originalTenant
	// A stale new request cannot append; the winning version remains two.
	call(http.MethodPost, "/v1/forms/templates", "definition191-stale", body("CUSTOM-191", "Stale", 0), http.StatusConflict)
	var count int
	if err := env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT count(*) FROM pf_forms.template WHERE tenant_id=$1 AND code='CUSTOM-191'`, tid).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("denied/replayed/stale invocation appended rows: %d", count)
	}
}

func TestTemplateDefinitionModuleRequiredAndNilRegistrationDeny(t *testing.T) {
	profile := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, RequireDefinitionPolicy: true}
	makeCall := func(mod *forms.Module) {
		t.Helper()
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{"admin"}}))
		})
		r.Use(mod.Middleware())
		mod.RegisterApp(r.Group("/v1"))
		req := httptest.NewRequest(http.MethodPost, "/v1/forms/templates", bytes.NewBufferString(`{"profile":"fx","code":"DENIED-191","title":"Denied","fields":[{"key":"value","label":"Value","type":"text"}],"base_version":0}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("missing/nil owner policy did not deny before DB: %d %s", w.Code, w.Body.String())
		}
	}
	mod, err := forms.New(nil, nil, forms.Deps{Profiles: []port.Profile{profile}})
	if err != nil {
		t.Fatal(err)
	}
	makeCall(mod)
	profile.RequireDefinitionPolicy = false
	mod, err = forms.New(nil, nil, forms.Deps{Profiles: []port.Profile{profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.UseDefinitionPolicy("unknown", nil); err == nil {
		t.Fatal("unknown policy registration accepted")
	}
	if err := mod.UseDefinitionPolicy("fx", nil); err != nil {
		t.Fatal(err)
	}
	makeCall(mod)
	if _, err = forms.New(nil, nil, forms.Deps{Profiles: []port.Profile{profile}, DefinitionPolicies: map[string]port.TemplateDefinitionPolicy{"unknown": nil}}); err == nil {
		t.Fatal("unknown configured policy accepted")
	}
}
