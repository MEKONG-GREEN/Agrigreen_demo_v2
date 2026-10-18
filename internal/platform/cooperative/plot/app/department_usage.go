package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DepartmentPlots đếm thửa (mọi trạng thái) của phòng ban cho party/port.DepartmentUsage (W11.02, FN-AD-04.03).
// X-12: thửa không mang phòng ban (có trước v7.7) tính cho store.LegacyKNDepartment.
func (s *Service) DepartmentPlots(ctx context.Context, code string) (int, error) {
	var n int
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot WHERE coalesce(department_code, $2::text) = $1::text`,
			code, store.LegacyKNDepartment).Scan(&n)
	})
	return n, err
}
