package port

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

// PublishOperationRequest is the complete first-dispatch command. Consumers
// persist it and their dispatch state before calling Execute; a digest is not a grant.
type PublishOperationRequest struct {
	OperationID uuid.UUID  `json:"operation_id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Input       BatchInput `json:"input"`
}

const (
	PublishOperationApplied    = "applied"
	PublishOperationRejected   = "rejected"
	PublishOperationUnknown    = "unknown"
	MaxPublishOperationPeriods = 366
	MaxPublishOperationUnits   = 2000
)

var (
	ErrPublishOperationUnavailable = problem.New(http.StatusServiceUnavailable, "forms.publish_receipt_unavailable", "Chưa nối policy receipt phát hành kỳ")
	ErrPublishOperationConflict    = problem.New(http.StatusConflict, "forms.publish_receipt_conflict", "Operation phát hành đã gắn với yêu cầu khác")
)

// NormalizePublishOperation bounds work before hashing, owns all mutable input,
// preserves collection order and nil-vs-zero MaxSubmissions, and uses SQL date /
// timestamptz precision. Empty kind/key remain explicit inputs, not live defaults.
func NormalizePublishOperation(in PublishOperationRequest) (PublishOperationRequest, error) {
	bad := func(field string) (PublishOperationRequest, error) {
		return PublishOperationRequest{}, problem.Validation.WithField(field, "giá trị không hợp lệ")
	}
	text := func(value string, limit int) bool {
		return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
	}
	if in.OperationID == uuid.Nil {
		return bad("operation_id")
	}
	if in.TenantID == uuid.Nil {
		return bad("tenant_id")
	}
	b := in.Input
	if !text(b.Profile, 64) || b.Profile == "" || strings.TrimSpace(b.Profile) != b.Profile {
		return bad("profile")
	}
	if b.TemplateID == uuid.Nil {
		return bad("template_id")
	}
	if !text(b.UnitKind, 32) || strings.TrimSpace(b.UnitKind) != b.UnitKind {
		return bad("unit_kind")
	}
	if len(b.Periods) < 1 || len(b.Periods) > MaxPublishOperationPeriods {
		return bad("periods")
	}
	if len(b.Units) < 1 || len(b.Units) > MaxPublishOperationUnits {
		return bad("units")
	}
	if len(b.UnitParents) != 0 && len(b.UnitParents) != len(b.Units) {
		return bad("unit_parents")
	}
	b.Units = slices.Clone(b.Units)
	b.UnitParents = append([]string{}, b.UnitParents...)
	b.Periods = slices.Clone(b.Periods)
	seenUnits := map[string]bool{}
	for _, unit := range b.Units {
		if !text(unit, 40) || unit == "" || seenUnits[unit] {
			return bad("units")
		}
		seenUnits[unit] = true
	}
	for _, parent := range b.UnitParents {
		if !text(parent, 40) || parent == "" {
			return bad("unit_parents")
		}
	}
	if b.MaxSubmissions != nil {
		value := *b.MaxSubmissions
		if value < 0 || value > 20 {
			return bad("max_submissions")
		}
		b.MaxSubmissions = &value
	}
	date := func(value time.Time) (time.Time, bool) {
		value = value.UTC()
		if value.Year() < 1 || value.Year() > 9999 {
			return time.Time{}, false
		}
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC), true
	}
	seenKeys := map[string]bool{}
	for i, period := range b.Periods {
		if !text(period.Label, 800) || !text(period.Kind, 32) || !text(period.Key, 80) || !text(period.Rule, 800) {
			return bad("periods")
		}
		period.Label = strings.TrimSpace(period.Label)
		period.Key = strings.TrimSpace(period.Key)
		period.Rule = strings.TrimSpace(period.Rule)
		if utf8.RuneCountInString(period.Label) < 1 || utf8.RuneCountInString(period.Label) > 200 || len(period.Key) > 20 || utf8.RuneCountInString(period.Rule) > 200 {
			return bad("periods")
		}
		if period.DueAt.IsZero() || period.DueAt.Year() < 1 || period.DueAt.Year() > 9999 {
			return bad("due_at")
		}
		period.DueAt = period.DueAt.UTC().Truncate(time.Microsecond)
		var ok bool
		if period.From, ok = date(period.From); !ok {
			return bad("from")
		}
		if period.To, ok = date(period.To); !ok || period.To.Before(period.From) {
			return bad("to")
		}
		key := period.Key
		if key == "" {
			key = period.From.Format(time.DateOnly)
		}
		if seenKeys[key] {
			return bad("periods")
		}
		seenKeys[key] = true
		b.Periods[i] = period
	}
	in.Input = b
	encoded, err := json.Marshal(in)
	if err != nil || len(encoded) > 1<<20 {
		return bad("command")
	}
	return in, nil
}

func PublishOperationDigest(in PublishOperationRequest) (string, error) {
	normalized, err := NormalizePublishOperation(in)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// PublishedCreation is evidence from this operation's native INSERTs, never a
// current head. EventRecordedAt binds the partitioned event identity exactly.
type PublishedCreation struct {
	ID              uuid.UUID      `json:"id"`
	Key             string         `json:"key"`
	Label           string         `json:"label"`
	DueAt           time.Time      `json:"due_at"`
	Applied         PeriodRevision `json:"applied"`
	EventID         uuid.UUID      `json:"event_id"`
	EventRecordedAt time.Time      `json:"event_recorded_at"`
}
type PublishOperationReceipt struct {
	OperationID   uuid.UUID           `json:"operation_id"`
	TenantID      uuid.UUID           `json:"tenant_id"`
	Profile       string              `json:"profile"`
	RequestDigest string              `json:"request_digest"`
	ActorRef      uuid.UUID           `json:"actor_ref"`
	AuthorityHash string              `json:"authority_hash"`
	Outcome       string              `json:"outcome"`
	RejectionCode string              `json:"rejection_code"`
	Items         []PublishedCreation `json:"items"`
	RecordedAt    time.Time           `json:"recorded_at"`
}

// PublishOperationPolicy runs outside Forms TX on BOTH Execute and Read. The
// trusted owner binds current assignment/scope/revocation; no default allow.
type PublishOperationPolicy func(context.Context, authz.Principal, PublishOperationRequest) (PublishOperationGrant, error)
type PublishOperationGrant struct {
	Allowed bool
	Binding string
}

// PublishOperations is additive. Execute is a durable consumer's FIRST dispatch,
// not an unknown-outcome retry: after lost ACK use Read only. A fully rolled-back
// TX leaves no supplier first-dispatch history. Missing/purged evidence is opaque
// unknown, never proof of rollback, fresh dispatch permission or a current token.
type PublishOperations interface {
	ExecutePublishOperation(context.Context, PublishOperationRequest) (PublishOperationReceipt, error)
	ReadPublishOperation(context.Context, PublishOperationRequest) (PublishOperationReceipt, error)
}

func ExecutePublishOperation(ctx context.Context, capability PublishOperations, in PublishOperationRequest) (PublishOperationReceipt, error) {
	if capability == nil {
		return PublishOperationReceipt{Outcome: PublishOperationUnknown}, ErrPublishOperationUnavailable
	}
	return capability.ExecutePublishOperation(ctx, in)
}
