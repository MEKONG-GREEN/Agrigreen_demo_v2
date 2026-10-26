package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/platformread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NewPublishedAdminUnits exposes geographic publications through read-only orgtree capabilities.
func NewPublishedAdminUnits(pool tenant.Beginner) port.AdministrativeUnits {
	return platformread.NewUnits(pool)
}

// NewPublishedOrganizations exposes published organization metadata, omitting representatives.
func NewPublishedOrganizations(pool tenant.Beginner) port.OrganizationData {
	return platformread.NewOrganizations(pool)
}
