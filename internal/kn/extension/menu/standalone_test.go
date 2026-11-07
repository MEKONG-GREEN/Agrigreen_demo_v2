package menu

import (
	"context"
	"strings"
	"testing"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
)

// S-4: the reporting and agricultural menus belong to TT; department modules
// and shared HRM/contact entries retain their existing owner permissions.
func TestStandaloneDepartmentMenus(t *testing.T) {
	c := catalog(t)
	c.UseFormAvailability(forms(true))
	unit := uuid.New()
	for _, profile := range c.t.MenuProfiles {
		for _, audience := range []string{partyport.AudienceTW, partyport.AudienceTinh, partyport.AudienceXa} {
			t.Run(profile+"/"+audience, func(t *testing.T) {
				def, err := c.Build(context.Background(), partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{
					Audience: audience, UnitRef: unit, Department: department(unit, profile, profile), FilterTraits: true, Manager: true}, Mode: partyport.MenuDepartment})
				if err != nil {
					t.Fatal(err)
				}
				got := leaves(def.Groups)
				for key, leaf := range got {
					if leaf.Route == "/kn/bao-cao/dot-xuat" {
						t.Errorf("removed ad hoc menu %s", key)
					}
					if leaf.Route == "/kn/van-ban" && profile != partyport.MenuProfileChanNuoiThuySan {
						t.Errorf("FN20 outside CNTS: %s", key)
					}
					if profile != partyport.MenuProfileTrongTrot && (strings.HasPrefix(key, "tw.bao_cao.") || strings.HasPrefix(key, "xa.bao_cao.") || strings.HasPrefix(key, "tinh.bao_cao.tong_hop") || strings.HasPrefix(key, "tinh.nhiem_vu.") || key == "xa.nhiem_vu.cong_viec" || key == "tinh.dashboard" || key == "tw.dashboard" || (key == "tinh.trang_chu" && profile != partyport.MenuProfileChanNuoiThuySan)) {
						t.Errorf("TT menu copied to %s: %s", profile, key)
					}
				}
				if _, ok := got["chung.contact"]; !ok {
					t.Error("shared Contact removed")
				}
				if profile == partyport.MenuProfileTrongTrot && audience == partyport.AudienceXa {
					for _, key := range []string{"xa.nhiem_vu.cong_viec", "xa.bao_cao.tuan.t01", "xa.bao_cao.tuan.t07", "xa.bao_cao.hang_vu.v01", "xa.bao_cao.hang_vu.v06"} {
						if _, ok := got[key]; !ok {
							t.Errorf("TT FN-XA-03.02 removed: %s", key)
						}
					}
				}
			})
		}
		d := department(unit, profile, profile)
		def, err := c.Build(context.Background(), partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{
			Audience: partyport.AudienceAdmin, UnitRef: unit, Department: d}, Mode: partyport.MenuDepartmentModules, Departments: []partyport.Department{d}})
		if err != nil {
			t.Fatal(err)
		}
		for key, leaf := range leaves(def.Groups) {
			if profile != partyport.MenuProfileTrongTrot && (key == "admin.pb.theo_doi_nop" || key == "admin.pb.tong_hop" || key == "admin.pb.du_lieu_bao_cao" || key == "admin.pb.danh_muc") {
				t.Errorf("TT Admin menu copied to %s: %s", profile, key)
			}
			if strings.HasSuffix(leaf.Route, "/van-ban") && profile != partyport.MenuProfileChanNuoiThuySan {
				t.Errorf("FN20 Admin outside CNTS: %s", profile)
			}
		}
	}
}

func TestStandaloneWorkerProfileEligibility(t *testing.T) {
	c := catalog(t)
	for _, test := range []struct {
		name, code, profile, status string
		retired, want               bool
	}{
		{name: "TT", code: "trong_trot", profile: "trong_trot", status: partyport.DeptActive, want: true},
		{name: "generic without TT profile", code: "trong_trot", profile: "generic", status: partyport.DeptActive},
		{name: "custom TT", code: "pb_trong_trot", profile: "trong_trot", status: partyport.DeptActive, want: true},
		{name: "foreign", code: "chan_nuoi_thuy_san", profile: "chan_nuoi_thuy_san", status: partyport.DeptActive},
		{name: "unknown", code: "pb_trong_trot", profile: "generic", status: partyport.DeptActive},
		{name: "retired", code: "trong_trot", profile: "trong_trot", status: partyport.DeptActive, retired: true},
		{name: "inactive", code: "trong_trot", profile: "trong_trot", status: partyport.DeptSuspended},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := c.SupportsTrongTrot(partyport.Department{Code: test.code, MenuProfile: test.profile, Status: test.status, Retired: test.retired}); got != test.want {
				t.Fatalf("%s: %v want %v", test.name, got, test.want)
			}
		})
	}
}
