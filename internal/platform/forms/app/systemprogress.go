package app

import (
	"context"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SystemReader supplies progress to trusted jobs using the same state projection
// as Control.Progress. It neither resolves a user role nor creates an identity.
type SystemReader struct{ Svc *Service }

var _ port.SystemProgressReader = SystemReader{}

func (r SystemReader) SystemProgress(ctx context.Context, tid uuid.UUID, profile string, id uuid.UUID) (port.SystemPeriodProgress, error) {
	if tid == uuid.Nil {
		return port.SystemPeriodProgress{}, tenant.ErrMissing
	}
	if p, ok := authz.FromContext(ctx); ok && p.TenantID != tid {
		return port.SystemPeriodProgress{}, port.ErrPeriodNotFound
	}
	if contextual, err := tenant.FromContext(ctx); err == nil && contextual != tid {
		return port.SystemPeriodProgress{}, port.ErrPeriodNotFound
	} else if err != nil && !errors.Is(err, tenant.ErrMissing) {
		return port.SystemPeriodProgress{}, err
	}
	if _, ok := r.Svc.profiles[profile]; !ok {
		return port.SystemPeriodProgress{}, port.ErrProfileNotFound
	}
	if id == uuid.Nil {
		return port.SystemPeriodProgress{}, port.ErrPeriodNotFound
	}
	if r.Svc.DB == nil {
		return port.SystemPeriodProgress{}, errNoPool
	}
	var out port.SystemPeriodProgress
	err := tenant.WithTenantTxOptions(ctx, db.ForModule(r.Svc.DB, DBModule), tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		v, err := load(ctx, tx, id, r.Svc.Clock.Now().UTC())
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile {
			return port.ErrPeriodNotFound
		}
		out.Progress = progressOf(v)
		out.Progress.From, out.Progress.To = cloneProgressTime(out.Progress.From), cloneProgressTime(out.Progress.To)
		if v.Period.OwnerRef != nil {
			owner := *v.Period.OwnerRef
			out.OwnerRef = &owner
		}
		out.PublisherLevel = v.Period.PublisherLevel
		return nil
	})
	if err != nil {
		return port.SystemPeriodProgress{}, err
	}
	return out, nil
}

func cloneProgressTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	out := *t
	return &out
}
