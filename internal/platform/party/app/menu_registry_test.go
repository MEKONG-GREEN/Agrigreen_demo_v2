package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

type menuModuleStates struct {
	disabled map[string]bool
	err      error
}

func (m menuModuleStates) DisabledModules(context.Context, uuid.UUID) (map[string]bool, error) {
	return m.disabled, m.err
}
func (m menuModuleStates) ModuleEnabled(context.Context, uuid.UUID, string) (bool, error) {
	return true, m.err
}
func TestRegisteredMenusFilterBeforeLandingAndKeepOwnerProfile(t *testing.T) {
	builder := func(context.Context, port.MenuBuildRequest) (port.MenuDefinition, error) {
		return port.MenuDefinition{Version: "app/v1", DefaultRoute: "/workspace/disabled", Groups: []port.MenuGroup{{Key: "work", Label: "Work", Items: []port.MenuItem{
			{Key: "disabled", Label: "Disabled module", Route: "/workspace/disabled", Module: "pf/reports", Enabled: true},
			{Key: "unavailable", Label: "Unavailable", Route: "/workspace/unavailable", Module: "pf/party", Enabled: false},
			{Key: "parent", Label: "Disabled parent", Enabled: false, Children: []port.MenuItem{{Key: "hidden", Label: "Hidden", Route: "/workspace/hidden", Module: "pf/party", Enabled: true}}},
			{Key: "available", Label: "Available", Route: "/workspace/available", Module: "pf/party", Enabled: true},
		}}}}, nil
	}
	reg, err := port.NewMenus(builder, port.MenuRoute{Pattern: "/workspace/disabled", Module: "pf/reports"}, port.MenuRoute{Pattern: "/workspace/unavailable", Module: "pf/party"}, port.MenuRoute{Pattern: "/workspace/hidden", Module: "pf/party"}, port.MenuRoute{Pattern: "/workspace/available", Module: "pf/party"})
	if err != nil {
		t.Fatal(err)
	}
	k := KN{Menus: reg, Modules: menuModuleStates{disabled: map[string]bool{"pf/reports": true}}}
	request := port.MenuBuildRequest{Mode: port.MenuDepartment, NavigationRequest: port.NavigationRequest{Audience: "example", Department: port.Department{Code: port.DeptKeHoachTaiChinh, MenuProfile: "generic"}}}
	view, err := k.registeredMenu(context.Background(), ids.New(), request)
	if err != nil || view.DefaultRoute != "/workspace/available" || len(view.Groups[0].Items) != 3 {
		t.Fatal("nil-policy registered landing escaped filters", view, err)
	}
	k.NavigationPolicy = func(in port.NavigationRequest) ([]string, error) {
		if in.Department.MenuProfile != "generic" {
			t.Fatal("legacy alias changed configured metadata")
		}
		return []string{"/workspace/disabled", "/workspace/unavailable", "/workspace/hidden"}, nil
	}
	view, err = k.registeredMenu(context.Background(), ids.New(), request)
	if err != nil || view.DefaultRoute != "/workspace/available" {
		t.Fatal("policy escaped disabled/unavailable leaves", err)
	}
	sentinel := errors.New("module source unavailable")
	k.Modules = menuModuleStates{err: sentinel}
	if _, err := k.registeredMenu(context.Background(), ids.New(), request); !errors.Is(err, sentinel) {
		t.Fatal("module error fell back", err)
	}
	k.Modules = nil
	k.NavigationPolicy = func(port.NavigationRequest) ([]string, error) { return nil, problem.Forbidden }
	if _, err := k.registeredMenu(context.Background(), ids.New(), request); !errors.Is(err, problem.Forbidden) {
		t.Fatal("policy denial fell back", err)
	}
}
