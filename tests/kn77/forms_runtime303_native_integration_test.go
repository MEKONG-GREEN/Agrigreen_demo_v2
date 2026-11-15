package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real router authentication, public account/assignment/context APIs and actual
// #191 CNTS owner policy are used here. The harness has a signed test OIDC issuer
// and in-memory Keycloak directory; no principal/session/MFA flags or grants are
// injected into a request, and no runtime owner from an unmerged branch is used.
func TestFormsRuntime303NativeAccountsRESTPublisherReplayAndRevoke(t *testing.T) {
	clk := clock.NewFixed(t36)
	st := harness.NewAt(t, clk)
	s := &agScenario{t: t, s: st, clk: clk, tid: harness.NewTenant()}
	s.rootA = s.admin(authz.RolePlatformAdmin)
	adm := func(method, path string, body any, status int) agResp {
		return s.want(s.rootA.adm(method, path, body), status, path)
	}
	adm(http.MethodPost, "/v1/masterdata/seed", map[string]any{}, http.StatusOK)
	s.nat = str(obj(adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_national", "code": "R303-NAT", "name": "Synthetic national"}, http.StatusCreated).body, "org"), "ref")
	s.p91 = str(obj(adm(http.MethodPost, "/v1/orgs", map[string]any{"kind": "ttkn_province", "code": "R303-PROV", "name": "Synthetic province", "parent_ref": s.nat, "admin_unit_code": agProv}, http.StatusCreated).body, "org"), "ref")
	for _, dept := range []string{"chan_nuoi_thuy_san", "trong_trot"} {
		adm(http.MethodPost, "/v1/departments", map[string]any{"org_ref": s.p91, "code": dept, "name": dept, "status": "active", "sort_order": 1, "menu_profile": dept}, http.StatusCreated)
	}
	user, assignments := s.createKN("Synthetic runtime author", "runtime303-author@example.invalid", "0912303001", asgKN(authz.KNTinh, s.p91, []string{"chan_nuoi_thuy_san", "trong_trot"}, "chuyen_vien"))
	assignment := assignments[authz.KNTinh]
	attrPath := "/v1/accounts/users/" + user.id.String() + "/assignments/" + assignment + "/attributes"
	adm(http.MethodPut, attrPath, map[string]any{"province_group": "tong_hop", "reason": "Synthetic runtime authority test"}, http.StatusOK)
	writer := user.as(assignment, "chan_nuoi_thuy_san")
	// A one-connection Forms pool catches nested owner I/O in a Forms TX.
	config := st.PF(dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fm, err := forms.New(pool, clk, forms.Deps{Profiles: extension.FormProfiles(), DefinitionPolicies: map[string]formsport.TemplateDefinitionPolicy{"kn_cnts": st.Mods.Extension.CNTSDefinitionPolicy()}})
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewEngine(telemetry.NewLogger("runtime303", io.Discard), 1<<20)
	authenticated := router.Group("/v1", httpx.Authenticate(st.Issuer.Verifier()), st.Mods.Accounts.Middleware())
	native := authenticated.Group("", fm.Middleware(), idempotency.Middleware(db.ForModule(pool, db.Ops)))
	fm.RegisterApp(native)
	// This test-only adapter exercises the public Publisher with the very same
	// authenticated account context; its durable replay does not use HTTP cache.
	authenticated.POST("/runtime303/publisher", func(c *gin.Context) {
		var req struct {
			Definition formsport.TemplateDef   `json:"definition"`
			Copy       *formsport.TemplateCopy `json:"copy"`
		}
		if c.ShouldBindJSON(&req) != nil {
			httpx.Fail(c, formsport.ErrInvalidTemplate)
			return
		}
		if req.Copy != nil {
			out, e := fm.TemplateCopies().CopyTemplate(c.Request.Context(), *req.Copy, c.GetHeader(idempotency.Header))
			if e != nil {
				httpx.Fail(c, e)
				return
			}
			c.JSON(http.StatusCreated, out)
			return
		}
		out, e := fm.Publisher().DefineTemplate(c.Request.Context(), req.Definition, c.GetHeader(idempotency.Header))
		if e != nil {
			httpx.Fail(c, e)
			return
		}
		c.JSON(http.StatusCreated, out)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	call := func(u *agUser, method, path, key string, body any, want int) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+u.token)
		request.Header.Set(authz.ContextHeader, u.ctx)
		request.Header.Set("Content-Type", "application/json")
		if key != "" {
			request.Header.Set(idempotency.Header, key)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, response.StatusCode, want, result)
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(result, &out) != nil {
			t.Fatalf("non JSON response: %s", result)
		}
		return out
	}
	fields := json.RawMessage(`[{"key":"count","label":"Count","type":"integer","required":false}]`)
	body := map[string]any{"profile": "kn_cnts", "code": "CNTS-DT-RUNTIME303", "title": "Native original", "fields": fields, "base_version": 0}
	first := call(writer, http.MethodPost, "/v1/forms/templates", "native303-original", body, http.StatusCreated)
	var identity formsport.TemplateIdentity
	if err = json.Unmarshal(first["identity"], &identity); err != nil {
		t.Fatal(err)
	}
	if identity.TenantID != s.tid || identity.Version != 1 || identity.Digest == "" {
		t.Fatal("native identity missing")
	}
	zero := int64(0)
	definition := formsport.TemplateDef{Profile: "kn_cnts", Code: "CNTS-DT-RUNTIME303", Title: "Native original", Fields: fields, BaseVersion: &zero}
	replay := call(writer, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusCreated)
	var replayID string
	_ = json.Unmarshal(replay["ID"], &replayID)
	if replayID != identity.ID.String() {
		t.Fatal("native REST/Publisher receipt mismatch")
	}
	body["title"] = "Native successor"
	body["base_version"] = 1
	call(writer, http.MethodPost, "/v1/forms/templates", "native303-successor", body, http.StatusCreated)
	replay = call(writer, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusCreated)
	_ = json.Unmarshal(replay["ID"], &replayID)
	if replayID != identity.ID.String() {
		t.Fatal("native replay selected a later head")
	}
	copyRequest := formsport.TemplateCopy{Source: identity, Code: "CNTS-DT-COPY303", Title: "Native copy", BaseVersion: 0}
	copied := call(writer, http.MethodPost, "/v1/forms/templates/"+identity.ID.String()+"/copy", "native303-copy", copyRequest, http.StatusCreated)
	var copyIdentity formsport.TemplateIdentity
	_ = json.Unmarshal(copied["identity"], &copyIdentity)
	if copyIdentity.SchemaDigest != identity.SchemaDigest || copyIdentity.ID == identity.ID {
		t.Fatal("copy did not preserve content and create real identity")
	}
	call(writer, http.MethodPost, "/v1/runtime303/publisher", "native303-copy", map[string]any{"copy": copyRequest}, http.StatusCreated)
	// The actual owner has not registered runtime support yet. No shared path may
	// discard an unsupported config or accept a caller's validation/authority flag.
	unsupported := map[string]any{"profile": "kn_cnts", "code": "CNTS-DT-UNSUPPORTED303", "title": "Unsupported", "fields": fields, "base_version": 0, "runtime": map[string]any{"schema": formsport.RuntimeDefinitionV1, "contract": "sample.runtime/v1", "config": map[string]any{"gps": true}}}
	call(writer, http.MethodPost, "/v1/forms/templates", "native303-unsupported", unsupported, http.StatusUnprocessableEntity)
	unsupported["runtime_validated"] = true
	call(writer, http.MethodPost, "/v1/forms/templates", "native303-spoof", unsupported, http.StatusBadRequest)
	otherDepartment := user.as(assignment, "trong_trot")
	call(otherDepartment, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusForbidden)
	// A fresh legitimate context is a different session, not the original receipt.
	changedSession := user.as(assignment, "chan_nuoi_thuy_san")
	call(changedSession, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusUnprocessableEntity)
	// Current read-only and revoked state come from public account writes, then
	// real middleware re-resolves them before either cache or durable replay.
	updated := asgKN(authz.KNTinh, s.p91, []string{"chan_nuoi_thuy_san", "trong_trot"}, "pho_giam_doc")
	updated["reason"] = "Synthetic read-only transition"
	adm(http.MethodPut, "/v1/accounts/users/"+user.id.String()+"/assignments/"+assignment, updated, http.StatusOK)
	adm(http.MethodPut, attrPath, map[string]any{"province_group": "ban_giam_doc", "reason": "Synthetic read-only group"}, http.StatusOK)
	readOnly := user.as(assignment, "chan_nuoi_thuy_san")
	body["title"] = "Native original"
	body["base_version"] = 0
	call(readOnly, http.MethodPost, "/v1/forms/templates", "native303-original", body, http.StatusForbidden)
	call(readOnly, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusForbidden)
	adm(http.MethodPost, "/v1/accounts/users/"+user.id.String()+"/assignments/"+assignment+"/revoke", map[string]any{"reason": "Synthetic revoke"}, http.StatusCreated)
	call(writer, http.MethodPost, "/v1/forms/templates", "native303-original", body, http.StatusForbidden)
	call(writer, http.MethodPost, "/v1/runtime303/publisher", "native303-original", map[string]any{"definition": definition}, http.StatusForbidden)
}
