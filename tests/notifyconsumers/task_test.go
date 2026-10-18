package notifyconsumers_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type taskLookupSeed struct {
	Nil     bool
	Records []notifyport.StaffRef
	Error   bool
}
type taskFixtureDirectory struct {
	seed  taskLookupSeed
	calls []string
}

func (d *taskFixtureDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, units []string) ([]notifyport.StaffRef, error) {
	d.calls = append(d.calls, "staff|"+tid.String()+"|"+strings.Join(units, ","))
	if d.seed.Error {
		return nil, errors.New("fixture staff failure")
	}
	return d.seed.Records, nil
}
func TestTaskOwnedBuildersMatchLegacy(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               taskLookupSeed
	}
	var expected []struct {
		Name   string
		Drafts []notifyport.NotificationDraft
		Error  string
		Calls  []string
	}
	for path, target := range map[string]any{"../golden/kn-notify-task-inputs.json": &inputs, "../golden/kn-notify-task.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 83 || len(expected) != len(inputs) {
		t.Fatal("missing task vectors")
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			directory := &taskFixtureDirectory{seed: in.Lookup}
			var lookup notifyport.StaffDirectory = directory
			if in.Lookup.Nil {
				lookup = nil
			}
			registrations := extension.TaskNotificationBuilders(lookup)
			if len(registrations) != 1 || len(registrations[0].Events) != 2 {
				t.Fatal("task builder catalog")
			}
			got, err := registrations[0].Build(context.Background(), in.TenantID, in.Event, json.RawMessage(in.Payload), in.At)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			want := expected[i]
			if want.Name != in.Name || !reflect.DeepEqual(got, want.Drafts) || errText != want.Error || !reflect.DeepEqual(directory.calls, want.Calls) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want.Drafts)
				t.Fatalf("got %s / %q / %v; want %s / %q / %v", a, errText, directory.calls, b, want.Error, want.Calls)
			}
		})
	}
}

func TestTaskBuildersRequireOwnerRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "empty"} {
		t.Run(mode, func(t *testing.T) {
			var registrations []notifyport.EventBuilderRegistration
			if mode == "empty" {
				registrations = []notifyport.EventBuilderRegistration{}
			}
			module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Builders: registrations})
			for _, registration := range extension.TaskNotificationBuilders(nil) {
				for _, event := range registration.Events {
					if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err != nil {
						t.Errorf("Task legacy fallback without owner builder for %s: %v", event, err)
					}
				}
			}
		})
	}
	module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.TaskNotificationBuilders(nil)})
	for _, registration := range extension.TaskNotificationBuilders(nil) {
		for _, event := range registration.Events {
			if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err == nil {
				t.Errorf("registered Task builder did not reject malformed %s", event)
			}
		}
	}
}
