package app

import (
	"context"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/jackc/pgx/v5"
	"slices"
)

type digestRegistry struct {
	configured bool
	job        port.DigestJobDefinition
	format     port.DigestFormatter
	events     map[string]bool
}

// RegisterDigestPolicies configures the existing single queue once, before workers start.
func (s *Service) RegisterDigestPolicies(registrations []port.DigestRegistration, subscribed, native []string) {
	if s.digests != nil {
		panic("notify: digest policies already initialized")
	}
	if len(registrations) > 1 {
		panic("notify: one formatter is supported by the physical digest queue")
	}
	registry := &digestRegistry{}
	if len(registrations) == 1 {
		registration := registrations[0]
		if registration.Format == nil || len(registration.Events) == 0 || !rendererEventPattern.MatchString(registration.Job.Name) || registration.Job.Every <= 0 || registration.Job.Timeout <= 0 {
			panic("notify: invalid digest registration")
		}
		allowed, reserved := map[string]bool{}, map[string]bool{}
		for _, event := range subscribed {
			allowed[event] = true
		}
		for _, event := range native {
			reserved[event] = true
		}
		events := map[string]bool{}
		for _, event := range registration.Events {
			if !rendererEventPattern.MatchString(event) || !allowed[event] || reserved[event] || events[event] {
				panic("notify: invalid, duplicate or reserved digest source " + event)
			}
			events[event] = true
		}
		registry.configured = true
		registry.events = events
		registry.format = registration.Format
		registry.job = registration.Job
		if registry.job.LogMessage == "" {
			registry.job.LogMessage = "email tóm tắt"
		}
	}
	s.digests = registry
}
func (s *Service) DigestJobDefinition() (port.DigestJobDefinition, bool) {
	if s.digests == nil || !s.digests.configured {
		return port.DigestJobDefinition{}, false
	}
	return s.digests.job, true
}
func (s *Service) queueDigestDrafts(ctx context.Context, tx pgx.Tx, env outbox.Envelope, drafts []port.DigestDraft) error {
	if len(drafts) == 0 {
		return nil
	}
	if s.digests == nil || !s.digests.configured || !s.digests.events[env.EventType] {
		return fmt.Errorf("notify: %w: digest source is not registered", ErrDependency)
	}
	for _, draft := range drafts {
		// The legacy physical column remains directive_id; policy uses only an opaque reference.
		if err := store.InsertDigestItem(ctx, tx, env.TenantID, domain.DigestItem{Reference: draft.Reference, AudienceUnits: draft.AudienceUnits, IssuedAt: draft.IssuedAt}, env.EventID, s.now()); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) formatDigest(items []domain.DigestItem) (string, string) {
	if s.digests == nil || !s.digests.configured {
		return "", ""
	}
	entries := make([]port.DigestDraft, len(items))
	for i, item := range items {
		entries[i] = port.DigestDraft{Reference: item.Reference, AudienceUnits: slices.Clone(item.AudienceUnits), IssuedAt: item.IssuedAt}
	}
	return s.digests.format(entries)
}
