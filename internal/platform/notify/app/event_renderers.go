package app

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
)

type eventRendererRegistry map[string]port.EventRenderer

var rendererEventPattern = regexp.MustCompile("^[a-z][a-z0-9_.-]{1,159}$")

// RegisterEventRenderers is called once by the module constructor, before workers start.
// Only application-owned subscriptions may supply renderers; native delivery handlers are reserved.
func (s *Service) RegisterEventRenderers(registrations []port.EventRendererRegistration, subscribed, native []string) {
	if s.eventRenderers != nil {
		panic("notify: event renderers already initialized")
	}
	allowed, reserved := map[string]bool{}, map[string]bool{}
	for _, event := range subscribed {
		allowed[event] = true
	}
	for _, event := range native {
		reserved[event] = true
	}
	registry := eventRendererRegistry{}
	for _, registration := range registrations {
		if registration.Render == nil || len(registration.Events) == 0 {
			panic("notify: invalid event renderer")
		}
		for _, event := range registration.Events {
			if !rendererEventPattern.MatchString(event) || !allowed[event] || reserved[event] || registry[event] != nil {
				panic("notify: invalid, duplicate or reserved renderer event " + event)
			}
			registry[event] = registration.Render
		}
	}
	s.eventRenderers = registry
}

func copyRendererRef(ref *uuid.UUID) *uuid.UUID {
	if ref == nil {
		return nil
	}
	value := *ref
	return &value
}

func (r eventRendererRegistry) render(event string, payload json.RawMessage, at time.Time) ([]domain.Notification, bool, error) {
	renderer, ok := r[event]
	if !ok {
		return nil, false, nil
	}
	drafts, err := renderer(event, slices.Clone(payload), at)
	if err != nil {
		return nil, true, err
	}
	if drafts == nil {
		return nil, true, nil
	}
	notifications := make([]domain.Notification, len(drafts))
	for i, d := range drafts {
		notifications[i] = domain.Notification{
			RecipientRef: copyRendererRef(d.RecipientRef),
			Audience: domain.Audience{National: d.Audience.National, Units: slices.Clone(d.Audience.Units),
				OrgRef: copyRendererRef(d.Audience.OrgRef), Roles: slices.Clone(d.Audience.Roles)},
			Kind: d.Kind, Severity: d.Severity, Title: d.Title, Body: d.Body, DeepLink: d.DeepLink,
			SubjectRef: copyRendererRef(d.SubjectRef), CreatedAt: d.CreatedAt, Email: d.Email,
		}
	}
	return notifications, true, nil
}
