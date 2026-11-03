package port

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

// PeriodOperationRequest is the complete original native command. Persist it
// with the consumer intent before dispatch. Readers resubmit these arguments;
// a caller-supplied digest alone is never evidence of the original command.
type PeriodOperationRequest struct {
	OperationID uuid.UUID      `json:"operation_id"`
	Profile     string         `json:"profile"`
	PeriodID    uuid.UUID      `json:"period_id"`
	Action      string         `json:"action"`
	Expected    PeriodRevision `json:"expected"`
	DueAt       time.Time      `json:"due_at"`
	Reason      string         `json:"reason"`
}

const (
	PeriodOperationChangeDue = "change_due"
	PeriodOperationClose     = "close"
	PeriodOperationReopen    = "reopen"
	PeriodOperationCancel    = "cancel"
	PeriodOperationApplied   = "applied"
	PeriodOperationRejected  = "rejected"
	PeriodOperationUnknown   = "unknown"
)

var (
	ErrPeriodOperationUnavailable = problem.New(http.StatusServiceUnavailable, "forms.operation_receipt_unavailable", "Chưa nối policy receipt thao tác kỳ")
	ErrPeriodOperationConflict    = problem.New(http.StatusConflict, "forms.operation_receipt_conflict", "Operation đã gắn với yêu cầu khác")
)

// NormalizePeriodOperation defines the public canonical input contract. No
// actor/authority comes from the command; the supplier obtains both at runtime.
func NormalizePeriodOperation(in PeriodOperationRequest) (PeriodOperationRequest, error) {
	bad := func(field string) (PeriodOperationRequest, error) {
		return PeriodOperationRequest{}, problem.Validation.WithField(field, "giá trị không hợp lệ")
	}
	if in.OperationID == uuid.Nil {
		return bad("operation_id")
	}
	if in.PeriodID == uuid.Nil || in.Expected.TenantID == uuid.Nil || in.Expected.PeriodID != in.PeriodID || in.Expected.Profile != in.Profile || in.Expected.EventSeq < 1 || in.Expected.DueSeq < 0 {
		return bad("expected")
	}
	if len(in.Profile) == 0 || len(in.Profile) > 64 || strings.TrimSpace(in.Profile) != in.Profile || strings.ContainsRune(in.Profile, 0) {
		return bad("profile")
	}
	if len(in.Reason) > 4096 || !utf8.ValidString(in.Reason) || strings.ContainsRune(in.Reason, 0) {
		return bad("reason")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 500 {
		return bad("reason")
	}
	in.DueAt = in.DueAt.UTC()
	switch in.Action {
	case PeriodOperationChangeDue:
		if in.DueAt.IsZero() {
			return bad("due_at")
		}
	case PeriodOperationClose:
		if !in.DueAt.IsZero() || in.Reason != "" {
			return bad("action")
		}
	case PeriodOperationReopen, PeriodOperationCancel:
		if !in.DueAt.IsZero() || utf8.RuneCountInString(in.Reason) < 10 {
			return bad("reason")
		}
	default:
		return bad("action")
	}
	return in, nil
}

// PeriodOperationDigest is a comparison value, not a permission or receipt.
// The supplier independently normalizes and hashes the full command on every call.
func PeriodOperationDigest(in PeriodOperationRequest) (string, error) {
	normalized, err := NormalizePeriodOperation(in)
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

// PeriodOperationResult is captured inside the same native committing TX.
// Snapshot metadata identifies immutable close output; no snapshot bytes/PII are returned.
type PeriodOperationResult struct {
	Status       string    `json:"status"`
	DueAt        time.Time `json:"due_at"`
	SnapshotID   uuid.UUID `json:"snapshot_id"`
	SnapshotNo   int64     `json:"snapshot_no"`
	SnapshotHash string    `json:"snapshot_hash"`
}

// PeriodOperationReceipt only describes its own operation, even after a successor.
// Unknown is not evidence of rollback and never authorizes mutation retry.
type PeriodOperationReceipt struct {
	OperationID   uuid.UUID             `json:"operation_id"`
	Profile       string                `json:"profile"`
	PeriodID      uuid.UUID             `json:"period_id"`
	Action        string                `json:"action"`
	RequestDigest string                `json:"request_digest"`
	ActorRef      uuid.UUID             `json:"actor_ref"`
	AuthorityHash string                `json:"authority_hash"`
	Expected      PeriodRevision        `json:"expected"`
	Applied       PeriodRevision        `json:"applied"`
	Outcome       string                `json:"outcome"`
	RejectionCode string                `json:"rejection_code"`
	Result        PeriodOperationResult `json:"result"`
	RecordedAt    time.Time             `json:"recorded_at"`
}

// PeriodOperationPolicy is registered by the trusted application owner. It runs
// OUTSIDE Forms TX on execute AND read. Binding must identify current verified
// session/assignment/scope authority, changing on revocation; never copy request
// headers or a caller-asserted digest. A nil policy disables this capability.
type PeriodOperationPolicy func(context.Context, authz.Principal, PeriodOperationRequest) (PeriodOperationGrant, error)
type PeriodOperationGrant struct {
	Allowed bool
	Binding string
}

// PeriodOperations is additive: the existing ConditionalPeriodControl contract
// remains unchanged. Consumers must not resolve unknown without an authenticated
// receipt and their own durable intent transition.
type PeriodOperations interface {
	ExecutePeriodOperation(context.Context, PeriodOperationRequest) (PeriodOperationReceipt, error)
	ReadPeriodOperation(context.Context, PeriodOperationRequest) (PeriodOperationReceipt, error)
}

// ExecutePeriodOperation fails closed when a consumer has no receipt capability.
func ExecutePeriodOperation(ctx context.Context, capability PeriodOperations, in PeriodOperationRequest) (PeriodOperationReceipt, error) {
	if capability == nil {
		return PeriodOperationReceipt{}, ErrPeriodOperationUnavailable
	}
	return capability.ExecutePeriodOperation(ctx, in)
}
