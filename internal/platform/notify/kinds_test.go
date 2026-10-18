package notify

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"testing"
)

func TestKindConstructorRejectsNativeAndDuplicate(t *testing.T) {
	for _, mode := range []string{"native", "duplicate", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			registrations := []port.KindRegistration{{Kinds: []string{"fixture_kind"}}}
			if mode == "native" {
				registrations[0].Kinds = []string{port.KindAlert}
			}
			if mode == "duplicate" {
				registrations = append(registrations, registrations[0])
			}
			if mode == "invalid" {
				registrations[0].Kinds = []string{"bad kind"}
			}
			defer func() {
				if recover() == nil {
					t.Fatal("constructor accepted invalid kind registration")
				}
			}()
			New(nil, clock.System{}, Deps{Kinds: registrations})
		})
	}
	New(nil, clock.System{}, Deps{Kinds: []port.KindRegistration{{Kinds: []string{"fixture_kind"}}}})
}
