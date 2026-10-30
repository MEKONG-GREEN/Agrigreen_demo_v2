package port

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

// Operation identifies a specific application-owned invocation. The application
// must verify durable metadata; a client-supplied kind is never an authorization.
type Operation struct {
	Action       string
	Profile      string
	PeriodID     uuid.UUID
	SubmissionID uuid.UUID
	PayloadHash  string
	IdemKey      string
}

const (
	OperationPublishBatch = "publish_batch"
	OperationReview       = "review"
	OperationClose        = "close"
	OperationCancel       = "cancel"
	OperationChangeDue    = "change_due"
)

type OperationGrant struct {
	Allowed bool
	Source  string
}

// Source is a bounded audit reference returned only after canonical metadata verification.
type OperationAuthorizer func(context.Context, authz.Principal, Operation) (OperationGrant, error)

// OperationHash binds normalized port arguments without changing native identities.
func OperationHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// DurablePublisher is an optional receipt-preserving port for a caller that has a
// durable intent. It grants no extra rights and is not exposed by native HTTP.
type DurablePublisher interface {
	Publisher
	PublishBatchOnce(context.Context, BatchInput, string) ([]PublishedPeriod, error)
}
