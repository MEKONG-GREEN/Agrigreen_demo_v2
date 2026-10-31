package adhocintegration_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

var fixtureKHTCProfile = func() port.Profile {
	for _, p := range extension.FormProfiles() {
		if p.Name == "kn_khtc" {
			return p
		}
	}
	panic("missing KN profile")
}()
