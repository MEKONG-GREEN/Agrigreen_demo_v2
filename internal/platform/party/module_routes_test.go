package party

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func routeSet(register func(*gin.RouterGroup)) map[string]bool {
	e := gin.New()
	register(e.Group("/v1"))
	out := map[string]bool{}
	for _, r := range e.Routes() {
		out[r.Method+" "+r.Path] = true
	}
	return out
}

// KNSPLIT-2 #118: bảng đăng ký là hợp đồng — pf/party không còn đăng ký danh mục phòng ban (/v1/departments,
// /v1/departments/:id*) ở Application lẫn Admin API; chủ KN (internal/kn/extension/departments) đăng ký qua
// port.DepartmentAdmin. Cây module của phòng ban, menu, khu vực, cây tổ chức và nạp phòng ban mặc định vẫn ở party.
func TestDepartmentCatalogRoutesNotRegisteredByParty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := New(nil, clock.System{})
	app, admin := routeSet(m.RegisterApp), routeSet(m.RegisterAdmin)
	for _, set := range []map[string]bool{app, admin} {
		for r := range set {
			path := strings.SplitN(r, " ", 2)[1]
			if path == "/v1/departments" || strings.HasPrefix(path, "/v1/departments/:id") {
				t.Errorf("pf/party vẫn đăng ký route danh mục phòng ban của chủ KN: %s", r)
			}
		}
	}
	for _, r := range []string{"GET /v1/departments/:code/modules", "GET /v1/menu", "GET /v1/regions", "GET /v1/orgs/tree"} {
		if !app[r] {
			t.Errorf("Application API mất route còn thuộc pf/party: %s", r)
		}
	}
	for _, r := range []string{"GET /v1/regions", "GET /v1/orgs/tree", "POST /v1/orgs/:ref/departments/seed", "GET /v1/orgs/:ref/impact"} {
		if !admin[r] {
			t.Errorf("Admin API mất route còn thuộc pf/party: %s", r)
		}
	}
	if m.DepartmentAdmin() == nil {
		t.Fatal("pf/party phải cung cấp port.DepartmentAdmin cho chủ KN")
	}
}
