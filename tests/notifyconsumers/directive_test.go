package notifyconsumers_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type directiveFixtureLookup struct {
	NilStaff, NilUnits, StaffError bool
	Staff                          []port.StaffRef
	Children                       map[string][]string
	NotFound                       []string
	UnitsError                     string
}
type directiveFixtureDirectory struct {
	seed  directiveFixtureLookup
	calls []string
}

func (d *directiveFixtureDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, units []string) ([]port.StaffRef, error) {
	d.calls = append(d.calls, "staff|"+tid.String()+"|"+strings.Join(units, ","))
	if d.seed.StaffError {
		return nil, errors.New("fixture staff failure")
	}
	return d.seed.Staff, nil
}
func (d *directiveFixtureDirectory) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	d.calls = append(d.calls, "unexpected-get|"+code)
	return partyport.AdminUnit{}, errors.New("unexpected Get")
}
func (d *directiveFixtureDirectory) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	d.calls = append(d.calls, "unexpected-current|"+code)
	return nil, errors.New("unexpected Current")
}
func (d *directiveFixtureDirectory) Descendants(_ context.Context, code string, at time.Time) ([]string, error) {
	d.calls = append(d.calls, "desc|"+code+"|"+at.Format(time.RFC3339Nano))
	if d.seed.UnitsError == code {
		return nil, errors.New("fixture units failure")
	}
	if slices.Contains(d.seed.NotFound, code) {
		return nil, partyport.ErrUnitNotFound
	}
	return d.seed.Children[code], nil
}

func TestDirectiveOwnedPlanMatchesLegacy(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
		TenantID             uuid.UUID
		Lookup               directiveFixtureLookup
	}
	var expected []struct {
		Name    string
		Drafts  []port.NotificationDraft
		Digests []port.DigestDraft
		Error   string
		Calls   []string
	}
	for path, target := range map[string]any{"../golden/kn-notify-directive-inputs.json": &inputs, "../golden/kn-notify-directive.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 52 || len(expected) != len(inputs) {
		t.Fatal("missing directive vectors")
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			directory := &directiveFixtureDirectory{seed: in.Lookup}
			var staff port.StaffDirectory = directory
			var units partyport.AdminUnits = directory
			if in.Lookup.NilStaff {
				staff = nil
			}
			if in.Lookup.NilUnits {
				units = nil
			}
			registrations := extension.DirectiveNotificationBuilders(staff, units)
			if len(registrations) != 1 || len(registrations[0].Events) != 1 || registrations[0].Plan == nil || registrations[0].Build != nil {
				t.Fatal("directive plan registration")
			}
			got, err := registrations[0].Plan(context.Background(), in.TenantID, in.Event, json.RawMessage(in.Payload), in.At)
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			want := expected[i]
			if want.Name != in.Name || !reflect.DeepEqual(got.Drafts, want.Drafts) || !reflect.DeepEqual(got.Digests, want.Digests) || errText != want.Error || !reflect.DeepEqual(directory.calls, want.Calls) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want)
				t.Fatalf("got %s / %q / %v; want %s", a, errText, directory.calls, b)
			}
		})
	}
}
func TestDirectiveDigestAndJobMatchLegacy(t *testing.T) {
	var inputs []struct {
		Name  string
		Items []port.DigestDraft
	}
	var expected []struct{ Name, Subject, Body string }
	for path, target := range map[string]any{"../golden/kn-notify-digest-inputs.json": &inputs, "../golden/kn-notify-digest.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 11 || len(expected) != len(inputs) {
		t.Fatal("missing digest vectors")
	}
	registration := extension.NotificationDigest()
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			subject, body := registration.Format(in.Items)
			want := expected[i]
			if want.Name != in.Name || subject != want.Subject || body != want.Body {
				t.Fatal("full digest subject/body changed")
			}
		})
	}
	var old struct {
		Name       string
		EveryNS    int64
		RunOnStart bool
		TimeoutNS  int64
	}
	raw, err := os.ReadFile("../golden/kn-notify-digest-job.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	def := registration.Job
	if len(registration.Events) != 1 || registration.Events[0] != "pf.extension.directive.issued" || def.Name != old.Name || int64(def.Every) != old.EveryNS || def.RunOnStart != old.RunOnStart || int64(def.Timeout) != old.TimeoutNS {
		t.Fatal("digest job/source contract changed")
	}
}

func TestDirectivePlannerRequiresOwnerRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "empty"} {
		t.Run(mode, func(t *testing.T) {
			var plans []port.EventBuilderRegistration
			if mode == "empty" {
				plans = []port.EventBuilderRegistration{}
			}
			module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: plans, Digests: []port.DigestRegistration{extension.NotificationDigest()}})
			for _, registration := range extension.DirectiveNotificationBuilders(nil, nil) {
				for _, event := range registration.Events {
					if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err != nil {
						t.Errorf("Directive legacy fallback without owner planner: %v", err)
					}
				}
			}
		})
	}
	module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil), Digests: []port.DigestRegistration{extension.NotificationDigest()}})
	for _, registration := range extension.DirectiveNotificationBuilders(nil, nil) {
		for _, event := range registration.Events {
			if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{EventType: event, Payload: json.RawMessage("{")}); err == nil {
				t.Fatal("registered Directive plan did not reject malformed payload")
			}
		}
	}
}
func TestDigestJobRequiresOwnerRegistration(t *testing.T) {
	for _, mode := range []string{"nil", "empty"} {
		t.Run(mode, func(t *testing.T) {
			var policies []port.DigestRegistration
			if mode == "empty" {
				policies = []port.DigestRegistration{}
			}
			module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil), Digests: policies})
			if len(module.Jobs(nil, nil)) != 0 {
				t.Fatal("Digest legacy job without owner registration")
			}
		})
	}
}
