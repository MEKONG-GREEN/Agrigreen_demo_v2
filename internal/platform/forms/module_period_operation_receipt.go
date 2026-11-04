package forms

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

// UsePeriodOperationPolicy registers a trusted owner before serving. There is no
// default authorization fallback; nil leaves the receipt capability disabled.
func (m *Module) UsePeriodOperationPolicy(profile string, policy port.PeriodOperationPolicy) error {
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
	if m.svc.PeriodOperationPolicies == nil {
		m.svc.PeriodOperationPolicies = map[string]port.PeriodOperationPolicy{}
	}
	m.svc.PeriodOperationPolicies[profile] = policy
	return nil
}

// PeriodOperations is the additive supplier. Native owners must explicitly wire
// its current-authority policy before use; existing consumers are not rolled out.
func (m *Module) PeriodOperations() port.PeriodOperations { return app.ReceiptControl{Svc: m.svc} }
