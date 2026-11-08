package forms

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// UsePendingPolicy registers the current authority owner before serving. Nil
// disables the supplier. Existing read/write ports and consumers are unchanged.
func (m *Module) UsePendingPolicy(profile string, policy port.PendingPolicy) error {
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
	if m.svc.PendingPolicies == nil {
		m.svc.PendingPolicies = map[string]port.PendingPolicy{}
	}
	m.svc.PendingPolicies[profile] = policy
	return nil
}

// PendingSubmissions is an additive native supplier. The owner must explicitly
// wire a current-authority policy; no application/REST consumer is enabled here.
func (m *Module) PendingSubmissions() port.PendingReader { return app.Control{Svc: m.svc} }
