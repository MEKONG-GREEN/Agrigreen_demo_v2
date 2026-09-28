package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// StaffDirectory hiện thực port.StaffDirectory: đọc danh bạ trực, trả mã tham
// chiếu và đơn vị — không bao giờ trả tên hiển thị (R-14).
type StaffDirectory struct{ Svc *Service }

var _ port.StaffDirectory = StaffDirectory{}

// ActiveStaff thực hiện port.StaffDirectory.
func (d StaffDirectory) ActiveStaff(ctx context.Context, tid uuid.UUID, units []string) ([]port.StaffRef, error) {
	if len(units) == 0 {
		return nil, nil
	}
	var out []port.StaffRef
	err := tenant.WithTenantTx(ctx, d.Svc.DB, tid, func(tx pgx.Tx) error {
		staff, err := store.StaffInUnits(ctx, tx, false, units)
		if err != nil {
			return err
		}
		for _, st := range staff {
			out = append(out, port.StaffRef{ActorRef: st.ActorRef, UnitCode: st.UnitCode, ProvinceCode: st.ProvinceCode})
		}
		return nil
	})
	return out, err
}
