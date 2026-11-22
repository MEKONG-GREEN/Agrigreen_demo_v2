package port

import (
	"context"

	"github.com/google/uuid"
)

// SupportAuditContext contains only trusted audit labels from a session owner.
// It cannot override the authenticated actor, tenant, realm roles or MFA.
type SupportAuditContext struct {
	ContextID, SessionID, OnBehalfOf uuid.UUID
	Audience, Department, Reason     string
}

// SessionPolicies are instance-scoped owner projections. Nil callbacks select
// the existing compatibility path during rollout; a configured callback error is final, never a fallback.
type SessionPolicies struct {
	SupportAudit func(context.Context) (SupportAuditContext, error) `json:"-"`
	// DefaultUnit reports handled for every selected owner, including invalid or
	// empty owner state. Unhandled is reserved for genuinely native requests.
	DefaultUnit func(context.Context) (unitRef uuid.UUID, handled bool, err error) `json:"-"`
	// RouteGate (KN-STANDALONE-1-ADMIN-TOOLS #264) is called by the session middleware for every request that carries a
	// resolved owner context, with the HTTP method and the route pattern (gin FullPath). A non-nil error (a problem) is
	// returned to the caller as is: the owner closes routes that are not part of its surface for its sessions only.
	// Requests without an owner context are never gated. Nil selects no gate.
	RouteGate func(method, route string) error `json:"-"`
}
