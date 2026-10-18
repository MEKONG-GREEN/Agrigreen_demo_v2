package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"slices"
	"testing"
)

func TestPolicyRegistryIsolationAndComposition(t *testing.T) {
	roles := []string{"reader"}
	sources := map[string][]string{"example": {"reader"}}
	r := NewPolicyRegistry(port.Policy{ReaderRoles: roles, SourceRoles: sources, AttachmentLimitParam: "example.limit"}, port.Policy{ReaderRoles: []string{"second", "reader"}, SourceRoles: map[string][]string{"other": {"second"}}, AttachmentLimitParam: "example.limit"})
	roles[0] = "mutated"
	sources["example"][0] = "mutated"
	sources["extra"] = []string{"mutated"}
	got := r.ReaderRoles()
	got[0] = "escaped"
	src := r.SourceRoles("example")
	src[0] = "escaped"
	if !slices.Equal(r.ReaderRoles(), []string{"reader", "second"}) || !slices.Equal(r.SourceRoles("example"), []string{"reader"}) || !slices.Equal(r.SourceRoles("other"), []string{"second"}) || len(r.SourceRoles("extra")) != 0 || r.AttachmentLimitParam() != "example.limit" {
		t.Fatal("registration changed through input/output aliases or leaked across modules")
	}
	var empty *PolicyRegistry
	if len(empty.ReaderRoles()) != 0 || len(empty.SourceRoles("example")) != 0 || empty.AttachmentLimitParam() != "" {
		t.Fatal("nil registry granted policy")
	}
}

func TestPolicyRegistryConflictingParameterRejected(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("conflicting application configuration accepted")
		}
	}()
	NewPolicyRegistry(port.Policy{AttachmentLimitParam: "one"}, port.Policy{AttachmentLimitParam: "two"})
}
