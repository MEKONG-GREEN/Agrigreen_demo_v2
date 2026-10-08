package masterdata

// Nguồn của bản chiếu k_ref.admin_unit (pf/kref, W2.5, ADR-0029): mọi phiên bản đơn vị hành chính (kể cả đã
// đóng hiệu lực, kèm mã kế thừa — ADR-0032), đọc bằng vai của module (masterdata_app, R-05). Dòng chảy thường
// ngày là sự kiện pf.masterdata.admin_unit.changed (nhập danh mục).

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	krefport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type krefUnits struct{ db tenant.Beginner }

var _ krefport.AdminUnitSource = krefUnits{}

// KRefAdminUnits trả nguồn đơn vị hành chính của k_ref.
func (m *Module) KRefAdminUnits() krefport.AdminUnitSource { return krefUnits{db: m.svc.DB} }

func (s krefUnits) AdminUnitVersions(ctx context.Context, tid uuid.UUID) ([]krefport.AdminUnit, error) {
	var us []domain.Unit
	if err := tenant.WithTenantTx(ctx, s.db, tid, func(tx pgx.Tx) error {
		var e error
		us, e = store.AllUnits(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]krefport.AdminUnit, 0, len(us))
	for _, u := range us {
		out = append(out, krefport.AdminUnit{Code: u.Code, ValidFrom: u.ValidFrom, ValidTo: u.ValidTo, Name: u.Name, Level: string(u.Level),
			ParentCode: u.ParentCode, SuccessorCodes: u.Successors})
	}
	return out, nil
}

// KRefTenants liệt kê tenant có đơn vị hành chính (pool ag_ops_jobs — BYPASSRLS, chỉ cột tenant_id,
// pf 000092).
func KRefTenants(pool *pgxpool.Pool) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_masterdata.admin_unit`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}
