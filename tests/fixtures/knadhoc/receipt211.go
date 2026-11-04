package knadhoc

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"

// Public supplier capabilities, used by owned Close protocol fixtures only.
func (f *Fixture) ConditionalControl() port.ConditionalPeriodControl { return f.conditional }
func (f *Fixture) PeriodOperations() port.PeriodOperations           { return f.operations }
func (f *Fixture) UsePeriodOperationPolicy(profile string, policy port.PeriodOperationPolicy) error {
	return f.policy(profile, policy)
}
