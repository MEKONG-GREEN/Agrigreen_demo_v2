package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
	"slices"
	"time"
)

type eventBuilderBinding struct {
	build port.EventBuilder
	plan  port.EventPlanner
}
type eventBuilderRegistry map[string]*eventBuilderBinding

// RegisterEventBuilders is a startup-only operation, with the same ownership checks as pure renderers.
func (s *Service) RegisterEventBuilders(registrations []port.EventBuilderRegistration, subscribed, native []string) {
	if s.eventBuilders != nil {
		panic("notify: event builders already initialized")
	}
	allowed, reserved := map[string]bool{}, map[string]bool{}
	for _, event := range subscribed {
		allowed[event] = true
	}
	for _, event := range native {
		reserved[event] = true
	}
	registry := eventBuilderRegistry{}
	for _, registration := range registrations {
		if (registration.Build == nil) == (registration.Plan == nil) || len(registration.Events) == 0 {
			panic("notify: invalid event builder")
		}
		for _, event := range registration.Events {
			if !rendererEventPattern.MatchString(event) || !allowed[event] || reserved[event] || registry[event] != nil || s.eventRenderers[event] != nil {
				panic("notify: invalid, duplicate or reserved builder event " + event)
			}
			registry[event] = &eventBuilderBinding{build: registration.Build, plan: registration.Plan}
		}
	}
	s.eventBuilders = registry
}

func (r eventBuilderRegistry) build(ctx context.Context, tenantID uuid.UUID, event string, payload json.RawMessage, at time.Time) ([]domain.Notification, []port.DigestDraft, bool, error) {
	binding, ok := r[event]
	if !ok {
		return nil, nil, false, nil
	}
	var plan port.NotificationPlan
	var err error
	if binding.plan != nil {
		plan, err = binding.plan(ctx, tenantID, event, slices.Clone(payload), at)
	} else {
		plan.Drafts, err = binding.build(ctx, tenantID, event, slices.Clone(payload), at)
	}
	if err != nil {
		return nil, nil, true, err
	}
	digests := slices.Clone(plan.Digests)
	for i := range digests {
		digests[i].AudienceUnits = slices.Clone(digests[i].AudienceUnits)
	}
	return notificationsFromDrafts(plan.Drafts), digests, true, nil
}
