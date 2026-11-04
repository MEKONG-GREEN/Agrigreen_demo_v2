package port

import "context"

// KHTCPlanYearReferences counts presence, not year lifecycle. Historical/expired
// references remain references; unknown legacy owner Year must return an error.
type KHTCPlanYearReferences struct {
	HasPeriodMetadata bool
	HasOpenIntent     bool
}
type KHTCPlanYearReferencesReader interface {
	PlanYearReferences(context.Context, int) (KHTCPlanYearReferences, error)
}
