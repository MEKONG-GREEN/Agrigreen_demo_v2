package port

import (
	"context"

	"github.com/google/uuid"
)

// UnitManagers reads, for trusted notification jobs (NOTIFY-HCTH #48), the current
// leaders of internal units at one owning node. A leader is a province-level
// assignment whose management level or group grants leader permissions under the
// attribute catalog, and which is not a view-only group. The unit of an assignment
// is its internal-unit attribute, or each of its departments when that attribute is
// empty (the session's own fallback). Validity, revocation and UnitRef→OrgRef
// precedence follow ScopedDutyHolders. A nil Principal is allowed; existing
// identity and tenant contexts must match tenantID. Only account references are
// returned (sorted, distinct per unit); units without leaders are absent.
type UnitManagers interface {
	UnitManagers(ctx context.Context, tenantID, nodeRef uuid.UUID, units []string) (map[string][]uuid.UUID, error)
}
