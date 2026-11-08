package port

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// ReviewOperationPolicy is installed by the trusted application owner before
// serving. It must check current assignment, profile, department/unit, read-only,
// MFA and revocation for execute AND read, outside all native DB transactions.
// Binding versions current authority not fully represented by SelectedSession.
// Nil disables this additive capability; Entry compatibility is unchanged.
type ReviewOperationPolicy func(context.Context, authz.Principal, ReviewOperationRequest) (ReviewOperationGrant, error)
type ReviewOperationGrant struct {
	Allowed bool
	Binding string
}

// EntryOperations is additive. Persist the complete original normalized intent
// before execute; preserve it for reads/retries. Unknown never licenses a new
// mutation or legacy Entry.Review fallback. Selected application context binding
// cannot distinguish separate OIDC logins without unique token metadata (#259).
type EntryOperations interface {
	ExecuteReviewOperation(context.Context, ReviewOperationRequest) (ReviewOperationReceipt, error)
	ReadReviewOperation(context.Context, ReviewOperationRequest) (ReviewOperationReceipt, error)
}
