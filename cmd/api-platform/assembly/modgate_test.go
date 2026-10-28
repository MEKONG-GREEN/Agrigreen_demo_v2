package assembly

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// W1.4: tên trong Named() là tên repo (module.yaml) của đúng gói module — cổng route chặn đúng module.
func TestNamedModulesMatchCatalog(t *testing.T) {
	manifests, err := modreg.Load("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"extension", "price", "hrm", "contact"} {
		m, ok := manifests.Get("pf/" + name)
		if !ok || m.Dir() != "internal/kn/"+name {
			t.Fatalf("namespace not moved: %s", name)
		}
	}

	seen := map[string]bool{}
	for _, n := range (Modules{}).Named() {
		if _, ok := modreg.Entry(n.Name); !ok {
			t.Fatalf("%s không có trong danh mục modreg", n.Name)
		}
		pkg := reflect.TypeOf(n.Module).Elem().PkgPath()
		manifest, ok := manifests.Get(n.Name)
		if !ok {
			t.Fatalf("%s không có manifest", n.Name)
		}
		if want := "github.com/MEKONG-GREEN/Agrigreen_demo_v2/" + manifest.Dir(); pkg != want {
			t.Fatalf("%s gắn với gói %s", n.Name, pkg)
		}
		if seen[n.Name] {
			t.Fatalf("%s lặp", n.Name)
		}
		seen[n.Name] = true
	}
	if len((Modules{}).List()) != len(seen) {
		t.Fatal("List và Named lệch")
	}
}

type fakeMod struct{ mw bool }

func (fakeMod) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
}
func (fakeMod) RegisterAdmin(*gin.RouterGroup) {}
func (f fakeMod) Middleware() gin.HandlerFunc {
	if !f.mw {
		return nil
	}
	return func(c *gin.Context) { c.Header("X-Mw", "1"); c.Next() }
}
func (fakeMod) RegisterPublic(rg *gin.RouterGroup) {
	rg.GET("/p", func(c *gin.Context) { c.Status(http.StatusOK) })
}

// Gated: route App qua cổng; middleware phiên và route công khai chuyển nguyên; gate nil → module gốc.
func TestGated(t *testing.T) {
	m := fakeMod{mw: true}
	if Gated(m, nil) != httpx.Module(m) {
		t.Fatal("gate nil phải trả module gốc")
	}
	blocked := Gated(m, func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) })
	if len(httpx.Middlewares([]httpx.Module{blocked})) != 1 {
		t.Fatal("middleware phiên phải được chuyển")
	}
	if _, ok := blocked.(publicRoutes); !ok {
		t.Fatal("route công khai phải được chuyển")
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	blocked.RegisterApp(r.Group("/v1"))
	blocked.(publicRoutes).RegisterPublic(r.Group("/pub"))
	for path, want := range map[string]int{"/v1/x": http.StatusForbidden, "/pub/p": http.StatusOK} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d, muốn %d", path, w.Code, want)
		}
	}
}

// Reports checks current owner authority before the HTTP idempotency response
// cache. Its authorizer must receive the session resolved by accounts first.
func TestAccountsSessionPrecedesReportsReplayAuthorization(t *testing.T) {
	accountsAt, reportsAt := -1, -1
	for i, module := range (Modules{}).Named() {
		switch module.Name {
		case "pf/accounts":
			accountsAt = i
		case "pf/reports":
			reportsAt = i
		}
	}
	if accountsAt < 0 || reportsAt < 0 || accountsAt >= reportsAt {
		t.Fatal("accounts must resolve the current session before reports replay authorization")
	}
}
