package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// These server-side query errors do not introduce an HTTP contract.
var (
	ErrScopedDutyQuery  = errors.New("accounts: phạm vi người nhận không hợp lệ")
	ErrScopedDutyTenant = errors.New("accounts: tenant truy vấn người nhận không khớp ngữ cảnh")
)

// ScopedDutyHolders reads current duty holders at one issuer node for trusted jobs.
// Role, department, duty and effective node must belong to the same assignment.
// A nil Principal is allowed; existing identity and tenant contexts must match.
// This port neither grants delegated powers nor changes user authorization.
type ScopedDutyHolders interface {
	ScopedDutyHolders(ctx context.Context, tenantID, issuerNode uuid.UUID, role, department, duty string) ([]uuid.UUID, error)
}
