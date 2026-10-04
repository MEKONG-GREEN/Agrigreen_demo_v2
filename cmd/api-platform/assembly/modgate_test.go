package assembly

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// W1.4: tên trong Named() là tên repo (module.yaml) của đúng gói module — cổng route chặn đúng module.
func TestNamedModulesMatchCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range (Modules{}).Named() {
		if _, ok := modreg.Entry(n.Name); !ok {
			t.Fatalf("%s không có trong danh mục modreg", n.Name)
		}
		pkg := reflect.TypeOf(n.Module).Elem().PkgPath()
		if want := "/internal/platform/" + strings.TrimPrefix(n.Name, "pf/"); !strings.HasSuffix(pkg, want) {
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
