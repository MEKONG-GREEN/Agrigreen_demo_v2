package assembly

import (
	"context"

	"github.com/google/uuid"

	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// KNHRMAccess là cấu hình quyền dom/hrm (W11.21, ADR-0050) của App Khuyến nông — BRD HCTH v3.0 §VI.4: danh mục nhân sự
// (FN-19) Admin CRUD, Cấp Tỉnh – HCTH xem; chủ dữ liệu là Phòng Hành chính – Tổng hợp.
var KNHRMAccess = hrmport.Access{
	Department: partyport.DeptHanhChinhTongHop,
	ManageBy:   []string{authz.KNAdmin},
	ReadBy:     []string{authz.KNTinh},
}

// KNHRMUnits nối danh mục đơn vị nội bộ của pf/accounts (W11.07, pf_accounts.internal_unit) vào hồ sơ nhân sự (W11.22:
// đơn vị công tác HS-16, đơn vị quản lý HS-37).
type KNHRMUnits struct{ Units accountsport.InternalUnits }

var _ hrmport.Units = KNHRMUnits{}

// InternalUnits thực hiện hrmport.Units.
func (u KNHRMUnits) InternalUnits(ctx context.Context, unitRef uuid.UUID) ([]hrmport.Unit, error) {
	if u.Units == nil {
		return nil, nil
	}
	xs, err := u.Units.UnitInternalUnits(ctx, unitRef, true)
	if err != nil {
		return nil, err
	}
	out := make([]hrmport.Unit, 0, len(xs))
	for _, x := range xs {
		out = append(out, hrmport.Unit{Code: x.Code, Name: x.Name, Kind: x.Kind, Active: x.Active})
	}
	return out, nil
}
