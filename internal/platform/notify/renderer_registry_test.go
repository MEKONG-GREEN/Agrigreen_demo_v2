package notify

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"testing"
	"time"
)

func TestRendererConstructorChecksConsumerOwnership(t *testing.T) {
	renderer := func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) { return nil, nil }
	for _, event := range []string{"fixture.unsubscribed", "pf.accounts.user.dormant_warning", "pf.notify.push.requested", "pf.notify.email.requested"} {
		t.Run(event, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("unsubscribed/native override accepted")
				}
			}()
			New(nil, clock.System{}, Deps{Renderers: []port.EventRendererRegistration{{Events: []string{event}, Render: renderer}}})
		})
	}
	reg := sampleConsumerRegistration()
	reg.Definitions[0].Order = 1 // Slot 2 belongs to the native content consumer.
	New(nil, clock.System{}, Deps{Consumers: []port.ConsumerRegistration{reg},
		Renderers: []port.EventRendererRegistration{{Events: []string{"fixture.owned.event"}, Render: renderer}}})
}
