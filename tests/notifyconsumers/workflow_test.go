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

func TestWorkflowOwnedRenderersMatchLegacy(t *testing.T) {
	var inputs []struct {
		Name, Group, Event, Payload string
		At                          time.Time
	}
	var expected []struct {
		Name   string
		Drafts []notifyport.NotificationDraft
		Error  string
	}
	for path, target := range map[string]any{"../golden/kn-notify-workflow-inputs.json": &inputs, "../golden/kn-notify-workflow.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 77 || len(expected) != len(inputs) {
		t.Fatal("missing workflow vectors")
	}
	renderers := map[string]notifyport.EventRenderer{}
	for _, registration := range extension.WorkflowNotificationRenderers() {
		for _, event := range registration.Events {
			if renderers[event] != nil {
				t.Fatal("duplicate event")
			}
			renderers[event] = registration.Render
		}
	}
	if len(renderers) != 7 {
		t.Fatalf("workflow event count %d", len(renderers))
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			var got []notifyport.NotificationDraft
			var err error
			if renderer := renderers[in.Event]; renderer != nil {
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
				t.Fatalf("got %s / %q; want %s / %q", a, errText, b, want.Error)
			}
		})
	}
	all := map[string]bool{}
	for _, registration := range extension.NotificationRenderers() {
		for _, event := range registration.Events {
			if all[event] {
				t.Fatal("assembled duplicate")
			}
			all[event] = true
		}
	}
	if len(all) != 10 {
		t.Fatalf("assembled owner event count %d", len(all))
	}
}

func TestWorkflowRenderersRequireOwnerRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "empty"} {
		t.Run(mode, func(t *testing.T) {
			var registrations []notifyport.EventRendererRegistration
			if mode == "empty" {
				registrations = []notifyport.EventRendererRegistration{}
			}
			module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Renderers: registrations})
			for _, registration := range extension.WorkflowNotificationRenderers() {
				for _, event := range registration.Events {
					if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err != nil {
						t.Errorf("Workflow legacy fallback without owner renderer for %s: %v", event, err)
					}
				}
			}
		})
	}
	module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Renderers: extension.WorkflowNotificationRenderers()})
	for _, registration := range extension.WorkflowNotificationRenderers() {
		for _, event := range registration.Events {
			if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err == nil {
				t.Errorf("registered Workflow renderer did not reject malformed %s", event)
			}
		}
	}
}
