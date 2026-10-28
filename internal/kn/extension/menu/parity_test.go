package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Bảng KN biên dịch trong Go == bảng bộ sinh đã nhúng vào cây compatibility (registeredMenuJSON) — cùng nguồn,
// cùng ngữ nghĩa gộp/thay thế, không bản sao JSON thứ hai.
func TestCompiledTablesMatchGeneratedCompatibilityCatalog(t *testing.T) {
	raw, err := os.ReadFile(generatedTables)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^const registeredMenuJSON = (".*")$`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("registeredMenuJSON not found")
	}
	text, err := strconv.Unquote(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := decodeAny([]byte(text), &want); err != nil {
		t.Fatal(err)
	}
	got, err := Compile(sources(t)...)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Fatalf("compiled tables differ from generated catalog\n got %d bytes\nwant %d bytes", len(gj), len(wj))
	}
}

type goldenCase struct {
	Name       string `json:"name"`
	Audience   string `json:"audience"`
	Department string `json:"department"`
	AdHoc      bool   `json:"ad_hoc"`
	Menu       view   `json:"menu"`
}

type goldenDept struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

// Ca vàng web (kn-menu.json, do TestWebMenuFixture của pf/party sinh từ cây compatibility): Menus.Build của KN +
// bước shared cho đúng từng nhóm/mục/khóa/nhãn/icon/route/badge/trạng thái/trang đích/phiên bản.
func TestBuilderMatchesWebGoldenFixture(t *testing.T) {
	raw, err := os.ReadFile(goldenFixture)
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Version     string            `json:"menu_version"`
		Departments []goldenDept      `json:"departments"`
		Defaults    map[string]string `json:"default_routes"`
		Cases       []goldenCase      `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	c := catalog(t)
	if golden.Version != Version || !reflect.DeepEqual(golden.Defaults, c.t.DefaultRoutes) {
		t.Fatalf("version/defaults: %s %v", golden.Version, golden.Defaults)
	}
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	depts := make([]partyport.Department, 0, len(golden.Departments))
	for _, d := range golden.Departments {
		depts = append(depts, partyport.Department{OrgRef: unit, Code: d.Code, Name: d.Name, Status: d.Status, MenuProfile: d.MenuProfile})
	}
	if len(golden.Cases) != 18 {
		t.Fatalf("golden cases = %d", len(golden.Cases))
	}
	for _, gc := range golden.Cases {
		t.Run(gc.Name, func(t *testing.T) {
			c.UseFormAvailability(forms(gc.AdHoc))
			request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: gc.Audience, UnitRef: unit}, Mode: partyport.MenuAdmin, Departments: depts}
			if gc.Audience != partyport.AudienceAdmin {
				request.Mode, request.Departments = partyport.MenuDepartment, nil
				for _, d := range depts {
					if d.Code == gc.Department {
						d.Status = partyport.DeptActive // menu phòng ban chỉ dựng cho phòng chọn được (bảng không phụ thuộc trạng thái)
						request.Department = d
					}
				}
			}
			def, err := reg.Build(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			got, err := sharedView(def, request, nil, c.NavigationPolicy())
			if err != nil {
				t.Fatal(err)
			}
			want := gc.Menu
			want.Version = golden.Version
			gj, _ := json.Marshal(got)
			wj, _ := json.Marshal(want)
			if !bytes.Equal(gj, wj) {
				t.Fatalf("menu differs\n got %s\nwant %s", gj, wj)
			}
		})
	}
	c.UseFormAvailability(nil)
}
