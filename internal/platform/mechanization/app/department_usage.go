package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// DepartmentUsage hiện thực party/port.DepartmentUsage (W11.02, FN-AD-04.03): số dòng máy móc HTX (FN-XA-03.04) mang
// department_code của phòng ban — chặn "xóa" phòng ban đã có dữ liệu.
type DepartmentUsage struct{ Svc *Service }

var _ partyport.DepartmentUsage = DepartmentUsage{}

// DepartmentUsage đếm bản ghi (tenant từ Principal).
func (d DepartmentUsage) DepartmentUsage(ctx context.Context, _ uuid.UUID, code string) (partyport.Usage, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return partyport.Usage{}, nil
	}
	var n int
	err := d.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_mechanization.coop_machine_count WHERE department_code = $1`, code).Scan(&n)
	})
	return partyport.Usage{Records: n}, err
}
