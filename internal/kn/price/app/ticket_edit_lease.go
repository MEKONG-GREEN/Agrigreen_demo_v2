package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// EditLeaseTTL is a technical timeout, not a business parameter screen. Each tab renews every 30 seconds.
const EditLeaseTTL = 2 * time.Minute

type EditLease struct {
	Held       bool
	Mine       bool
	HolderName string
	Expires    *time.Time
	Now        time.Time
	Handle     uuid.UUID // only returned for the caller's successful acquire/renew
	holder     uuid.UUID
}

func editLeaseView(l *store.EditLease, c caller) EditLease {
	if l == nil {
		return EditLease{}
	}
	return EditLease{Held: true, Mine: l.Holder == c.ref, Expires: &l.Expires, holder: l.Holder}
}
func (s *TicketService) fillLeaseName(ctx context.Context, l *EditLease) {
	if l == nil || !l.Held {
		return
	}
	if names, ok := s.Points.Officers.(port.EditHolderNames); ok {
		l.HolderName, _ = names.EditHolderName(ctx, l.holder)
	}
}

// editTicket locks the same point as Correct/Redate/Delete, then reads current authorization data.
func (s *TicketService) editTicket(ctx context.Context, tx pgx.Tx, prof port.Profile, c caller, id uuid.UUID) (store.Ticket, *sheetCtx, error) {
	t, err := ticketByID(ctx, tx, id)
	if err != nil {
		return t, nil, err
	}
	if err = store.LockPointTickets(ctx, tx, c.tenant, t.PointCode); err != nil {
		return t, nil, err
	}
	t, err = ticketByID(ctx, tx, id)
	if err != nil {
		return t, nil, err
	}
	sc, err := s.newSheetCtx(ctx, tx, prof, c)
	if err != nil {
		return t, nil, err
	}
	p, ok, _ := sc.pointAt(t.PointCode, t.Day)
	if !ok {
		return t, sc, port.ErrPointNotFound
	}
	if err = canView(c, p); err != nil {
		return t, sc, err
	}
	return t, sc, nil
}
func (s *TicketService) editGate(ctx context.Context, tx pgx.Tx, sc *sheetCtx, t store.Ticket) error {
	if !reviewer(sc.c) || sc.c.readOnly {
		return errNotReviewer
	}
	if !domain.Live(t.Status) {
		return problem.Conflict.WithDetail("phiếu đã xóa")
	}
	p, _, _ := sc.pointAt(t.PointCode, t.Day)
	_, err := s.reviewGate(ctx, tx, sc, p, t.Day)
	return err
}

// EditTicketLease atomically reads/acquires/renews/releases a ticket lease. Read and release
// need ticket visibility; acquire/renew additionally recheck the current editing permission and board.
// A denied acquisition returns the scoped holder, never their edit handle.
func (s *TicketService) EditTicketLease(ctx context.Context, profile string, id, handle uuid.UUID, action string) (EditLease, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return EditLease{}, err
	}
	if action != "read" && handle == uuid.Nil {
		return EditLease{}, problem.Validation.WithField("handle_id", "UUID của phiên sửa là bắt buộc")
	}
	var out EditLease
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		t, sc, err := s.editTicket(ctx, tx, prof, c, id)
		if err != nil {
			return err
		}
		if action == "acquire" || action == "renew" {
			if err = s.editGate(ctx, tx, sc, t); err != nil {
				return err
			}
		}
		now := s.Clock.Now() // after any wait for the board/permission gate
		current, err := store.LoadEditLease(ctx, tx, id, now)
		if err != nil {
			return err
		}
		switch action {
		case "read":
		case "acquire":
			if current != nil && current.Holder != c.ref {
				out = editLeaseView(current, c)
				return nil
			}
			if err = store.ClaimEditLease(ctx, tx, c.tenant, id, c.ref, handle, now, now.Add(EditLeaseTTL)); err != nil {
				return err
			}
			out.Handle = handle
		case "renew":
			ok, err := store.HasEditHandle(ctx, tx, id, c.ref, handle, now)
			if err != nil {
				return err
			}
			if !ok {
				return port.ErrEditLeaseLost
			}
			if err = store.RenewEditHandle(ctx, tx, id, handle, now.Add(EditLeaseTTL)); err != nil {
				return err
			}
			out.Handle = handle
		case "release":
			if err = store.ClearEditHandle(ctx, tx, id, c.ref, handle); err != nil {
				return err
			}
		default:
			return problem.Validation.WithField("action", "thao tác phiên sửa không hợp lệ")
		}
		current, err = store.LoadEditLease(ctx, tx, id, now)
		if err != nil {
			return err
		}
		ownedHandle := out.Handle
		out = editLeaseView(current, c)
		out.Handle = ownedHandle
		return nil
	})
	if err == nil {
		s.fillLeaseName(ctx, &out)
	}
	out.Now = s.Clock.Now()
	return out, err
}
func (s *TicketService) requireEditLease(ctx context.Context, tx pgx.Tx, sc *sheetCtx, id, handle uuid.UUID) error {
	if sc.c.readOnly {
		return errNotReviewer
	}
	ok, err := store.HasEditHandle(ctx, tx, id, sc.c.ref, handle, s.Clock.Now())
	if err != nil {
		return err
	}
	if !ok {
		return port.ErrEditLeaseLost
	}
	return nil
}
