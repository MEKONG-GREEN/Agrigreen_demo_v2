package menu

import (
	"context"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestWeeklyProgramMenuScope284(t *testing.T) {
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.New()
	for _, audience := range []string{partyport.AudienceTinh, partyport.AudienceXa, partyport.AudienceTW} {
		request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: audience, UnitRef: unit, Department: department(unit, "thong_tin_truyen_thong", "thong_tin_truyen_thong")}, Mode: partyport.MenuDepartment}
		def, err := reg.Build(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		items := leaves(def.Groups)
		for _, key := range []string{"tinh.chuong_trinh.thong_tin", "tinh.chuong_trinh.dao_tao"} {
			item, ok := items[key]
			if audience == partyport.AudienceTinh {
				if !ok || !item.Enabled || item.Module != "pf/price" || !strings.HasPrefix(item.Route, "/kn/chuong-trinh-khuyen-nong/") {
					t.Fatalf("province weekly menu missing: %+v", item)
				}
			} else if ok {
				t.Fatalf("weekly menu leaked to %s", audience)
			}
		}
		filtered, err := sharedView(def, request, map[string]bool{"pf/price": true}, c.NavigationPolicy())
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range filtered.Groups {
			for _, i := range g.Items {
				if strings.HasPrefix(i.Route, "/kn/chuong-trinh-khuyen-nong/") {
					t.Fatal("disabled module still visible")
				}
			}
		}
	}
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: partyport.AudienceTinh, UnitRef: unit, Department: department(unit, "hanh_chinh_tong_hop", "hanh_chinh_tong_hop")}, Mode: partyport.MenuDepartment}
	def, err := reg.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range leaves(def.Groups) {
		if strings.HasPrefix(i.Route, "/kn/chuong-trinh-khuyen-nong/") {
			t.Fatal("weekly menu leaked to another department")
		}
	}
}
