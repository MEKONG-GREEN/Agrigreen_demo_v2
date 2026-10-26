package port

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

// DigestDraft is an opaque source reference and audience labels for the configured digest queue.
// Tenant, source-event identity and queue timestamps are assigned by notify.
type DigestDraft struct {
	Reference     uuid.UUID
	AudienceUnits []string
	IssuedAt      time.Time
}

// NotificationPlan declares notification content and digest contributions without a transaction.
type NotificationPlan struct {
	Drafts  []NotificationDraft
	Digests []DigestDraft
}
type EventPlanner func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (NotificationPlan, error)
type DigestFormatter func([]DigestDraft) (subject, body string)

// DigestJobDefinition is value-only metadata for the existing single digest queue.
type DigestJobDefinition struct {
	Name       string
	Every      time.Duration
	RunOnStart bool
	Timeout    time.Duration
	LogMessage string
}

// DigestRegistration binds the single physical queue to one formatter and owned source subscriptions.
type DigestRegistration struct {
	Events []string
	Job    DigestJobDefinition
	Format DigestFormatter
}

// DigestRecipients resolves service mailboxes for the authenticated queue tenant.
// A configured resolver is authoritative: errors or an empty result never fall
// back to a process-wide recipient list. The caller may modify the returned slice.
type DigestRecipients func(context.Context, uuid.UUID) ([]string, error)
