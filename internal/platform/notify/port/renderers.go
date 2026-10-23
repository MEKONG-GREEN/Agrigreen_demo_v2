package port

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

// DraftAudience describes a delivery target; notify retains matching and authorization.
type DraftAudience struct {
	National bool
	Units    []string
	OrgRef   *uuid.UUID
	Roles    []string
}

// NotificationDraft contains source-owned content, never inbox identity or tenant authority.
// Notify assigns IDs and source provenance, redacts content and stores/delivers in its inbox transaction.
type NotificationDraft struct {
	RecipientRef                          *uuid.UUID
	Audience                              DraftAudience
	Kind, Severity, Title, Body, DeepLink string
	SubjectRef                            *uuid.UUID
	CreatedAt                             time.Time
	Email                                 bool
	// Channels nil preserves legacy inbox + push and Email. An explicit choice
	// replaces that default; at least one channel must be enabled. Email requires
	// a concrete RecipientRef and configured delivery dependencies.
	Channels *DeliveryChannels
}

// EventRenderer is a pure content builder. No database, transaction or tenant capability is supplied.
type EventRenderer func(eventType string, payload json.RawMessage, occurredAt time.Time) ([]NotificationDraft, error)

type EventRendererRegistration struct {
	Events []string
	Render EventRenderer
}
