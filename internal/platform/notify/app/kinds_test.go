package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestKindRegistryCopiesAndIsolates(t *testing.T) {
	registrations := []port.KindRegistration{{Kinds: []string{"owned_kind", "other_kind"}}}
	native := []string{"native_kind"}
	service := &Service{}
	service.RegisterKinds(registrations, native)
	registrations[0].Kinds[0] = "corrupt"
	native[0] = "corrupt"
	if !service.kinds["owned_kind"] || !service.kinds["other_kind"] || !service.kinds["native_kind"] || service.kinds["corrupt"] {
		t.Fatal("kind registry aliases inputs")
	}
	other := &Service{}
	other.RegisterKinds(nil, []string{"native_kind"})
	if other.kinds["owned_kind"] || len(other.kinds) != 1 || len(service.kinds) != 3 {
		t.Fatal("kind registry leaked across instances")
	}
}
func TestKindRegistryRejectsInvalidOrDuplicate(t *testing.T) {
	for _, mode := range []string{"empty-registration", "empty-key", "uppercase", "digit", "dot", "long", "duplicate-item", "duplicate-group", "native-collision", "duplicate-native", "invalid-native", "twice"} {
		t.Run(mode, func(t *testing.T) {
			service := &Service{}
			native := []string{"native_kind"}
			registrations := []port.KindRegistration{{Kinds: []string{"owned_kind"}}}
			switch mode {
			case "empty-registration":
				registrations[0].Kinds = nil
			case "empty-key":
				registrations[0].Kinds = []string{""}
			case "uppercase":
				registrations[0].Kinds = []string{"UPPER"}
			case "digit":
				registrations[0].Kinds = []string{"kind_v1"}
			case "dot":
				registrations[0].Kinds = []string{"a.b"}
			case "long":
				registrations[0].Kinds = []string{strings.Repeat("a", 41)}
			case "duplicate-item":
				registrations[0].Kinds = append(registrations[0].Kinds, "owned_kind")
			case "duplicate-group":
				registrations = append(registrations, registrations[0])
			case "native-collision":
				registrations[0].Kinds = []string{"native_kind"}
			case "duplicate-native":
				native = append(native, "native_kind")
			case "invalid-native":
				native = []string{"UPPER"}
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid kind registration accepted")
				}
			}()
			service.RegisterKinds(registrations, native)
			if mode == "twice" {
				service.RegisterKinds(nil, nil)
			}
		})
	}
}
func TestUnregisteredKindRejectsBeforeWriting(t *testing.T) {
	service := &Service{Clock: clock.System{}}
	service.RegisterKinds(nil, []string{"native_kind"})
	recipient := uuid.New()
	for _, kind := range []string{"owned_missing", "fixture_kind", ""} {
		err := service.deliver(context.Background(), nil, outbox.Envelope{EventID: uuid.New(), EventType: "fixture.event"}, []domain.Notification{{RecipientRef: &recipient, Kind: kind, Title: "Fixture", DeepLink: "/fixture", CreatedAt: time.Now()}})
		if !errors.Is(err, ErrDependency) {
			t.Fatalf("unregistered kind touched DB: %v", err)
		}
	}
}
