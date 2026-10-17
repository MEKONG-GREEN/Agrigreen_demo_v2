package notify

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/jackc/pgx/v5"
)

func sampleConsumerRegistration() port.ConsumerRegistration {
	return port.ConsumerRegistration{Definitions: []port.ConsumerDefinition{
		{Name: "fixture.owned", Order: 2, Events: []string{"fixture.owned.event"}},
	}}
}
func sampleBaseConsumers() []orderedConsumer {
	return []orderedConsumer{{order: 1, consumer: Consumer{Name: "fixture.native", Events: []string{"fixture.native.event"}}}}
}
func TestConsumerRegistryCopiesAndHandlerOwnership(t *testing.T) {
	var nativeCalls, sourceCalls int
	native := func(context.Context, pgx.Tx, outbox.Envelope) error { nativeCalls++; return nil }
	source := func(context.Context, pgx.Tx, outbox.Envelope) error { sourceCalls++; return nil }
	base := sampleBaseConsumers()
	base[0].consumer.Handler = native
	reg := sampleConsumerRegistration()
	r := newConsumerRegistry(base, []port.ConsumerRegistration{reg})
	base[0].consumer.Events[0] = "corrupt"
	reg.Definitions[0].Events[0] = "corrupt"
	out := r.consumers(source)
	out[0].Events[0] = "corrupt"
	out = r.consumers(source)
	if len(out) != 2 || out[0].Events[0] != "fixture.native.event" || out[1].Events[0] != "fixture.owned.event" {
		t.Fatalf("aliased subscriptions: %+v", out)
	}
	for _, c := range out {
		if c.Handler == nil {
			t.Fatal("missing handler")
		}
		if err := c.Handler(context.Background(), nil, outbox.Envelope{}); err != nil {
			t.Fatal(err)
		}
	}
	if nativeCalls != 1 || sourceCalls != 1 {
		t.Fatalf("specialized handler overwritten: %d %d", nativeCalls, sourceCalls)
	}
	other := newConsumerRegistry(sampleBaseConsumers(), nil).consumers(source)
	if len(other) != 1 || other[0].Events[0] != "fixture.native.event" {
		t.Fatal("registry leaked across instances")
	}
}
func TestConsumerRegistryRejectsInvalid(t *testing.T) {
	for _, name := range []string{"name", "order", "order-high", "empty-events", "event", "duplicate-name", "duplicate-order", "duplicate-event", "duplicate-event-in-item", "native-name", "native-order", "native-event", "base-name"} {
		t.Run(name, func(t *testing.T) {
			base := sampleBaseConsumers()
			reg := sampleConsumerRegistration()
			d := &reg.Definitions[0]
			switch name {
			case "name":
				d.Name = "Invalid name"
			case "order":
				d.Order = 0
			case "order-high":
				d.Order = 65536
			case "empty-events":
				d.Events = nil
			case "event":
				d.Events[0] = "invalid event"
			case "duplicate-name":
				reg.Definitions = append(reg.Definitions, *d)
			case "duplicate-order":
				reg.Definitions = append(reg.Definitions, port.ConsumerDefinition{Name: "fixture.other", Order: 2, Events: []string{"fixture.other.event"}})
			case "duplicate-event":
				reg.Definitions = append(reg.Definitions, port.ConsumerDefinition{Name: "fixture.other", Order: 3, Events: []string{"fixture.owned.event"}})
			case "duplicate-event-in-item":
				d.Events = append(d.Events, d.Events[0])
			case "native-name":
				d.Name = "fixture.native"
			case "native-order":
				d.Order = 1
			case "native-event":
				d.Events[0] = "fixture.native.event"
			case "base-name":
				base = append(base, base[0])
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid registration accepted")
				}
			}()
			newConsumerRegistry(base, []port.ConsumerRegistration{reg})
		})
	}
}
func TestConsumerRegistryAddsOrderedSubscription(t *testing.T) {
	reg := sampleConsumerRegistration()
	reg.Definitions = append(reg.Definitions, port.ConsumerDefinition{Name: "fixture.extra", Order: 3, Events: []string{"fixture.extra.event"}})
	r := newConsumerRegistry(sampleBaseConsumers(), []port.ConsumerRegistration{reg})
	for i, c := range r.consumers(inbox.Handler(func(context.Context, pgx.Tx, outbox.Envelope) error { return nil })) {
		if c.Name != []string{"fixture.native", "fixture.owned", "fixture.extra"}[i] {
			t.Fatalf("unstable order: %+v", c)
		}
	}
}
