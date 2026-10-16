package app

import "testing"

func TestDispatchProfileRequiresRegistration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		registry *PolicyRegistry
	}{{name: "missing"}, {name: "empty", registry: NewPolicyRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{Policies: tc.registry}
			profile, err := svc.dispatchProfile()
			if err == nil {
				t.Fatalf("missing registration inferred application dispatch profile %q", profile.TemplatePrefix)
			}
		})
	}
}
