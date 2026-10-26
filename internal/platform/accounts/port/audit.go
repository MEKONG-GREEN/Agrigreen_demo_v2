package port

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
)

// AuditValueKind deliberately excludes free text and unstructured JSON.
type AuditValueKind string

const (
	AuditBoolean AuditValueKind = "boolean"
	AuditRef     AuditValueKind = "ref"
	AuditCodes   AuditValueKind = "codes"
)

type AuditValue struct {
	Kind    AuditValueKind `json:"kind"`
	Boolean *bool          `json:"boolean,omitempty"`
	Ref     *uuid.UUID     `json:"ref,omitempty"`
	Codes   []string       `json:"codes,omitempty"`
}
type AuditChange struct {
	Field  string      `json:"field"`
	Before *AuditValue `json:"before,omitempty"`
	After  *AuditValue `json:"after,omitempty"`
}
type AuditPosition struct {
	RecordedAt time.Time `json:"recorded_at"`
	SourceKey  string    `json:"source"`
	EventRef   uuid.UUID `json:"event_ref"`
}

// AuditRecord is sanitized by its source owner. It contains no reason, label,
// payload or arbitrary text. An unknown outcome must remain unknown.
type AuditRecord struct {
	EventRef   uuid.UUID     `json:"event_ref"`
	EntityRef  uuid.UUID     `json:"entity_ref"`
	ActorRef   uuid.UUID     `json:"actor_ref"`
	OccurredAt time.Time     `json:"occurred_at"`
	RecordedAt time.Time     `json:"recorded_at"`
	Department string        `json:"department,omitempty"`
	Action     string        `json:"action"`
	Outcome    string        `json:"outcome"`
	Changes    []AuditChange `json:"changes"`
}

// AuditReadQuery is created only after authorization. Time filters refer to
// recorded_at in [From,To), with a fixed AsOf upper watermark across pages.
// Return at most Limit rows, strictly descending by (recorded_at,source,id).
type AuditReadQuery struct {
	From, To, AsOf             time.Time
	ActorRef                   uuid.UUID
	Department, Module, Action string
	Limit                      int
	Before                     *AuditPosition
}
type AuditSource struct {
	Key, Module string
	Fields      map[string]AuditValueKind
	Read        func(context.Context, uuid.UUID, AuditReadQuery) ([]AuditRecord, error)
}

// AuditPurposeCode is a closed purpose allowlist. The query endpoint emits only
// AuditPurposeView, chosen by the server; clients cannot submit free-text purpose.
type AuditPurposeCode string

const AuditPurposeView AuditPurposeCode = "audit_view"

// AuditReadReceipt records the viewing fact, never copies source audit content.
// RecordRead must durably persist it before any data is released. The application
// owner supplies that implementation; this port does not imply retention.
type AuditReadReceipt struct {
	RequestRef, TenantID, ActorRef uuid.UUID
	ReadAt                         time.Time
	FilterHash                     string
	// ActorRef above identifies the reader; FilterActorRef is the actor searched.
	// These normalized ref/code filters make the viewing record reconstructible.
	From, To, AsOf             time.Time
	FilterActorRef             uuid.UUID
	Department, Module, Action string
	Limit                      int
	PurposeCode                AuditPurposeCode
	Sources                    []string
	Returned                   []AuditPosition
}
type AuditAccess struct {
	// Authorize must validate the actual application Admin session. A realm role
	// or payload from another selected session must never serve as fallback.
	Authorize  func(context.Context) error
	RecordRead func(context.Context, AuditReadReceipt) error
}

var ErrAuditRegistry = errors.New("accounts: invalid audit source registration")
var ErrAuditRecord = errors.New("accounts: invalid audit source record")
var auditCode = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)

// AuditRegistry is an instance-local immutable snapshot. There is no global
// registration and no SQL table/schema name can come from an HTTP query.
type AuditRegistry struct{ sources []AuditSource }

func NewAuditRegistry(sources ...AuditSource) (*AuditRegistry, error) {
	if len(sources) == 0 || len(sources) > 32 {
		return nil, ErrAuditRegistry
	}
	r := &AuditRegistry{}
	seen := map[string]bool{}
	for _, s := range sources {
		if !auditCode.MatchString(s.Key) || !auditCode.MatchString(s.Module) || s.Read == nil || seen[s.Key] || len(s.Fields) > 32 {
			return nil, ErrAuditRegistry
		}
		seen[s.Key] = true
		for field, kind := range s.Fields {
			if !auditCode.MatchString(field) || (kind != AuditBoolean && kind != AuditRef && kind != AuditCodes) {
				return nil, ErrAuditRegistry
			}
		}
		s.Fields = maps.Clone(s.Fields)
		r.sources = append(r.sources, s)
	}
	slices.SortFunc(r.sources, func(a, b AuditSource) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	return r, nil
}
func (r *AuditRegistry) Sources() []AuditSource {
	if r == nil {
		return nil
	}
	out := slices.Clone(r.sources)
	for i := range out {
		out[i].Fields = maps.Clone(out[i].Fields)
	}
	return out
}

// ValidateRecord checks the registered typed allowlist. Source owners still
// decide which codes/fields are non-personal; values cannot be raw payloads.
func (r *AuditRegistry) ValidateRecord(source string, rec AuditRecord) error {
	if r == nil {
		return ErrAuditRecord
	}
	var fields map[string]AuditValueKind
	found := false
	for _, s := range r.sources {
		if s.Key == source {
			fields = s.Fields
			found = true
			break
		}
	}
	if !found || rec.EventRef == uuid.Nil || rec.EntityRef == uuid.Nil || rec.ActorRef == uuid.Nil || rec.OccurredAt.IsZero() || rec.RecordedAt.IsZero() || !auditCode.MatchString(rec.Action) || (rec.Department != "" && !auditCode.MatchString(rec.Department)) || len(rec.Changes) > 32 {
		return ErrAuditRecord
	}
	switch rec.Outcome {
	case "unknown", "succeeded", "denied", "failed":
	default:
		return ErrAuditRecord
	}
	seen := map[string]bool{}
	for _, c := range rec.Changes {
		kind, ok := fields[c.Field]
		if !ok || seen[c.Field] || (c.Before == nil && c.After == nil) {
			return ErrAuditRecord
		}
		seen[c.Field] = true
		for _, v := range []*AuditValue{c.Before, c.After} {
			if v == nil {
				continue
			}
			if v.Kind != kind {
				return ErrAuditRecord
			}
			switch kind {
			case AuditBoolean:
				if v.Boolean == nil || v.Ref != nil || v.Codes != nil {
					return ErrAuditRecord
				}
			case AuditRef:
				if v.Ref == nil || *v.Ref == uuid.Nil || v.Boolean != nil || v.Codes != nil {
					return ErrAuditRecord
				}
			case AuditCodes:
				if v.Boolean != nil || v.Ref != nil || len(v.Codes) > 64 {
					return ErrAuditRecord
				}
				for _, code := range v.Codes {
					if !auditCode.MatchString(code) {
						return ErrAuditRecord
					}
				}
			default:
				return ErrAuditRecord
			}
		}
	}
	return nil
}
