package notify

import (
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func TestConsumersRequireOwnerRegistration(t *testing.T) {
	want := []string{port.ConsumerContent, port.ConsumerSync, port.ConsumerParty, port.ConsumerCropCycle,
		port.ConsumerIntegration, port.ConsumerMRV, port.ConsumerEgressAnomaly, port.ConsumerAccounts, port.ConsumerPush, port.ConsumerEmail}
	for _, tc := range []struct {
		name string
		regs []port.ConsumerRegistration
	}{
		{name: "nil"}, {name: "empty", regs: []port.ConsumerRegistration{}}, {name: "empty-definition", regs: []port.ConsumerRegistration{{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumers := New(nil, clock.System{}, Deps{Consumers: tc.regs}).Consumers()
			var got []string
			for _, c := range consumers {
				got = append(got, c.Name)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("missing owner registration must not infer subscriptions: got %d %v, want %d %v", len(got), got, len(want), want)
			}
		})
	}
}
