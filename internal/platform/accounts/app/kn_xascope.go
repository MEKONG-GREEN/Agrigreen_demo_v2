package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ port.XaScopes = (*Service)(nil)

// XaScopes thực hiện port.XaScopes (W11.46 — lịch trực KSGM BM-03). Tenant truyền tường minh.
func (s *Service) XaScopes(ctx context.Context, tenantID uuid.UUID, dept string) ([]port.XaScope, error) {
	var out []port.XaScope
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.XaScopesAt(ctx, tx, dept, s.now())
		return e
	})
	return out, err
}
