package kn77_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

type fixtureDepartments struct {
	partyport.DepartmentAdmin
	build          partyport.MenuBuilder
	profiles       map[string]string
	reportCycle    extension.ReportCycleDeps
	jobDepartments []partyport.ProvinceDepartment
}

func newFixtureDepartments(t *testing.T) *fixtureDepartments {
	t.Helper()
	catalog, err := extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureDepartments{build: catalog.Builder(), profiles: map[string]string{}, reportCycle: extension.ReportCycleDeps{MenuCatalog: catalog}}
	for _, province := range []string{"89", "92"} {
		for _, code := range []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
			f.jobDepartments = append(f.jobDepartments, partyport.ProvinceDepartment{ProvinceCode: province, Department: partyport.Department{Code: code, MenuProfile: code, Status: partyport.DeptActive}})
		}
	}
	f.reportCycle.Departments = f
	return f
}

func (f fixtureDepartments) Menu(ctx context.Context, _ partyport.MenuQuery) (partyport.MenuView, error) {
	_, k, err := extport.RequireSession(ctx)
	if err != nil {
		return partyport.MenuView{}, err
	}
	request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: k.Role, UnitRef: k.OrgRef, FilterTraits: true, Manager: k.Manager, Duties: k.Duties}, Mode: partyport.MenuDepartment}
	for _, code := range []string{"trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		d := partyport.Department{OrgRef: k.OrgRef, Code: code, Name: code, Status: partyport.DeptActive, MenuProfile: code}
		request.Departments = append(request.Departments, d)
		if code == k.Department {
			request.Department = d
		}
	}
	if profile, ok := f.profiles[k.Department]; ok {
		request.Department = partyport.Department{OrgRef: k.OrgRef, Code: k.Department, Name: k.Department, Status: partyport.DeptActive, MenuProfile: profile}
	}
	if k.Role == extport.SessionRoleAdmin {
		request.Mode = partyport.MenuAdmin
	} else if request.Department.Code == "" {
		request.Department = partyport.Department{OrgRef: k.OrgRef, Code: k.Department, Name: k.Department, Status: partyport.DeptActive, MenuProfile: partyport.MenuProfileGeneric}
	}
	def, err := f.build(ctx, request)
	if err != nil {
		return partyport.MenuView{}, err
	}
	var items func([]partyport.MenuItem) []partyport.MenuItemView
	items = func(input []partyport.MenuItem) []partyport.MenuItemView {
		out := []partyport.MenuItemView{}
		for _, item := range input {
			out = append(out, partyport.MenuItemView{Key: item.Key, Label: item.Label, Route: item.Route, Enabled: item.Enabled, Children: items(item.Children)})
		}
		return out
	}
	view := partyport.MenuView{Version: def.Version, Audience: k.Role, UnitRef: k.OrgRef, DefaultRoute: def.DefaultRoute}
	for _, group := range def.Groups {
		view.Groups = append(view.Groups, partyport.MenuGroupView{Key: group.Key, Label: group.Label, Items: items(group.Items)})
	}
	return view, nil
}

func (f *fixtureDepartments) ProvinceDepartments(context.Context) ([]partyport.ProvinceDepartment, error) {
	return f.jobDepartments, nil
}
