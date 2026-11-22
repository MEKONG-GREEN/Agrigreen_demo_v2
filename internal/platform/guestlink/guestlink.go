package guestlink

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NewDormant exposes only a disabled factory at the module root. Activating an
// owner and delivery adapter is intentionally not part of this assembly surface.
func NewDormant(unit tenant.Beginner, clk clock.Clock) (port.GuestLinks, error) {
	return app.New(unit, clk, app.Config{}, nil, nil)
}
