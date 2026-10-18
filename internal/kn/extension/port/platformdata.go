package port

import pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"

// AdministrativeUnits composes the lower-layer orgtree read capabilities.
type AdministrativeUnits interface {
	pp.AdminUnits
	pp.AdminUnitTree
}

// OrganizationData offers common read DTOs without writes; RepresentativeRef is always zero.
type OrganizationData interface {
	pp.Orgs
	pp.OrgCodes
	pp.OrgDirectory
}
