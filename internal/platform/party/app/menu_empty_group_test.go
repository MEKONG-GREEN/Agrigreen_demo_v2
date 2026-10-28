package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"testing"
)

func TestRegisteredMenuPreservesOwnerEmptyGroupAndPrunesFilteredGroup(t *testing.T) {
	builder := func(context.Context, port.MenuBuildRequest) (port.MenuDefinition, error) {
		return port.MenuDefinition{Version: "fixture/v1", DefaultRoute: "/workspace/on", Groups: []port.MenuGroup{
			{Key: "intentional-empty", Label: "Empty category", Items: []port.MenuItem{}},
			{Key: "filtered-empty", Label: "Disabled module", Items: []port.MenuItem{{Key: "off", Label: "Off", Route: "/workspace/off", Module: "pf/reports", Enabled: true}}},
			{Key: "available", Label: "Available", Items: []port.MenuItem{{Key: "on", Label: "On", Route: "/workspace/on", Module: "pf/party", Enabled: true}}},
		}}, nil
	}
	reg, err := port.NewMenus(builder, port.MenuRoute{Pattern: "/workspace/off", Module: "pf/reports"}, port.MenuRoute{Pattern: "/workspace/on", Module: "pf/party"})
	if err != nil {
		t.Fatal(err)
	}
	k := KN{Menus: reg, Modules: menuModuleStates{disabled: map[string]bool{"pf/reports": true}}}
	view, err := k.registeredMenu(context.Background(), ids.New(), port.MenuBuildRequest{Mode: port.MenuAdmin, NavigationRequest: port.NavigationRequest{Audience: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Groups) != 2 || view.Groups[0].Key != "intentional-empty" || len(view.Groups[0].Items) != 0 || view.Groups[1].Key != "available" || view.DefaultRoute != "/workspace/on" {
		t.Fatalf("want explicit owner empty group retained and module-filtered group pruned: %+v", view)
	}
}
