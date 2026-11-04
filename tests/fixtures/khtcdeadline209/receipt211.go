package khtcdeadline209

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"

// Public supplier-only composition for211; existing209 behavior is unchanged.
func (f *Native) PeriodOperations() port.PeriodOperations { return f.module.PeriodOperations() }
func (f *Native) UsePeriodOperationPolicy(profile string, policy port.PeriodOperationPolicy) error {
	return f.module.UsePeriodOperationPolicy(profile, policy)
}
