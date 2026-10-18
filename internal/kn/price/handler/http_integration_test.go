package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Khung W11.71 (ADR-0049) qua HTTP: trạng thái, ghi danh mục mặc định (chỉ vai quản trị, lặp lại không ghi trùng), đọc
// danh mục theo ngày hiệu lực, cách ly tenant (RLS), port.Catalog theo tenant của ctx — hình dạng JSON khớp
// contracts/openapi/application.yaml (mục pf/price).
func TestPriceFrameHTTP_W1171(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // 10:00 giờ VN
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA"},
		ManageBy: []string{"admin"}}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now),
		[]port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid, other := ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":   {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong"}},
		"xa":     {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}}, // Cấp Xã gắn Nút (FD)
		"tw":     {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"other":  {ActorRef: ids.New(), TenantID: other, KN: &authz.KNContext{Role: "kn_tinh"}},
		"realm":  {ActorRef: ids.New(), TenantID: tid, Roles: []string{"kn_xa"}},
		"nobody": {ActorRef: ids.New(), TenantID: tid, Roles: []string{"htx_member"}},
	}
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path string, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(nil))
		req.Header.Set("X-Test-As", as)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return m
	}

	call("", http.MethodGet, "/v1/price/status", http.StatusUnauthorized)
	call("nobody", http.MethodGet, "/v1/price/status", http.StatusForbidden)
	call("tw", http.MethodGet, "/v1/price/status", http.StatusForbidden) // profile thử không có kn_tw
	call("tinh", http.MethodGet, "/v1/price/status?profile=htx", http.StatusNotFound)
	st := call("tinh", http.MethodGet, "/v1/price/status", http.StatusOK)
	if st["profile"] != "kn" || st["level"] != "TINH" || st["can_manage"] != false || fmt.Sprint(st["units"]) != "0" || st["day"] != "2026-10-12" ||
		st["rule_version"] != "price.v1" || st["department"] != "thong_tin_truyen_thong" {
		t.Fatalf("status: %v", st)
	}

	call("tinh", http.MethodPost, "/v1/price/catalog/defaults", http.StatusForbidden)
	if n := call("admin", http.MethodPost, "/v1/price/catalog/defaults", http.StatusCreated)["written"]; fmt.Sprint(n) != "15" { // 8 ĐVT + 5 nhóm hàng + 2 nhóm vùng (W11.72)
		t.Fatalf("ghi mặc định: %v", n)
	}
	if n := call("admin", http.MethodPost, "/v1/price/catalog/defaults", http.StatusCreated)["written"]; fmt.Sprint(n) != "0" {
		t.Fatalf("ghi lại mặc định phải không ghi gì: %v", n)
	}

	cat := call("xa", http.MethodGet, "/v1/price/catalog", http.StatusOK)
	gs, us := cat["groups"].([]any), cat["units"].([]any)
	if len(gs) != 5 || len(us) != 8 || cat["day"] != "2026-10-12" {
		t.Fatalf("danh mục: %v", cat)
	}
	if g := gs[0].(map[string]any); g["code"] != "lua_gao" || g["name"] != "Lúa gạo" || fmt.Sprint(g["version"]) != "1" || g["valid_from"] != "2026-10-12" {
		t.Fatalf("nhóm đầu: %v", g)
	}
	// Trước ngày hiệu lực: rỗng (BR-02 — áp dụng từ ngày báo giá ghi trong valid_from).
	if c := call("realm", http.MethodGet, "/v1/price/catalog?at=2026-10-11", http.StatusOK); len(c["groups"].([]any)) != 0 {
		t.Fatalf("trước hiệu lực: %v", c)
	}
	call("xa", http.MethodGet, "/v1/price/catalog?at=12/10/2026", http.StatusUnprocessableEntity)
	// Tenant khác không thấy (RLS p_tenant).
	if c := call("other", http.MethodGet, "/v1/price/catalog", http.StatusOK); len(c["groups"].([]any)) != 0 || len(c["units"].([]any)) != 0 {
		t.Fatalf("tenant khác: %v", c)
	}

	// port.Catalog: tenant lấy từ ctx.
	ctx := tenant.WithID(context.Background(), tid)
	units, err := svc.Units(ctx, now)
	if err != nil || len(units) != 8 || units[0].Code != "kg" {
		t.Fatalf("Units: %v %v", units, err)
	}
	groups, err := svc.Groups(tenant.WithID(context.Background(), other), now)
	if err != nil || len(groups) != 0 {
		t.Fatalf("Groups tenant khác: %v %v", groups, err)
	}
}
