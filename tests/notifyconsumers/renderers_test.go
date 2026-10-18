package notifyconsumers_test

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestCNTSOwnedRenderersMatchLegacy(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
	}
	var expected []struct {
		Name   string
		Drafts []notifyport.NotificationDraft
		Error  string
	}
	for path, target := range map[string]any{"../golden/kn-notify-cnts-inputs.json": &inputs, "../golden/kn-notify-cnts.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 46 || len(expected) != len(inputs) {
		t.Fatal("missing legacy vectors")
	}
	renderers := map[string]notifyport.EventRenderer{}
	for _, r := range extension.CNTSNotificationRenderers() {
		for _, event := range r.Events {
			if renderers[event] != nil {
				t.Fatal("duplicate renderer")
			}
			renderers[event] = r.Render
		}
	}
	if len(renderers) != 3 {
		t.Fatalf("CNTS events: %d", len(renderers))
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			renderer := renderers[in.Event]
			var got []notifyport.NotificationDraft
			var err error
			if renderer != nil {
				got, err = renderer(in.Event, json.RawMessage(in.Payload), in.At)
			}
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			want := expected[i]
			if want.Name != in.Name || !reflect.DeepEqual(got, want.Drafts) || errText != want.Error {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want.Drafts)
				t.Fatalf("legacy mismatch %s: got %s / %q; want %s / %q", in.Name, a, errText, b, want.Error)
			}
		})
	}
}

// A source consumer subscription alone must never reinstall application business rules.
func TestCNTSRenderersRequireOwnerRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "empty"} {
		t.Run(mode, func(t *testing.T) {
			var registrations []notifyport.EventRendererRegistration
			if mode == "empty" {
				registrations = []notifyport.EventRendererRegistration{}
			}
			module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Renderers: registrations})
			for _, registration := range extension.CNTSNotificationRenderers() {
				for _, event := range registration.Events {
					if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err != nil {
						t.Errorf("CNTS legacy fallback without owner renderer for %s: %v", event, err)
					}
				}
			}
		})
	}
	module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Renderers: extension.CNTSNotificationRenderers()})
	for _, registration := range extension.CNTSNotificationRenderers() {
		for _, event := range registration.Events {
			if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err == nil {
				t.Errorf("registered CNTS renderer did not reject malformed %s", event)
			}
		}
	}
}
