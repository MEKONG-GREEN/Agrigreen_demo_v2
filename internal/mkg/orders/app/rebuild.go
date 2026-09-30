package app

import (
	"context"

	"github.com/google/uuid"
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
	return s.RebuildTenant(ctx, p.TenantID)
}

// RebuildTenant dựng lại bảng chiếu của tenant tid từ sổ — tenant TƯỜNG MINH, không kiểm vai: chỉ cho công cụ
// vận hành chạy trên BẢN SAO database (diễn tập R-22, cmd/rebuilddrill). API dùng RebuildProjections (có vai).
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	var out map[string]int64
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.Rebuild(ctx, tx, tid)
		return e
	})
	return out, err
}
