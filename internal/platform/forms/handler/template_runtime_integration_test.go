package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

// The owner registry below is a generic contract fixture. It deliberately has no
// CNTS rules and is separate from native account-authority evidence in tests/kn77.
func TestRuntime303RESTMetadataLimitsCopyAndPreCachePolicy(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	owner, tid, program := ids.New(), ids.New(), ids.New()
	allowed := true
	principal := authz.Principal{TenantID: tid, ActorRef: owner, Roles: []string{"author"}}
	ctx := func() context.Context { return authz.WithPrincipal(context.Background(), principal) }
	profile := formsport.Profile{Name: "runtime303", Hierarchy: []string{"UPPER", "LEAF"}, Roles: map[string]string{"author": "UPPER"}, DefineBy: []string{"author"}, PublishBy: "UPPER", CloseBy: "UPPER", RequireDefinitionPolicy: true}
	policy := func(_ context.Context, r formsport.TemplateDefinitionRequest) (formsport.TemplateDefinitionDecision, error) {
		grant := formsport.TemplateDefinitionDecision{Allowed: allowed && r.Principal.ActorRef == owner, Binding: "verified-generic-owner-grant"}
		if r.Proposed.Runtime != nil {
			var config struct {
				Program   string `json:"program"`
				Frequency string `json:"frequency"`
			}
			if json.Unmarshal(r.Proposed.Runtime.Config, &config) != nil || config.Program != program.String() {
				grant.Allowed = false
				return grant, nil
			}
			grant.RuntimeValidated = r.Proposed.Runtime.Contract == "sample.runtime/v1" && config.Frequency == "daily"
			// An owner must never be able to mutate the payload being persisted.
			r.Proposed.Runtime.Config[0] = '['
		}
		return grant, nil
	}
	fm, err := forms.New(pool, nil, forms.Deps{Profiles: []formsport.Profile{profile}, DefinitionPolicies: map[string]formsport.TemplateDefinitionPolicy{profile.Name: policy}})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx()) }, fm.Middleware(), idempotency.Middleware(db.ForModule(pool, db.Ops)))
	fm.RegisterApp(router.Group("/v1"))
	call := func(method, path, key string, raw []byte, want int) map[string]json.RawMessage {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set(idempotency.Header, key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got%d want%d: %s", method, path, w.Code, want, w.Body.String())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal("invalid response JSON")
		}
		return result
	}
	fields := json.RawMessage(`[{"key":"count","label":"Count","type":"integer","required":false}]`)
	runtime := &formsport.RuntimeDefinition{Schema: formsport.RuntimeDefinitionV1, Contract: "sample.runtime/v1", Config: json.RawMessage(`{"program":"` + program.String() + `","frequency":"daily","gps":true,"photo":true,"threshold":"12.500"}`)}
	body := map[string]any{"profile": profile.Name, "code": "CUSTOM-303", "title": "REST runtime", "fields": fields, "base_version": 0, "runtime": runtime}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	first := call(http.MethodPost, "/v1/forms/templates", "rest303-original", raw, http.StatusCreated)
	var identity formsport.TemplateIdentity
	_ = json.Unmarshal(first["identity"], &identity)
	var saved formsport.RuntimeDefinition
	if err = json.Unmarshal(first["runtime"], &saved); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(saved.Config) || saved.Contract != runtime.Contract || !bytes.Contains(saved.Config, []byte(program.String())) {
		t.Fatal("owner mutated or metadata was dropped")
	}
	zero := int64(0)
	native, err := fm.Publisher().DefineTemplate(ctx(), formsport.TemplateDef{Profile: profile.Name, Code: "CUSTOM-303", Title: "REST runtime", Fields: fields, Runtime: runtime, BaseVersion: &zero}, "rest303-original")
	if err != nil || native.Identity != identity || native.Runtime == nil {
		t.Fatalf("REST/Publisher config receipt mismatch: %v", err)
	}
	copyRequest := formsport.TemplateCopy{Source: identity, Code: "COPY-303", Title: "REST copy", BaseVersion: 0}
	copyRaw, _ := json.Marshal(copyRequest)
	copied := call(http.MethodPost, "/v1/forms/templates/"+identity.ID.String()+"/copy", "rest303-copy", copyRaw, http.StatusCreated)
	var copiedIdentity formsport.TemplateIdentity
	_ = json.Unmarshal(copied["identity"], &copiedIdentity)
	if copiedIdentity.RuntimeDigest != identity.RuntimeDigest || copiedIdentity.SchemaDigest != identity.SchemaDigest {
		t.Fatal("REST copy lost runtime/schema")
	}
	usage := call(http.MethodGet, "/v1/forms/templates/"+identity.ID.String()+"/usage?profile="+profile.Name, "", nil, http.StatusOK)
	if string(usage["state"]) != `"unused"` || string(usage["has_data"]) != "false" {
		t.Fatal("new exact usage missing")
	}
	for i, number := range []string{"1e100000", "1e-100000", strings.Repeat("9", 20000)} {
		hostile := bytes.Replace(raw, []byte(`"threshold":"12.500"`), []byte(`"threshold":`+number), 1)
		call(http.MethodPost, "/v1/forms/templates", "rest303-number-"+string(rune('a'+i)), hostile, http.StatusUnprocessableEntity)
	}
	unsupported := bytes.Replace(raw, []byte("sample.runtime/v1"), []byte("sample.runtime/v2"), 1)
	call(http.MethodPost, "/v1/forms/templates", "rest303-unsupported", unsupported, http.StatusUnprocessableEntity)
	spoof := bytes.Replace(raw, []byte(`"base_version":0`), []byte(`"base_version":0,"runtime_validated":true`), 1)
	call(http.MethodPost, "/v1/forms/templates", "rest303-spoof", spoof, http.StatusBadRequest)
	unknownField := bytes.Replace(raw, []byte(`"type":"integer"`), []byte(`"type":"integer","camera":true`), 1)
	call(http.MethodPost, "/v1/forms/templates", "rest303-unknown-field", unknownField, http.StatusBadRequest)
	allowed = false
	call(http.MethodPost, "/v1/forms/templates", "rest303-original", raw, http.StatusForbidden)
	call(http.MethodPost, "/v1/forms/templates/"+identity.ID.String()+"/copy", "rest303-copy", copyRaw, http.StatusForbidden)
	if _, err = fm.Publisher().DefineTemplate(ctx(), formsport.TemplateDef{Profile: profile.Name, Code: "CUSTOM-303", Title: "REST runtime", Fields: fields, Runtime: runtime, BaseVersion: &zero}, "rest303-original"); err == nil {
		t.Fatal("Publisher replay skipped current owner authority")
	}
}
