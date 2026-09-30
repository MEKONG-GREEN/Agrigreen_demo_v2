package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// RebuildProjections dựng lại mọi bảng chiếu của tenant thẳng từ sổ trong một giao dịch (R-22; diễn tập
// dựng lại của cổng G4). Vai sales_approver (bề mặt quản trị, MFA).
func (s *Service) RebuildProjections(ctx context.Context) (map[string]int64, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return nil, err
	}
	var out map[string]int64
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.Rebuild(ctx, tx, p.TenantID)
		return e
	})
	return out, err
}
