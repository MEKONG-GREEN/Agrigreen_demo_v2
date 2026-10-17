package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
)

func TestEventRendererRegistryCopiesAndProvenance(t *testing.T) {
	ref := uuid.New()
	other := uuid.New()
	at := time.Date(2027, 10, 13, 3, 4, 5, 0, time.UTC)
	drafts := []port.NotificationDraft{{RecipientRef: &ref, Audience: port.DraftAudience{National: true, Units: []string{"30001"}, OrgRef: &ref, Roles: []string{"fixture.role"}},
		Kind: "fixture.kind", Severity: "normal", Title: "Fixture", Body: "Body", DeepLink: "/fixture", SubjectRef: &ref, CreatedAt: at, Email: true}}
	var seenEvent string
	var seenAt time.Time
	renderer := func(event string, payload json.RawMessage, occurred time.Time) ([]port.NotificationDraft, error) {
		seenEvent, seenAt = event, occurred
		payload[0] = 'x'
		return drafts, nil
	}
	registrations := []port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: renderer}}
	s := &Service{}
	s.RegisterEventRenderers(registrations, []string{"fixture.owned"}, nil)
	registrations[0].Events[0] = "corrupt"
	registrations[0].Render = nil
	input := json.RawMessage("{}")
	got, handled, err := s.eventRenderers.render("fixture.owned", input, at)
	if err != nil || !handled || len(got) != 1 || string(input) != "{}" || seenEvent != "fixture.owned" || !seenAt.Equal(at) {
		t.Fatalf("dispatch: %+v %v %v", got, handled, err)
	}
	n := got[0]
	if n.ID != uuid.Nil || n.SourceEventID != uuid.Nil || n.SourceEventType != "" || n.ReadAt != nil || n.SyncSeq != 0 {
		t.Fatal("renderer gained provenance authority")
	}
	if n.Kind != drafts[0].Kind || n.Severity != drafts[0].Severity || n.Title != drafts[0].Title || n.Body != drafts[0].Body || n.DeepLink != drafts[0].DeepLink || !n.CreatedAt.Equal(at) || !n.Email || !n.Audience.National {
		t.Fatal("draft fields lost")
	}
	ref = other
	drafts[0].Audience.Units[0] = "corrupt"
	drafts[0].Audience.Roles[0] = "corrupt"
	drafts[0].Title = "corrupt"
	if *n.RecipientRef == other || *n.SubjectRef == other || *n.Audience.OrgRef == other || n.Audience.Units[0] != "30001" || n.Audience.Roles[0] != "fixture.role" || n.Title != "Fixture" {
		t.Fatal("renderer output aliased")
	}
	*n.RecipientRef = uuid.Nil
	n.Audience.Units[0] = "caller"
	if ref != other || drafts[0].Audience.Units[0] != "corrupt" {
		t.Fatal("caller mutated provider")
	}
	if out, ok, err := s.eventRenderers.render("fixture.absent", nil, at); ok || err != nil || out != nil {
		t.Fatal("unknown event was handled")
	}
}

func TestEventRendererEmptyAndErrorsDoNotFallThrough(t *testing.T) {
	failure := errors.New("fixture error")
	for _, name := range []string{"nil", "empty", "error"} {
		t.Run(name, func(t *testing.T) {
			s := &Service{}
			s.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
				if name == "error" {
					return []port.NotificationDraft{{Title: "ignored"}}, failure
				}
				if name == "empty" {
					return []port.NotificationDraft{}, nil
				}
				return nil, nil
			}}}, []string{"fixture.owned"}, nil)
			out, handled, err := s.eventRenderers.render("fixture.owned", nil, time.Time{})
			if !handled || (name == "nil" && out != nil) || (name == "empty" && (out == nil || len(out) != 0)) || (name == "error" && !errors.Is(err, failure)) {
				t.Fatalf("result: %+v %v %v", out, handled, err)
			}
			want := error(nil)
			if name == "error" {
				want = failure
			}
			if got := s.HandleEvent(context.Background(), nil, outbox.Envelope{EventType: "fixture.owned"}); !errors.Is(got, want) {
				t.Fatalf("fallback/DB unexpectedly used: %v", got)
			}
		})
	}
}

func TestEventRendererInvalidRegistrations(t *testing.T) {
	renderer := func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) { return nil, nil }
	for _, name := range []string{"nil-callback", "empty", "bad-event", "unsubscribed", "native", "duplicate", "duplicate-in-item", "twice"} {
		t.Run(name, func(t *testing.T) {
			regs := []port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: renderer}}
			allowed := []string{"fixture.owned", "fixture.native"}
			native := []string{"fixture.native"}
			switch name {
			case "nil-callback":
				regs[0].Render = nil
			case "empty":
				regs[0].Events = nil
			case "bad-event":
				regs[0].Events = []string{"bad event"}
				allowed = append(allowed, "bad event")
			case "unsubscribed":
				allowed = nil
			case "native":
				regs[0].Events = []string{"fixture.native"}
			case "duplicate":
				regs = append(regs, regs[0])
			case "duplicate-in-item":
				regs[0].Events = append(regs[0].Events, regs[0].Events[0])
			}
			s := &Service{}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid renderer accepted")
				}
			}()
			s.RegisterEventRenderers(regs, allowed, native)
			if name == "twice" {
				s.RegisterEventRenderers(nil, nil, nil)
			}
		})
	}
	// Independent instances and explicitly empty registrations have no shared state.
	a, b := &Service{}, &Service{}
	a.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: renderer}}, []string{"fixture.owned"}, nil)
	b.RegisterEventRenderers(nil, nil, nil)
	if len(a.eventRenderers) != 1 || !reflect.DeepEqual(b.eventRenderers, eventRendererRegistry{}) {
		t.Fatal("registry leaked across instances")
	}
}
