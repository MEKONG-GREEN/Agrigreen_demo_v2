package port

import "testing"

func TestRegisteredNavigationIsImmutableAndProfileBased(t *testing.T) {
	defaults := map[string]string{"reader": "/overview"}
	profiles := map[string]map[string]string{"special": {"reader": "/periods"}}
	policy, err := RegisteredNavigation(defaults, profiles)
	if err != nil {
		t.Fatal(err)
	}
	defaults["reader"] = "/changed"
	profiles["special"]["reader"] = "/changed"
	for _, tc := range []struct{ code, profile, want string }{{"custom-code", "special", "/periods"}, {"special", "unknown", "/overview"}} {
		got, err := policy(NavigationRequest{Audience: "reader", Department: Department{Code: tc.code, MenuProfile: tc.profile}})
		if err != nil || len(got) == 0 || got[0] != tc.want {
			t.Fatalf("%s/%s:%v %v", tc.code, tc.profile, got, err)
		}
		got[0] = "/mutated"
		again, _ := policy(NavigationRequest{Audience: "reader", Department: Department{MenuProfile: tc.profile}})
		if again[0] != tc.want {
			t.Fatal("returned routes alias registration")
		}
	}
	if _, err := policy(NavigationRequest{Audience: "unknown"}); err == nil {
		t.Fatal("unknown audience accepted")
	}
}

func TestRegisteredNavigationRejectsExternalAndUnknownRoutes(t *testing.T) {
	for _, route := range []string{"", "https://example.test/", "//example.test/", "/\\example.test", "/route?next=else", "/route#fragment", "/route\nother"} {
		if _, err := RegisteredNavigation(map[string]string{"reader": route}, nil); err == nil {
			t.Errorf("unsafe registration accepted %q", route)
		}
	}
	if _, err := RegisteredNavigation(map[string]string{"reader": "/ok"}, map[string]map[string]string{"profile": {"unknown": "/page"}}); err == nil {
		t.Fatal("unregistered audience accepted")
	}
}
