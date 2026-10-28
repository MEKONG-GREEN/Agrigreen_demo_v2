package app

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"

// RoleCatalog is shared by the public catalog and every realm-account use case.
// Only absence of configuration enables the compatibility snapshot. An explicitly
// configured invalid catalog is never replaced by the legacy bridge.
func (s *Service) RoleCatalog() *port.RoleCatalog {
	if s.Roles != nil {
		return s.Roles
	}
	return port.LegacyRoleCatalog()
}
