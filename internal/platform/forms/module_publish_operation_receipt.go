package forms

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// UsePublishOperationPolicy registers the trusted owner before serving. Missing
// registration disables only the new receipt capability, never authorizes fallback.
func (m *Module) UsePublishOperationPolicy(profile string, policy port.PublishOperationPolicy) error {
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
	if m.svc.PublishOperationPolicies == nil {
		m.svc.PublishOperationPolicies = map[string]port.PublishOperationPolicy{}
	}
	m.svc.PublishOperationPolicies[profile] = policy
	return nil
}

// PublishOperations is additive and dormant until an owner policy is registered.
func (m *Module) PublishOperations() port.PublishOperations {
	return app.PublicationReceipts{Svc: m.svc}
}
