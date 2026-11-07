package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/menu"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"os"
	"path/filepath"
)

// A copied menu must not remain callable through a direct API URL. An invalid
// ID distinguishes the TT/admin route (404) from a forbidden department (403),
// and also proves rejection does not depend on a database lookup or body parse.
func TestStandaloneTTDirectRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catalog := departmentCatalog(t)
	for _, dept := range []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		for _, role := range []string{port.SessionRoleNational, port.SessionRoleProvince, port.SessionRoleCommune, port.SessionRoleAdmin} {
			t.Run(role+"/"+dept, func(t *testing.T) {
				session, err := port.NewSessionContext(port.SessionContext{ID: uuid.New(), Role: role, Department: dept, OrgRef: uuid.New(), Communes: []string{"91301"}, Scope: authz.Scope{Units: []string{"91301"}}})
				if err != nil {
					t.Fatal(err)
				}
				principal := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), SelectedSession: &session}
				engine := gin.New()
				group := engine.Group("/v1", func(c *gin.Context) {
					c.Request = c.Request.WithContext(authz.WithPrincipal(context.Background(), principal))
				})
				Handler{Menu: func(ctx context.Context) (partyport.MenuView, error) {
					d := partyport.Department{OrgRef: uuid.New(), Code: dept, Name: dept, MenuProfile: dept, Status: partyport.DeptActive}
					request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: role, UnitRef: d.OrgRef, Department: d}, Mode: partyport.MenuDepartment}
					if role == port.SessionRoleAdmin {
						request.Mode = partyport.MenuAdmin
						request.Department = partyport.Department{}
						request.Departments = []partyport.Department{{OrgRef: d.OrgRef, Code: "trong_trot", Name: "TT", MenuProfile: "trong_trot", Status: partyport.DeptActive}}
					}
					def, err := catalog.Build(ctx, request)
					if err != nil {
						return partyport.MenuView{}, err
					}
					return catalogView(def), nil
				}}.RegisterApp(group)
				for _, path := range []string{"/v1/extension/report-batches/invalid", "/v1/extension/report-tasks/invalid", "/v1/extension/report-periods/invalid"} {
					recorder := httptest.NewRecorder()
					engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
					want := http.StatusForbidden
					if dept == "trong_trot" || role == port.SessionRoleAdmin {
						want = http.StatusNotFound
					}
					if recorder.Code != want {
						t.Fatalf("%s: %d want%d: %s", path, recorder.Code, want, recorder.Body.String())
					}
				}
			})
		}
	}
}

func departmentCatalog(t *testing.T) *menu.Catalog {
	t.Helper()
	var sources [][]byte
	for _, module := range []string{"extension", "hrm", "price", "contact"} {
		files, err := filepath.Glob(filepath.Join("../../../../..", "internal/kn", module, "menus/*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("menu sources %s: %v", module, err)
		}
		for _, file := range files {
			raw, err := os.ReadFile(file) //nolint:gosec // fixed repository-owned menu sources
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, raw)
		}
	}
	catalog, err := menu.New(sources...)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func catalogView(def partyport.MenuDefinition) partyport.MenuView {
	var items func([]partyport.MenuItem) []partyport.MenuItemView
	items = func(input []partyport.MenuItem) []partyport.MenuItemView {
		output := []partyport.MenuItemView{}
		for _, item := range input {
			output = append(output, partyport.MenuItemView{Key: item.Key, Enabled: item.Enabled, Route: item.Route, Children: items(item.Children)})
		}
		return output
	}
	view := partyport.MenuView{}
	for _, group := range def.Groups {
		view.Groups = append(view.Groups, partyport.MenuGroupView{Key: group.Key, Items: items(group.Items)})
	}
	return view
}
