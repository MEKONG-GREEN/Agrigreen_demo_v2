package app

import (
	"context"
	period "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BeginClose211 serializes with BeginCancel under the report-owned snapshot lock.
// Unlike cancellation, Final is valid for Close. The reservation never expires.
func (s *Service) BeginClose211(ctx context.Context, id, op, event uuid.UUID) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, id); err != nil {
			return err
		}
		r, err := store.Reservation211(ctx, tx, op)
		if err != nil {
			return err
		}
		if r != nil {
			if r.Period == id && r.Event == event && r.Actor == p.ActorRef && r.Action == "reserved" {
				return nil
			}
			return period.ErrCancelling
		}
		pending, err := store.CancelPending(ctx, tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if pending {
			return period.ErrCancelling
		}
		intent, err := ps.CloseByOperation211(ctx, tx, op)
		if err != nil {
			return err
		}
		if intent == nil || intent.PeriodID != id || intent.EventID != event || intent.Actor != p.ActorRef || intent.Phase != "prepared" {
			return period.ErrCancelling
		}
		return store.ReserveEvent211(ctx, tx, p.TenantID, store.CloseReservation211{Operation: op, Period: id, Event: event, Actor: p.ActorRef, Action: "reserved"}, s.now())
	})
}

// FinishClose211 commits only extension-owned writes together. The callback must
// never invoke a native port; its owned event/outcome and reservation are atomic.
func (s *Service) FinishClose211(ctx context.Context, id, op, event uuid.UUID, fn func(pgx.Tx) error) error {
	return s.finishClose211(ctx, id, op, event, "completed", fn)
}
func (s *Service) RejectClose211(ctx context.Context, id, op, event uuid.UUID, fn func(pgx.Tx) error) error {
	return s.finishClose211(ctx, id, op, event, "released", fn)
}
func (s *Service) finishClose211(ctx context.Context, id, op, event uuid.UUID, action string, fn func(pgx.Tx) error) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, id); err != nil {
			return err
		}
		r, err := store.Reservation211(ctx, tx, op)
		if err != nil {
			return err
		}
		if r == nil {
			if action == "released" {
				return fn(tx)
			}
			return period.ErrCancelling
		}
		if r.Period != id || r.Event != event || r.Actor != p.ActorRef || r.Action != "reserved" {
			return period.ErrCancelling
		}
		if err = fn(tx); err != nil {
			return err
		}
		r.Action = action
		return store.ReserveEvent211(ctx, tx, p.TenantID, *r, s.now())
	})
}
