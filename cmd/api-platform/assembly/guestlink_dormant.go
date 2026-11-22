package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink"
	guestport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NewDormantGuestLinks reserves the generic capability in the deployment plan.
// It cannot issue/open anything: no enabled config, key, owner or delivery adapter.
// Existing constructors and HTTP routing do not call this factory. W10.18's owner
// and verified-contact integration remains fail-closed in follow-up #200.
func NewDormantGuestLinks(unit tenant.Beginner, clk clock.Clock) (guestport.GuestLinks, error) {
	return guestlink.NewDormant(unit, clk)
}
