package port

import (
	"context"
)

// SessionContext is the minimum authorization snapshot supplied by the session
// owner. It cannot override the authenticated actor, tenant, realm roles or MFA.
type SessionContext struct {
	Role     string
	ReadOnly bool
	Duties   []string
}

// SessionResolver reads a server-bound owner session. A configured resolver must
// return an error for a missing or invalid binding; callers never fall back.
type SessionResolver func(context.Context) (SessionContext, error)
