package app

import "testing"

func TestFormSeedsRequireRegistration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		registry *FormSeedRegistry
	}{{name: "missing"}, {name: "empty", registry: NewFormSeedRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{FormSeeds: tc.registry}
			set, err := svc.formSeedSet()
			if err == nil {
				t.Fatalf("missing registration inferred %d application seed templates", len(set.Templates))
			}
		})
	}
}
