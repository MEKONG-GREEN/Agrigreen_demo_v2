package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// AccountRoleCatalog composes native policy and the actual owner declarations.
// The bridge supplies ordering only during rollout; it cannot replace policy,
// add a grant or remove a TOTP requirement from the composed catalog.
// Invalid owner declarations/order stop startup; there is no legacy fallback.
func AccountRoleCatalog() (*accport.RoleCatalog, error) {
	catalog, err := extension.RoleCatalog(accport.NativeRoleCatalog())
	if err != nil {
		return nil, err
	}
	return accport.WithRoleCatalogOrder(catalog, accport.LegacyRoleCatalog())
}
