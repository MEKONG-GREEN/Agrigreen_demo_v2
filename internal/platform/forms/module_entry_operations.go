package forms

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// UseReviewOperationPolicy installs the trusted owner before serving. No
// default policy or legacy-write fallback is available for immutable receipts.
func (m *Module) UseReviewOperationPolicy(profile string, policy port.ReviewOperationPolicy) error {
	known := false
	for _, p := range m.svc.Profiles() {
		if p.Name == profile {
			known = true
			break
		}
	}
	if !known {
		return port.ErrProfileNotFound
	}
	if m.svc.ReviewOperationPolicies == nil {
		m.svc.ReviewOperationPolicies = map[string]port.ReviewOperationPolicy{}
	}
	m.svc.ReviewOperationPolicies[profile] = policy
	return nil
}

// EntryOperations is an additive supplier; callers install a current owner
// policy explicitly. The existing Entry interface and consumers are unchanged.
func (m *Module) EntryOperations() port.EntryOperations { return app.Entry{Svc: m.svc} }
