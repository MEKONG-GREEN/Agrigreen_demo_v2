package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PeriodRevision identifies the committed native control state. EventSeq and
// DueSeq are heads of the existing append-only ledgers, not caller lease numbers.
// Equal due/status after an ABA still has different heads. This is a precondition,
// never an authorization; tenant/profile/target policy remains mandatory.
type PeriodRevision struct {
	TenantID uuid.UUID `json:"tenant_id"`
	PeriodID uuid.UUID `json:"period_id"`
	Profile  string    `json:"profile"`
	EventSeq int64     `json:"event_seq"`
	DueSeq   int64     `json:"due_seq"`
}

// PeriodControlSnapshot reads progress and its revision under the same native
// period lock. A caller persists Revision with its durable intent BEFORE I/O.
type PeriodControlSnapshot struct {
	Progress PeriodProgress
	Revision PeriodRevision
}

// ConditionalPeriodControl rejects a stale native control operation atomically
// before writing. All four operations compare both ledger heads under the same
// period lock used by legacy control. There is no fallback to unconditional I/O.
//
// The caller must retain its original revision on retry. After conflict/uncertain
// outcome it reconciles only while it STILL owns its application intent; it must
// not refresh a revision after losing a lease. This port does not fence an
// application's intent until that application's consumer explicitly adopts it.
// Legacy PeriodControl remains compatible and its writes invalidate revisions.
// A successful mutation returns the heads read INSIDE its own committing TX,
// allowing Reopen -> ChangeDue chaining without refreshing a lost owner's token.
// On any error the returned revision is zero (including uncertain commit outcome).
type ConditionalPeriodControl interface {
	ControlSnapshot(context.Context, string, uuid.UUID) (PeriodControlSnapshot, error)
	ChangeDueCAS(context.Context, string, uuid.UUID, PeriodRevision, time.Time, string, string) (PeriodRevision, error)
	ClosePeriodCAS(context.Context, string, uuid.UUID, PeriodRevision, string) (PeriodRevision, error)
	CancelPeriodCAS(context.Context, string, uuid.UUID, PeriodRevision, string, string) (PeriodRevision, error)
	ReopenPeriodCAS(context.Context, string, uuid.UUID, PeriodRevision, string, string) (PeriodRevision, error)
}
