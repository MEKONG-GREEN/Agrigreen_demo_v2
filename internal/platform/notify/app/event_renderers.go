package app

import (
	"encoding/json"
	"fmt"
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
			if !rendererEventPattern.MatchString(event) || !allowed[event] || reserved[event] || registry[event] != nil || s.eventBuilders[event] != nil {
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
	ns, err := notificationsFromDrafts(drafts)
	return ns, true, err
}

func notificationsFromDrafts(drafts []port.NotificationDraft) ([]domain.Notification, error) {
	if drafts == nil {
		return nil, nil
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
		if channels := d.Channels; channels != nil {
			if !channels.InApp && !channels.Push && !channels.Email {
				return nil, fmt.Errorf("notify: draft %d has no delivery channel", i)
			}
			if channels.Email && (d.RecipientRef == nil || *d.RecipientRef == uuid.Nil) {
				return nil, fmt.Errorf("notify: draft %d email requires a concrete recipient", i)
			}
			notifications[i].SuppressInApp = !channels.InApp
			notifications[i].SuppressPush = !channels.Push
			notifications[i].Email = channels.Email
			notifications[i].RequireEmail = channels.Email
		}
	}
	return notifications, nil
}
