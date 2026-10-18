package app

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// StaffDirectory hiện thực port.StaffDirectory: đọc danh bạ trực, trả mã tham
// chiếu và đơn vị — không bao giờ trả tên hiển thị (R-14).
type StaffDirectory struct{ Svc *Service }

var _ notifyport.StaffDirectory = StaffDirectory{}

// ActiveStaff thực hiện port.StaffDirectory.
func (d StaffDirectory) ActiveStaff(ctx context.Context, tid uuid.UUID, units []string) ([]notifyport.StaffRef, error) {
	if len(units) == 0 {
		return nil, nil
	}
	var out []notifyport.StaffRef
	err := tenant.WithTenantTx(ctx, d.Svc.DB, tid, func(tx pgx.Tx) error {
		staff, err := store.StaffInUnits(ctx, tx, false, units)
		if err != nil {
			return err
		}
		for _, st := range staff {
			out = append(out, notifyport.StaffRef{ActorRef: st.ActorRef, UnitCode: st.UnitCode, ProvinceCode: st.ProvinceCode})
		}
		return nil
	})
	return out, err
}
