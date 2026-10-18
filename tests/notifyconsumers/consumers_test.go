package notifyconsumers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func descriptors(t *testing.T, consumers []notify.Consumer) []port.ConsumerDefinition {
	t.Helper()
	out := make([]port.ConsumerDefinition, len(consumers))
	for i, c := range consumers {
		if c.Handler == nil {
			t.Fatalf("consumer %s has no handler", c.Name)
		}
		out[i] = port.ConsumerDefinition{Name: c.Name, Order: i + 1, Events: c.Events}
	}
	return out
}

func TestOwnedNotificationConsumersMatchLegacy(t *testing.T) {
	regs := []port.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers()}
	defs, events := 0, 0
	for _, r := range regs {
		defs += len(r.Definitions)
		for _, d := range r.Definitions {
			events += len(d.Events)
		}
	}
	if defs != 6 || events != 22 {
		t.Fatalf("owner metadata must have6groups/22events: %d/%d", defs, events)
	}
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../golden/kn-notify-consumers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var legacy []port.ConsumerDefinition
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	owned := descriptors(t, notify.New(nil, clock.System{}, notify.Deps{Consumers: regs}).Consumers())
	if len(legacy) != 16 || !reflect.DeepEqual(legacy, owned) {
		t.Fatalf("changed subscription catalog:\nlegacy=%+v\nowner=%+v", legacy, owned)
	}
}
