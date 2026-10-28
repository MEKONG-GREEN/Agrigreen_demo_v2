package port

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func menuExample() MenuDefinition {
	return MenuDefinition{Version: "example/v1", DefaultRoute: "/workspace/reports", Groups: []MenuGroup{{Key: "work", Label: "Work", Items: []MenuItem{
		{Key: "reports", Label: "Reports", Route: "/workspace/reports", Module: "pf/reports", Enabled: true},
	}}}}
}
func TestMenusCopiesInputsOutputsAndInstances(t *testing.T) {
	routes := []MenuRoute{{Pattern: "/workspace/reports", Module: "pf/reports"}}
	original := menuExample()
	first, err := NewMenus(func(_ context.Context, in MenuBuildRequest) (MenuDefinition, error) {
		in.Duties[0] = "changed"
		in.Departments[0].Code = "changed"
		return original, nil
	}, routes...)
	if err != nil {
		t.Fatal(err)
	}
	routes[0].Module = "unknown"
	request := MenuBuildRequest{Mode: MenuDepartment, NavigationRequest: NavigationRequest{Duties: []string{"allowed"}}, Departments: []Department{{Code: "original"}}}
	a, err := first.Build(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	a.Groups[0].Items[0].Label = "mutated"
	again, err := first.Build(context.Background(), request)
	if err != nil || again.Groups[0].Items[0].Label != "Reports" || request.Duties[0] != "allowed" || request.Departments[0].Code != "original" {
		t.Fatal("aliased input/output", err)
	}
	second, err := NewMenus(func(context.Context, MenuBuildRequest) (MenuDefinition, error) {
		d := menuExample()
		d.Version = "second"
		return d, nil
	}, MenuRoute{Pattern: "/workspace/reports", Module: "pf/reports"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Build(context.Background(), request)
	if err != nil || b.Version != "second" || again.Version != "example/v1" {
		t.Fatal("instance registration leaked", err)
	}
	sentinel := errors.New("owner denied")
	denied, _ := NewMenus(func(context.Context, MenuBuildRequest) (MenuDefinition, error) { return MenuDefinition{}, sentinel }, MenuRoute{Pattern: "/workspace/reports", Module: "pf/reports"})
	if _, err := denied.Build(context.Background(), request); !errors.Is(err, sentinel) {
		t.Fatal("owner error hidden", err)
	}
}

func TestMenusPreserveRepeatedRoutePresentationAliases(t *testing.T) {
	leaf := MenuItem{Key: "reports.main", Label: "Reports", Route: "/workspace/reports", Module: "pf/reports", Enabled: true, Children: []MenuItem{}}
	shortcut := leaf
	shortcut.Key, shortcut.Label, shortcut.Icon = "reports.shortcut", "Quick reports", "shortcut"
	shared := leaf
	shared.Key, shared.Label, shared.Enabled = "reports.shared", "Shared reports", false
	want := MenuDefinition{Version: "aliases/v1", DefaultRoute: leaf.Route, Groups: []MenuGroup{
		{Key: "work", Label: "Work", Items: []MenuItem{leaf, {Key: "shortcuts", Label: "Shortcuts", Enabled: true, Children: []MenuItem{shortcut}}}},
		{Key: "shared", Label: "Shared", Items: []MenuItem{shared}},
	}}
	reg, err := NewMenus(func(context.Context, MenuBuildRequest) (MenuDefinition, error) { return want, nil }, MenuRoute{Pattern: leaf.Route, Module: leaf.Module})
	if err != nil {
		t.Fatal(err)
	}
	request := MenuBuildRequest{Mode: MenuDepartment}
	got, err := reg.Build(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("registered aliases were rejected, reordered, or deduplicated", got, err)
	}
	got.Groups[0].Items[1].Children[0].Label = "mutated"
	got.Groups[1].Items[0].Route = "/mutated"
	again, err := reg.Build(context.Background(), request)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal("alias output mutated the owner's presentation", again, err)
	}
}

func TestMenusRejectAmbiguousRegistrationAndMalformedDefinitions(t *testing.T) {
	builder := func(context.Context, MenuBuildRequest) (MenuDefinition, error) { return menuExample(), nil }
	for i, routes := range [][]MenuRoute{
		nil, {{Pattern: "//outside", Module: "pf/party"}}, {{Pattern: "/path", Module: "unknown"}},
		{{Pattern: "/path", Module: "pf/party"}, {Pattern: "/path", Module: "pf/reports"}},
		{{Pattern: "/path/{x}", Module: "pf/party"}, {Pattern: "/path/known", Module: "pf/reports"}},
		{{Pattern: "/path/{x}", Module: "pf/party"}, {Pattern: "/path/{y}", Module: "pf/party"}},
		{{Pattern: "/path/{bad-name}", Module: "pf/party"}}, {{Pattern: "/../path", Module: "pf/party"}},
	} {
		if _, err := NewMenus(builder, routes...); !errors.Is(err, ErrMenuRegistration) {
			t.Fatalf("registration %d accepted: %v", i, err)
		}
	}
	if _, err := NewMenus(nil, MenuRoute{Pattern: "/path", Module: "pf/party"}); !errors.Is(err, ErrMenuRegistration) {
		t.Fatal("nil callback accepted")
	}
	cases := []func(*MenuDefinition){
		func(d *MenuDefinition) { d.DefaultRoute = "/not-registered" },
		func(d *MenuDefinition) { d.Groups[0].Items[0].Module = "pf/party" },
		func(d *MenuDefinition) { d.Groups[0].Items[0].Route = "/workspace/reports?tenant=else" },
		func(d *MenuDefinition) {
			d.Groups[0].Items = append(d.Groups[0].Items, d.Groups[0].Items[0])
		},
		func(d *MenuDefinition) {
			alias := d.Groups[0].Items[0]
			alias.Key, alias.Module = "another", "pf/party"
			d.Groups[0].Items = append(d.Groups[0].Items, alias)
		},
		func(d *MenuDefinition) {
			d.Groups = append(d.Groups, d.Groups[0])
		},
		func(d *MenuDefinition) {
			d.Groups[0].Items[0].Children = []MenuItem{{Key: "child", Label: "Child", Route: "/workspace/reports", Module: "pf/reports"}}
		},
		func(d *MenuDefinition) { d.Version = "" },
	}
	for i, change := range cases {
		def := menuExample()
		change(&def)
		reg, _ := NewMenus(func(context.Context, MenuBuildRequest) (MenuDefinition, error) { return def, nil }, MenuRoute{Pattern: "/workspace/reports", Module: "pf/reports"})
		if _, err := reg.Build(context.Background(), MenuBuildRequest{Mode: MenuDepartment}); !errors.Is(err, ErrMenuDefinition) {
			t.Fatalf("definition %d accepted: %v", i, err)
		}
	}
	parameterized, _ := NewMenus(func(context.Context, MenuBuildRequest) (MenuDefinition, error) {
		d := menuExample()
		d.DefaultRoute = "/workspace/special/reports"
		d.Groups[0].Items[0].Route = d.DefaultRoute
		return d, nil
	}, MenuRoute{Pattern: "/workspace/{department}/reports", Module: "pf/reports"})
	if _, err := parameterized.Build(context.Background(), MenuBuildRequest{Mode: MenuDepartment}); err != nil {
		t.Fatal("valid bound path rejected", err)
	}
}
