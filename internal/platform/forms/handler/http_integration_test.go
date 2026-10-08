package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Route /v1/forms/* qua HTTP (người gọi gắn bằng middleware giả): cấu hình, cài biểu mẫu thử, phát hành, nộp, rà
// soát, chốt, ảnh chụp kèm nội dung — hình dạng JSON khớp contracts/openapi/application.yaml (mục pf/forms).
func TestFormsHTTP_W62(t *testing.T) {
	env := pgtest.Get(t)
	prof := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH",
		CloseBy: "TINH", DefineBy: []string{"admin"}, AllowFixtures: true}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(time.Now()), nil,
		[]port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid, node, xaRef := ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "tinh", OrgRef: node, Scope: authz.Scope{Units: []string{"U1"}}}},
		"xa":    {ActorRef: xaRef, TenantID: tid, KN: &authz.KNContext{Role: "xa", Scope: authz.Scope{Units: []string{"U1"}}}},
	}
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-As", as)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		if w.Code != want {
			t.Fatalf("%s %s: %d (muốn %d) %s", method, path, w.Code, want, w.Body.String())
		}
		return m
	}

	if items := call("xa", http.MethodGet, "/v1/forms/profiles", nil, http.StatusOK)["items"].([]any); len(items) != 1 {
		t.Fatalf("profiles: %v", items)
	}
	call("", http.MethodGet, "/v1/forms/profiles", nil, http.StatusUnauthorized)
	tpls := call("admin", http.MethodPost, "/v1/forms/templates/fixtures?profile=fx", nil, http.StatusCreated)["items"].([]any)
	tpl := tpls[1].(map[string]any)
	if tpl["fixture"] != true || tpl["code"] != "FX-02" {
		t.Fatalf("fixture: %v", tpl)
	}
	call("tinh", http.MethodPost, "/v1/forms/periods", map[string]any{"profile": "fx", "template_id": tpl["id"], "label": "Kỳ HTTP",
		"due_at": "bad", "units": []string{"U1"}}, http.StatusUnprocessableEntity)
	pv := call("tinh", http.MethodPost, "/v1/forms/periods", map[string]any{"profile": "fx", "template_id": tpl["id"], "label": "Kỳ HTTP",
		"due_at": time.Now().Add(time.Hour).Format(time.RFC3339), "units": []string{"U1"}}, http.StatusCreated)
	pid := pv["period"].(map[string]any)["id"].(string)
	if pv["status"] != "open" || len(pv["events"].([]any)) != 1 {
		t.Fatalf("phát hành: %v", pv)
	}
	sub := call("xa", http.MethodPost, "/v1/forms/periods/"+pid+"/submissions?profile=fx",
		map[string]any{"unit": "U1", "values": map[string]string{"pest": "rầy nâu", "area_infected": "2.25"}}, http.StatusCreated)
	if sub["status"] != "submitted" || sub["submitter_ref"] != xaRef.String() {
		t.Fatalf("nộp: %v", sub)
	}
	call("tinh", http.MethodPost, "/v1/forms/submissions/"+sub["id"].(string)+"/review?profile=fx", map[string]any{"decision": "accepted"}, http.StatusOK)
	cv := call("tinh", http.MethodPost, "/v1/forms/periods/"+pid+"/close?profile=fx", nil, http.StatusOK)
	snaps := cv["snapshots"].([]any)
	if cv["status"] != "closed" || len(snaps) != 1 || snaps[0].(map[string]any)["hash_valid"] != true || len(snaps[0].(map[string]any)["content_hash"].(string)) != 64 {
		t.Fatalf("chốt: %v", cv)
	}
	sv := call("tinh", http.MethodGet, "/v1/forms/snapshots/"+snaps[0].(map[string]any)["id"].(string)+"?profile=fx", nil, http.StatusOK)
	units := sv["content"].(map[string]any)["units"].([]any)
	if units[0].(map[string]any)["values"].(map[string]any)["area_infected"] != "2.25" {
		t.Fatalf("nội dung ảnh chụp: %v", sv)
	}
	call("tinh", http.MethodGet, "/v1/forms/periods/"+uuid.NewString()+"?profile=fx", nil, http.StatusNotFound)
	call("tinh", http.MethodGet, "/v1/forms/periods/not-a-uuid?profile=fx", nil, http.StatusNotFound)
	call("tinh", http.MethodPost, "/v1/forms/periods/"+pid+"/reopen?profile=fx", map[string]any{"reason": "Xã bổ sung số liệu sâu bệnh"}, http.StatusOK)
	if h := call("xa", http.MethodGet, "/v1/forms/periods/"+pid+"/submissions?profile=fx&unit=U1", nil, http.StatusOK)["items"].([]any); len(h) != 1 {
		t.Fatalf("lịch sử: %v", h)
	}
}
