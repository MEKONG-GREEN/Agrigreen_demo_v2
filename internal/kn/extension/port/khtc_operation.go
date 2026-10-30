package port

import (
	"context"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"strconv"
)

// KHTCOperationSource verifies immutable KN ledger metadata. The private context
// link cannot be supplied by an HTTP header and is insufficient without this read.
type KHTCOperationSource interface {
	VerifyKHTCOperation(context.Context, authz.Principal, SessionContext, KHTCOperationProof, forms.Operation) (bool, error)
}
type KHTCOperationProof struct {
	LeaseGeneration int64
	IntentID        uuid.UUID
	KNPeriodID      uuid.UUID
	SessionID       uuid.UUID
	Expected        forms.Operation
}
type khtcOperationKey struct{}
type khtcOperationLink struct {
	Source KHTCOperationSource
	Proof  KHTCOperationProof
}

func WithKHTCOperation(ctx context.Context, source KHTCOperationSource, proof KHTCOperationProof) context.Context {
	return context.WithValue(ctx, khtcOperationKey{}, khtcOperationLink{source, proof})
}
func AuthorizeKHTCOperation(ctx context.Context, p authz.Principal, op forms.Operation) (forms.OperationGrant, error) {
	link, ok := ctx.Value(khtcOperationKey{}).(khtcOperationLink)
	if !ok || link.Source == nil {
		return forms.OperationGrant{}, nil
	}
	actual, k, err := RequireSession(ctx)
	if err != nil {
		return forms.OperationGrant{}, err
	}
	if p.ActorRef != actual.ActorRef || p.TenantID != actual.TenantID || p.MFA != actual.MFA || op.Profile != "kn_khtc" || k.Role != SessionRoleAdmin || k.ReadOnly() || !actual.MFA {
		return forms.OperationGrant{}, nil
	}
	if link.Proof.SessionID != k.ID || link.Proof.Expected != op {
		return forms.OperationGrant{}, nil
	}
	switch op.Action {
	case forms.OperationPublishBatch, forms.OperationReview, forms.OperationClose, forms.OperationCancel, forms.OperationChangeDue:
	default:
		return forms.OperationGrant{}, nil
	}
	allowed, err := link.Source.VerifyKHTCOperation(ctx, p, k, link.Proof, op)
	if err != nil || !allowed {
		return forms.OperationGrant{}, err
	}
	source := "kn:khtc:adhoc-period:" + link.Proof.KNPeriodID.String()
	if op.Action == forms.OperationPublishBatch {
		source = "kn:khtc:adhoc-open:" + link.Proof.IntentID.String() + ":g" + strconv.FormatInt(link.Proof.LeaseGeneration, 10)
	}
	return forms.OperationGrant{Allowed: true, Source: source}, nil
}
