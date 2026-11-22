package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

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

// #175 (review Codex tại 3001fed6): GET /v1/forms/periods/{id}/submissions phải trả thông tin phiên bản đã lưu (sổ phụ
// submission_meta: nộp trễ, lý do/giải trình, ghi chú, không phát sinh) cho TỪNG phiên bản — màn Tỉnh BM-04 dựng lịch sử
// phiên bản (FN-09 AC-05) từ đây. Trước sửa serializer GET bỏ Meta → late=false, explanation=null dù POST trả đúng.
func TestFormsSubmissionsHistoryMeta_175(t *testing.T) {
	env := pgtest.Get(t)
	prof := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH",
		CloseBy: "TINH", DefineBy: []string{"admin"}, AllowFixtures: true}
	type subjectKey struct{}
	prof.ResolveActor = func(ctx context.Context, _ authz.Principal, _ port.Profile) (port.ResolvedActor, bool, error) {
		subject, ok := ctx.Value(subjectKey{}).(port.ResolvedActor)
		return subject, ok, nil
	}
	clk := clock.NewFixed(time.Now())
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, nil, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid, node := ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid},
		"tinh":  {ActorRef: ids.New(), TenantID: tid},
		"xa":    {ActorRef: ids.New(), TenantID: tid},
	}
	subjects := map[string]port.ResolvedActor{
		"admin": {Role: "admin", Scope: authz.Scope{All: true}},
		"tinh":  {Role: "tinh", Node: node, Scope: authz.Scope{Units: []string{"U1"}}},
		"xa":    {Role: "xa", Scope: authz.Scope{Units: []string{"U1"}}},
	}
	r.Use(func(c *gin.Context) {
		key := c.GetHeader("X-Test-As")
		if p, ok := who[key]; ok {
			c.Request = c.Request.WithContext(context.WithValue(authz.WithPrincipal(c.Request.Context(), p), subjectKey{}, subjects[key]))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		raw := []byte(nil)
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
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

	tpl := call("admin", http.MethodPost, "/v1/forms/templates/fixtures?profile=fx", nil, http.StatusCreated)["items"].([]any)[1].(map[string]any)
	pv := call("tinh", http.MethodPost, "/v1/forms/periods", map[string]any{"profile": "fx", "template_id": tpl["id"], "label": "Kỳ lịch sử #175",
		"due_at": clk.Now().Add(time.Hour).Format(time.RFC3339), "units": []string{"U1"}}, http.StatusCreated)
	pid := pv["period"].(map[string]any)["id"].(string)
	q := "/v1/forms/periods/" + pid + "/submissions?profile=fx"

	// Phiên bản 1 trước hạn, có ghi chú; phiên bản 2 sau hạn (đồng hồ giả lập), có lý do sửa sau hạn.
	v1 := call("xa", http.MethodPost, q, map[string]any{"unit": "U1", "values": map[string]string{"pest": "rầy nâu", "area_infected": "1"},
		"note": "ghi chú v1"}, http.StatusCreated)
	clk.Advance(2 * time.Hour)
	v2 := call("xa", http.MethodPost, q, map[string]any{"unit": "U1", "values": map[string]string{"pest": "rầy nâu", "area_infected": "2"},
		"base_version": 1, "explanation": "xã bổ sung sau hạn"}, http.StatusCreated)
	if v1["late"] != false || v2["late"] != true || v2["explanation"] != "xã bổ sung sau hạn" {
		t.Fatalf("POST meta: v1=%v v2=%v", v1, v2)
	}

	items := call("tinh", http.MethodGet, q+"&unit=U1", nil, http.StatusOK)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("lịch sử: %v", items)
	}
	byVer := map[float64]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		byVer[m["version"].(float64)] = m
	}
	g1, g2 := byVer[1], byVer[2]
	if g1 == nil || g2 == nil {
		t.Fatalf("thiếu phiên bản: %v", items)
	}
	if g2["late"] != true || g2["explanation"] != "xã bổ sung sau hạn" || g2["note"] != nil {
		t.Fatalf("GET phiên bản 2 phải giữ late/explanation đã lưu: %v", g2)
	}
	if g1["late"] != false || g1["note"] != "ghi chú v1" || g1["explanation"] != nil {
		t.Fatalf("GET phiên bản 1 phải giữ ghi chú đã lưu: %v", g1)
	}
}
