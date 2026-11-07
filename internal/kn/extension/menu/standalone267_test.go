package menu

import (
	"context"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
	"testing"
)

func TestStandalone267RemovesOnlyRetiredKHTCLeaves(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceTinh, UnitRef: unit, Department: department(unit, "ke_hoach_tai_chinh", partyport.MenuProfileKeHoachTaiChinh)}, Mode: partyport.MenuDepartment}
	def, err := reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items := leaves(def.Groups)
	for _, key := range []string{"tinh.khtc.so_lieu_ngoai", "tinh.khtc.mau_bao_cao"} {
		if _, ok := items[key]; ok {
			t.Errorf("retired key %s still registered", key)
		}
	}
	for _, r := range c.Routes() {
		if r.Pattern == "/kn/khtc/so-lieu-ngoai" || r.Pattern == "/kn/khtc/mau-bao-cao" {
			t.Errorf("retired route %s still registered", r.Pattern)
		}
	}
	request.Audience = partyport.AudienceAdmin
	request.Mode = partyport.MenuAdmin
	request.Departments = []partyport.Department{request.Department}
	def, err = reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	items = leaves(def.Groups)
	templates, outline := false, false
	for _, item := range items {
		templates = templates || item.Route == "/kn/phong-ban/ke_hoach_tai_chinh/mau-bao-cao"
		outline = outline || item.Route == "/kn/phong-ban/ke_hoach_tai_chinh/muc-luc-mau"
	}
	if !templates || !outline {
		t.Fatalf("Admin templates/outline lost: templates=%v outline=%v", templates, outline)
	}
}
