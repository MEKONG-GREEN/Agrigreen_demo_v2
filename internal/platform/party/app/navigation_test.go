package app

import (
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestNavigationSelectsOnlyEnabledLeavesAndPreservesNative(t *testing.T) {
	view := MenuView{DefaultRoute: "/native", Groups: []MenuGroupView{{Items: []MenuItemView{
		{Route: "/parent", Enabled: true, Children: []MenuItemView{{Route: "/closed", Enabled: false}, {Route: "/first", Enabled: true}}},
		{Route: "/preferred", Enabled: true},
	}}}}
	native, err := ApplyNavigation(view, nil)
	if err != nil || native.DefaultRoute != "/native" {
		t.Fatal("unregistered native changed")
	}
	policy := func(port.NavigationRequest) ([]string, error) {
		return []string{"/closed", "/parent", "/preferred"}, nil
	}
	selected, err := ApplyNavigation(view, policy)
	if err != nil || selected.DefaultRoute != "/preferred" {
		t.Fatalf("selected:%+v %v", selected, err)
	}
	selected, err = ApplyNavigation(view, func(port.NavigationRequest) ([]string, error) { return []string{"/missing"}, nil })
	if err != nil || selected.DefaultRoute != "/first" {
		t.Fatalf("fallback:%+v %v", selected, err)
	}
	selected, err = ApplyNavigation(MenuView{Groups: []MenuGroupView{{Items: []MenuItemView{{Route: "/closed-parent", Enabled: false, Children: []MenuItemView{{Route: "/child", Enabled: true}}}}}}}, policy)
	if !errors.Is(err, problem.Forbidden) || selected.DefaultRoute != "" {
		t.Fatalf("disabled branch escaped:%+v %v", selected, err)
	}
	sentinel := errors.New("owner policy denied")
	if _, err = ApplyNavigation(view, func(port.NavigationRequest) ([]string, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("policy error fell back")
	}
}
