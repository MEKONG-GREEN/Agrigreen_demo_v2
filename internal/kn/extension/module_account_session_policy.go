package extension

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

// AccountSessionPolicies keeps KN request policy at the owner while accounts
// consumes narrow audit/default-unit projections. Identity is never projected.
func AccountSessionPolicies() accountsport.SessionPolicies {
	return accountsport.SessionPolicies{SupportAudit: accountSupportAudit, DefaultUnit: accountDefaultUnit}
}

func accountSupportAudit(ctx context.Context) (accountsport.SupportAuditContext, error) {
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return accountsport.SupportAuditContext{}, err
	}
	if k.Support == nil {
		return accountsport.SupportAuditContext{}, problem.Forbidden
	}
	return accountsport.SupportAuditContext{ContextID: k.ID, SessionID: k.Support.SessionID, OnBehalfOf: k.Support.OnBehalfOf,
		Audience: k.Role, Department: k.Department, Reason: k.Support.Reason}, nil
}

func accountDefaultUnit(ctx context.Context) (uuid.UUID, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return uuid.Nil, false, problem.Unauthenticated
	}
	// Legacy presence only detects an incomplete binding and denies fallback. It
	// supplies no authority, role, unit, or other projected value.
	if p.SelectedSession == nil && p.KN == nil {
		return uuid.Nil, false, nil
	}
	_, k, err := port.RequireSession(ctx)
	if err != nil {
		return uuid.Nil, true, err
	}
	return k.UnitRef, true, nil
}
