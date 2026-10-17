package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestEventPlannerCopiesContextIntentAndProvenance(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "fixture")
	tid, ref := uuid.New(), uuid.New()
	original := ref
	at := time.Date(2027, 10, 13, 3, 4, 5, 0, time.UTC)
	plan := port.NotificationPlan{Drafts: []port.NotificationDraft{{RecipientRef: &ref, Audience: port.DraftAudience{Units: []string{"unit"}, Roles: []string{"role"}, OrgRef: &ref}, Kind: "fixture", Title: "Title", Body: "Body", DeepLink: "/fixture", SubjectRef: &ref, CreatedAt: at}}, Digests: []port.DigestDraft{{Reference: ref, AudienceUnits: []string{"unit"}, IssuedAt: at}}}
	calls := 0
	registration := []port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Plan: func(got context.Context, tenant uuid.UUID, event string, payload json.RawMessage, occurred time.Time) (port.NotificationPlan, error) {
		calls++
		if got.Value(marker{}) != "fixture" || tenant != tid || event != "fixture.owned" || !occurred.Equal(at) {
			t.Fatal("planner context lost")
		}
		payload[0] = 'x'
		return plan, nil
	}}}
	svc := &Service{}
	svc.RegisterEventBuilders(registration, []string{"fixture.owned"}, nil)
	registration[0].Events[0] = "corrupt"
	registration[0].Plan = nil
	payload := json.RawMessage("{}")
	ns, digests, handled, err := svc.eventBuilders.build(ctx, tid, "fixture.owned", payload, at)
	if err != nil || !handled || calls != 1 || len(ns) != 1 || len(digests) != 1 || string(payload) != "{}" {
		t.Fatal("planner dispatch/input clone")
	}
	if ns[0].ID != uuid.Nil || ns[0].SourceEventID != uuid.Nil || ns[0].SourceEventType != "" || ns[0].ReadAt != nil || ns[0].SyncSeq != 0 {
		t.Fatal("planner gained identity authority")
	}
	if ns[0].Title != "Title" || ns[0].Body != "Body" || ns[0].DeepLink != "/fixture" || digests[0].Reference != original || !digests[0].IssuedAt.Equal(at) {
		t.Fatal("plan fields changed")
	}
	ref = uuid.New()
	plan.Drafts[0].Audience.Units[0] = "changed"
	plan.Drafts[0].Audience.Roles[0] = "changed"
	plan.Digests[0].AudienceUnits[0] = "changed"
	plan.Digests[0].Reference = uuid.Nil
	if *ns[0].RecipientRef != original || *ns[0].SubjectRef != original || *ns[0].Audience.OrgRef != original || ns[0].Audience.Units[0] != "unit" || ns[0].Audience.Roles[0] != "role" || digests[0].Reference != original || digests[0].AudienceUnits[0] != "unit" {
		t.Fatal("plan output aliases owner")
	}
	digests[0].AudienceUnits[0] = "caller"
	*ns[0].RecipientRef = uuid.Nil
	if plan.Digests[0].AudienceUnits[0] != "changed" || ref == uuid.Nil {
		t.Fatal("caller changed owner plan")
	}
	if ns, digests, ok, err := svc.eventBuilders.build(ctx, tid, "fixture.absent", nil, at); ok || err != nil || ns != nil || digests != nil {
		t.Fatal("unknown planner handled")
	}
}
func TestEventPlannerEmptyPartialErrorsAndDigestGate(t *testing.T) {
	failure := errors.New("fixture planner failure")
	for _, mode := range []string{"nil", "empty", "error"} {
		t.Run(mode, func(t *testing.T) {
			svc := &Service{}
			calls := 0
			svc.RegisterEventBuilders([]port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Plan: func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (port.NotificationPlan, error) {
				calls++
				if mode == "error" {
					return port.NotificationPlan{Drafts: []port.NotificationDraft{{Title: "ignored"}}, Digests: []port.DigestDraft{{Reference: uuid.New()}}}, failure
				}
				if mode == "empty" {
					return port.NotificationPlan{Drafts: []port.NotificationDraft{}, Digests: []port.DigestDraft{}}, nil
				}
				return port.NotificationPlan{}, nil
			}}}, []string{"fixture.owned"}, nil)
			ns, digests, ok, err := svc.eventBuilders.build(context.Background(), uuid.New(), "fixture.owned", nil, time.Time{})
			if !ok || (mode == "nil" && (ns != nil || digests != nil)) || (mode == "empty" && (ns == nil || digests == nil || len(ns) != 0 || len(digests) != 0)) || (mode == "error" && (!errors.Is(err, failure) || ns != nil || digests != nil)) {
				t.Fatal("nil/empty/error plan contract")
			}
			var want error
			if mode == "error" {
				want = failure
			}
			if err := svc.HandleEvent(context.Background(), nil, outbox.Envelope{EventType: "fixture.owned"}); !errors.Is(err, want) || calls != 2 {
				t.Fatal("planner fell through, wrote DB or lost error")
			}
		})
	}
	svc := &Service{}
	if err := svc.queueDigestDrafts(context.Background(), nil, outbox.Envelope{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.queueDigestDrafts(context.Background(), nil, outbox.Envelope{EventType: "fixture.owned"}, []port.DigestDraft{{}}); !errors.Is(err, ErrDependency) {
		t.Fatal("missing digest policy touched DB")
	}
	svc.RegisterDigestPolicies([]port.DigestRegistration{{Events: []string{"fixture.other"}, Job: port.DigestJobDefinition{Name: "fixture.digest", Every: time.Hour, Timeout: time.Minute}, Format: func([]port.DigestDraft) (string, string) { return "s", "b" }}}, []string{"fixture.owned", "fixture.other"}, nil)
	if err := svc.queueDigestDrafts(context.Background(), nil, outbox.Envelope{EventType: "fixture.owned"}, []port.DigestDraft{{}}); !errors.Is(err, ErrDependency) {
		t.Fatal("digest source gate touched DB")
	}
}
func TestEventPlannerRegistrationGuards(t *testing.T) {
	plan := func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (port.NotificationPlan, error) {
		return port.NotificationPlan{}, nil
	}
	build := func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
		return nil, nil
	}
	render := func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) { return nil, nil }
	for _, mode := range []string{"both", "unknown", "native", "duplicate-plan", "duplicate-build", "renderer-first", "planner-first", "twice"} {
		t.Run(mode, func(t *testing.T) {
			svc := &Service{}
			allowed := []string{"fixture.owned", "fixture.native"}
			native := []string{"fixture.native"}
			regs := []port.EventBuilderRegistration{{Events: []string{"fixture.owned"}, Plan: plan}}
			switch mode {
			case "both":
				regs[0].Build = build
			case "unknown":
				allowed = nil
			case "native":
				regs[0].Events = []string{"fixture.native"}
			case "duplicate-plan":
				regs = append(regs, regs[0])
			case "duplicate-build":
				regs = append(regs, port.EventBuilderRegistration{Events: []string{"fixture.owned"}, Build: build})
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid planner registration accepted")
				}
			}()
			if mode == "renderer-first" {
				svc.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: render}}, allowed, native)
			}
			svc.RegisterEventBuilders(regs, allowed, native)
			if mode == "planner-first" {
				svc.RegisterEventRenderers([]port.EventRendererRegistration{{Events: []string{"fixture.owned"}, Render: render}}, allowed, native)
			}
			if mode == "twice" {
				svc.RegisterEventBuilders(nil, nil, nil)
			}
		})
	}
}
func TestDigestRegistrationCopiesAndValidation(t *testing.T) {
	ref := uuid.New()
	at := time.Date(2027, 10, 13, 3, 4, 5, 0, time.UTC)
	definition := port.DigestJobDefinition{Name: "fixture.digest", Every: time.Hour, Timeout: time.Minute, RunOnStart: true, LogMessage: "Fixture"}
	regs := []port.DigestRegistration{{Events: []string{"fixture.owned"}, Job: definition, Format: func(entries []port.DigestDraft) (string, string) {
		if len(entries) != 1 || entries[0].Reference != ref || !entries[0].IssuedAt.Equal(at) {
			t.Fatal("formatter projection")
		}
		entries[0].Reference = uuid.Nil
		entries[0].AudienceUnits[0] = "changed"
		return "subject", "body"
	}}}
	svc := &Service{}
	svc.RegisterDigestPolicies(regs, []string{"fixture.owned"}, nil)
	regs[0].Events[0] = "corrupt"
	regs[0].Job.Name = "corrupt"
	regs[0].Job.Every = 0
	regs[0].Format = nil
	got, ok := svc.DigestJobDefinition()
	if !ok || got != definition || !svc.digests.events["fixture.owned"] || svc.digests.events["corrupt"] {
		t.Fatal("digest registration aliases input")
	}
	items := []domain.DigestItem{{DirectiveID: ref, AudienceUnits: []string{"unit"}, IssuedAt: at}}
	subject, body := svc.formatDigest(items)
	if subject != "subject" || body != "body" || items[0].DirectiveID != ref || items[0].AudienceUnits[0] != "unit" {
		t.Fatal("formatter changed queue state")
	}
	other := &Service{}
	other.RegisterDigestPolicies(nil, nil, nil)
	if _, ok := other.DigestJobDefinition(); ok {
		t.Fatal("digest policy leaked across instances")
	}
	for _, mode := range []string{"multiple", "empty-events", "bad-name", "zero-every", "zero-timeout", "nil-format", "unknown", "native", "duplicate", "twice"} {
		t.Run(mode, func(t *testing.T) {
			svc := &Service{}
			registrations := []port.DigestRegistration{{Events: []string{"fixture.owned"}, Job: definition, Format: func([]port.DigestDraft) (string, string) { return "s", "b" }}}
			switch mode {
			case "multiple":
				registrations = append(registrations, registrations[0])
			case "empty-events":
				registrations[0].Events = nil
			case "bad-name":
				registrations[0].Job.Name = "bad name"
			case "zero-every":
				registrations[0].Job.Every = 0
			case "zero-timeout":
				registrations[0].Job.Timeout = 0
			case "nil-format":
				registrations[0].Format = nil
			case "unknown":
				registrations[0].Events = []string{"fixture.unknown"}
			case "native":
				registrations[0].Events = []string{"fixture.native"}
			case "duplicate":
				registrations[0].Events = append(registrations[0].Events, registrations[0].Events[0])
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid digest registration accepted")
				}
			}()
			svc.RegisterDigestPolicies(registrations, []string{"fixture.owned", "fixture.native"}, []string{"fixture.native"})
			if mode == "twice" {
				svc.RegisterDigestPolicies(nil, nil, nil)
			}
		})
	}
}
