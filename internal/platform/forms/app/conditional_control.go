package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
)

var _ port.ConditionalPeriodControl = Control{}

func (c Control) ControlSnapshot(ctx context.Context, profile string, id uuid.UUID) (port.PeriodControlSnapshot, error) {
	v, revision, err := c.Svc.periodState(ctx, profile, id, true)
	if err != nil {
		return port.PeriodControlSnapshot{}, err
	}
	return port.PeriodControlSnapshot{Progress: progressOf(&v), Revision: revision}, nil
}

func (c Control) ChangeDueCAS(ctx context.Context, profile string, id uuid.UUID, expect port.PeriodRevision, due time.Time, reason, idem string) (port.PeriodRevision, error) {
	guard := controlCAS{expected: expect}
	err := c.changeDue(ctx, profile, id, due, reason, idem, &guard)
	return guard.result(err)
}

func (c Control) ClosePeriodCAS(ctx context.Context, profile string, id uuid.UUID, expect port.PeriodRevision, idem string) (port.PeriodRevision, error) {
	guard := controlCAS{expected: expect}
	_, err := c.Svc.closeWithRevision(ctx, profile, id, idem, &guard)
	return guard.result(err)
}

func (c Control) CancelPeriodCAS(ctx context.Context, profile string, id uuid.UUID, expect port.PeriodRevision, reason, idem string) (port.PeriodRevision, error) {
	guard := controlCAS{expected: expect}
	_, err := c.Svc.cancelWithRevision(ctx, profile, id, reason, idem, &guard)
	return guard.result(err)
}

func (c Control) ReopenPeriodCAS(ctx context.Context, profile string, id uuid.UUID, expect port.PeriodRevision, reason, idem string) (port.PeriodRevision, error) {
	guard := controlCAS{expected: expect}
	_, err := c.Svc.reopenWithRevision(ctx, profile, id, ReopenInput{Reason: reason}, idem, &guard)
	return guard.result(err)
}

type controlCAS struct {
	expected, applied port.PeriodRevision
	receipt           *periodOperation
}

func (g *controlCAS) result(err error) (port.PeriodRevision, error) {
	if err != nil {
		return port.PeriodRevision{}, err
	}
	return g.applied, nil
}

// The caller has already locked/loaded the period and checked authorization.
// Comparison and the native ledger/snapshot/outbox writes share ONE transaction.
// No application callback or cross-module I/O occurs between this guard and commit.
func checkControlRevision(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, profile string, id uuid.UUID, guard *controlCAS) error {
	if guard == nil { // Existing unconditional API; never used by the CAS port.
		return nil
	}
	if guard.receipt != nil {
		if err := guard.receipt.before(ctx, tx); err != nil {
			return err
		}
	}
	expect := guard.expected
	if expect.TenantID != tenantID || expect.PeriodID != id || expect.Profile != profile || expect.EventSeq < 1 || expect.DueSeq < 0 {
		return rejectControl(ctx, tx, guard, port.ErrPeriodConflict)
	}
	eventSeq, dueSeq, err := store.ControlHeads(ctx, tx, id)
	if err != nil {
		return err
	}
	if expect.EventSeq != eventSeq || expect.DueSeq != dueSeq {
		return rejectControl(ctx, tx, guard, port.ErrPeriodConflict)
	}
	return nil
}

// Called after the ledger/snapshot/outbox writes, still holding the same lock and
// transaction. A failed read rolls back; any failed/uncertain commit exposes no
// revision, and the caller must reconcile only while it still owns its intent.
func appliedControlRevision(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, profile string, id uuid.UUID, guard *controlCAS) error {
	if guard == nil {
		return nil
	}
	events, due, err := store.ControlHeads(ctx, tx, id)
	if err != nil {
		return err
	}
	guard.applied = port.PeriodRevision{TenantID: tenantID, Profile: profile, PeriodID: id, EventSeq: events, DueSeq: due}
	return appliedOperation(ctx, tx, guard)
}
