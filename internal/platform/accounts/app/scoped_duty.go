package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ScopedDutyReader supplies a server-side recipient query without minting identity.
type ScopedDutyReader struct{ Svc *Service }

var scopedDutyModule = db.Module("pf/accounts")
var _ port.ScopedDutyHolders = ScopedDutyReader{}

func (r ScopedDutyReader) ScopedDutyHolders(ctx context.Context, tid, issuer uuid.UUID, role, dept, duty string) ([]uuid.UUID, error) {
	if tid == uuid.Nil {
		return nil, tenant.ErrMissing
	}
	if p, ok := authz.FromContext(ctx); ok && p.TenantID != tid {
		return nil, port.ErrScopedDutyTenant
	}
	if contextual, err := tenant.FromContext(ctx); err == nil && contextual != tid {
		return nil, port.ErrScopedDutyTenant
	} else if err != nil && !errors.Is(err, tenant.ErrMissing) {
		return nil, err
	}
	if issuer == uuid.Nil || strings.TrimSpace(role) == "" || strings.TrimSpace(dept) == "" || strings.TrimSpace(duty) == "" {
		return nil, port.ErrScopedDutyQuery
	}
	if r.Svc.DB == nil {
		return nil, ErrDependency
	}
	at := r.Svc.now()
	var out []uuid.UUID
	err := tenant.WithTenantTxOptions(ctx, db.ForModule(r.Svc.DB, scopedDutyModule), tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = store.ScopedDutyHoldersAt(ctx, tx, issuer, role, dept, duty, at)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
