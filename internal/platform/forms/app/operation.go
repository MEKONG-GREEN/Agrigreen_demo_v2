package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
)

func operationAllowed(ctx context.Context, prof port.Profile, op port.Operation) (port.OperationGrant, error) {
	if prof.AuthorizeOperation == nil {
		return port.OperationGrant{}, nil
	}
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return port.OperationGrant{}, nil
	}
	op.Profile = prof.Name
	grant, err := prof.AuthorizeOperation(ctx, principal, op)
	if err != nil {
		return port.OperationGrant{}, err
	}
	if grant.Allowed && (strings.TrimSpace(grant.Source) == "" || len(grant.Source) > 200 || strings.ContainsRune(grant.Source, 0)) {
		return port.OperationGrant{}, problem.Forbidden.WithDetail("Thiếu tham chiếu nguồn cấp quyền")
	}
	return grant, nil
}
func needOperation(ctx context.Context, prof port.Profile, a actor, level, what string, op port.Operation) (port.OperationGrant, error) {
	original := a.need(level, what)
	if original == nil {
		return port.OperationGrant{}, nil
	}
	grant, err := operationAllowed(ctx, prof, op)
	if err != nil {
		return port.OperationGrant{}, err
	}
	if grant.Allowed {
		return grant, nil
	}
	return port.OperationGrant{}, original
}

// Called before the write transaction. Metadata callbacks may perform their own
// reads; they must never run while a native transaction is open.
func (s *Service) reviewOperation(ctx context.Context, prof port.Profile, a actor, id uuid.UUID, decision, note, key string) (port.Operation, error) {
	hash, err := port.OperationHash(struct{ Decision, Note string }{decision, note})
	if err != nil {
		return port.Operation{}, err
	}
	op := port.Operation{Action: port.OperationReview, Profile: prof.Name, SubmissionID: id, PayloadHash: hash, IdemKey: key}
	if a.level != prof.Reviewer() && prof.AuthorizeOperation != nil {
		err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
			sb, e := store.SubmissionByID(ctx, tx, id)
			if sb != nil {
				op.PeriodID = sb.PeriodID
			}
			return e
		})
	}
	return op, err
}

// A narrow grant replaces only the level requirement. Template, owner and target
// scope remain independently enforced in the native write transaction.
func (a actor) canControlGranted(level string, p store.Period, spec domain.Spec, grant bool) bool {
	if a.canClose(level, p, spec, nil) {
		return true
	}
	if !grant || !a.allowsTemplate(p.TemplateCode) || (!a.scope.All && !a.owns(p)) {
		return false
	}
	for _, unit := range p.Units {
		if !a.allowsUnit(spec, p.Units, unit) {
			return false
		}
	}
	return true
}
