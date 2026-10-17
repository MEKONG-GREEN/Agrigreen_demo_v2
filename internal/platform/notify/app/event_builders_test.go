package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestEventBuilderCopiesContextAndProvenance(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "fixture")
	tid, ref := uuid.New(), uuid.New()
	original := ref
	at := time.Date(2027, 10, 13, 3, 4, 5, 0, time.UTC)
	drafts := []port.NotificationDraft{{RecipientRef: &ref, Audience: port.DraftAudience{National: true, Units: []string{"30001"}, OrgRef: &ref, Roles: []string{"fixture.role"}}, Kind: "fixture.kind", Severity: "normal", Title: "Fixture", Body: "Body", DeepLink: "/fixture", SubjectRef: &ref, CreatedAt: at, Email: true}}
	build := func(gotCtx context.Context, gotTenant uuid.UUID, event string, payload json.RawMessage, occurred time.Time) ([]port.NotificationDraft, error) {
		if gotCtx.Value(marker{}) != "fixture" || gotTenant != tid || event != "fixture.owned" || !occurred.Equal(at) {
			t.Fatal("read-directory context or tenant lost")
		}
		payload[0] = 'x'
		return drafts, nil
	}
	regs := []port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Build: build}}
	s := &Service{}
	s.RegisterEventBuilders(regs, []string{"fixture.owned"}, nil)
	regs[0].Events[0] = "corrupt"
	regs[0].Build = nil
	payload := json.RawMessage("{}")
	got, handled, err := s.eventBuilders.build(ctx, tid, "fixture.owned", payload, at)
	if err != nil || !handled || len(got) != 1 || string(payload) != "{}" {
		t.Fatalf("dispatch %v %v %v", got, handled, err)
	}
	n := got[0]
	if n.ID != uuid.Nil || n.SourceEventID != uuid.Nil || n.SourceEventType != "" || n.ReadAt != nil || n.SyncSeq != 0 {
		t.Fatal("builder gained identity authority")
	}
	if n.Title != "Fixture" || n.Body != "Body" || n.Kind != "fixture.kind" || n.Severity != "normal" || n.DeepLink != "/fixture" || !n.Email || !n.CreatedAt.Equal(at) || !n.Audience.National {
		t.Fatal("draft fields changed")
	}
	ref = uuid.New()
	drafts[0].Audience.Units[0] = "corrupt"
	drafts[0].Audience.Roles[0] = "corrupt"
	if *n.RecipientRef != original || *n.SubjectRef != original || *n.Audience.OrgRef != original || n.Audience.Units[0] != "30001" || n.Audience.Roles[0] != "fixture.role" {
		t.Fatal("returned draft aliases builder")
	}
	*n.RecipientRef = uuid.Nil
	n.Audience.Units[0] = "caller"
	if ref == uuid.Nil || drafts[0].Audience.Units[0] != "corrupt" {
		t.Fatal("caller changed builder state")
	}
	if ns, ok, err := s.eventBuilders.build(ctx, tid, "fixture.absent", nil, at); ok || err != nil || ns != nil {
		t.Fatal("unknown event was handled")
	}
	other := &Service{}
	other.RegisterEventBuilders(nil, nil, nil)
	if len(other.eventBuilders) != 0 || len(s.eventBuilders) != 1 {
		t.Fatal("instance registry leaked")
	}
}

func TestEventBuilderEmptyAndErrorsDoNotFallThrough(t *testing.T) {
	failure := errors.New("fixture lookup failure")
	for _, mode := range []string{"nil", "empty", "error"} {
		t.Run(mode, func(t *testing.T) {
			s := &Service{}
			s.RegisterEventBuilders([]port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Build: func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
				if mode == "error" {
					return []port.NotificationDraft{{Title: "ignored"}}, failure
				}
				if mode == "empty" {
					return []port.NotificationDraft{}, nil
				}
				return nil, nil
			}}}, []string{"fixture.owned"}, nil)
			ns, ok, err := s.eventBuilders.build(context.Background(), uuid.New(), "fixture.owned", nil, time.Time{})
			if !ok || (mode == "nil" && ns != nil) || (mode == "empty" && (ns == nil || len(ns) != 0)) || (mode == "error" && (!errors.Is(err, failure) || ns != nil)) {
				t.Fatal("nil/empty/error contract")
			}
			var want error
			if mode == "error" {
				want = failure
			}
			if got := s.HandleEvent(context.Background(), nil, outbox.Envelope{EventType: "fixture.owned"}); !errors.Is(got, want) {
				t.Fatalf("fallback/DB used: %v", got)
			}
		})
	}
}

func TestEventBuilderInvalidAndCrossRegistry(t *testing.T) {
	build := func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
		return nil, nil
	}
	render := func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) { return nil, nil }
	for _, mode := range []string{"nil-callback", "empty", "invalid-event", "unknown", "native", "duplicate", "duplicate-in-item", "twice", "renderer-first", "builder-first"} {
		t.Run(mode, func(t *testing.T) {
			s := &Service{}
			regs := []port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Build: build}}
			allowed := []string{"fixture.owned", "fixture.native"}
			native := []string{"fixture.native"}
			switch mode {
			case "nil-callback":
				regs[0].Build = nil
			case "empty":
				regs[0].Events = nil
			case "invalid-event":
				regs[0].Events = []string{"bad event"}
				allowed = append(allowed, "bad event")
			case "unknown":
				allowed = nil
			case "native":
				regs[0].Events = []string{"fixture.native"}
			case "duplicate":
				regs = append(regs, regs[0])
			case "duplicate-in-item":
				regs[0].Events = append(regs[0].Events, regs[0].Events[0])
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid builder registration accepted")
				}
			}()
			if mode == "renderer-first" {
				s.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: render}}, allowed, native)
			}
			s.RegisterEventBuilders(regs, allowed, native)
			if mode == "twice" {
				s.RegisterEventBuilders(nil, nil, nil)
			}
			if mode == "builder-first" {
				s.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: render}}, allowed, native)
			}
		})
	}
	// Distinct registered event families may coexist.
	s := &Service{}
	s.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.pure"}, Render: render}}, []string{"fixture.pure", "fixture.lookup"}, nil)
	s.RegisterEventBuilders([]port.EventBuilderRegistration{{Events: []string{"fixture.lookup"}, Build: build}}, []string{"fixture.pure", "fixture.lookup"}, nil)
	if _, ok, err := s.eventRenderers.render("fixture.pure", nil, time.Time{}); !ok || err != nil {
		t.Fatal("disjoint pure renderer lost")
	}
	if _, ok, err := s.eventBuilders.build(context.Background(), uuid.Nil, "fixture.lookup", nil, time.Time{}); !ok || err != nil {
		t.Fatal("disjoint lookup builder lost")
	}
}
