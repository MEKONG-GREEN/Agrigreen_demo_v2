package port

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

// EventBuilder may use injected read-only directories to resolve recipients.
// Notify supplies no transaction or database; inbox identity, provenance and delivery remain in the capability.
type EventBuilder func(ctx context.Context, tenantID uuid.UUID, eventType string, payload json.RawMessage, occurredAt time.Time) ([]NotificationDraft, error)

type EventBuilderRegistration struct {
	Events []string
	Build  EventBuilder
	// Exactly one of Build or Plan must be supplied.
	Plan EventPlanner
}
