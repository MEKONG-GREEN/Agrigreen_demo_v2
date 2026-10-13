package assembly

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	contactport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/port"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// KNContactAccess là cấu hình quyền pf/contact (`cap/directory`, W11.23) của App Khuyến nông — BRD HCTH v3.0 FN-13, §VI.4:
// Cấp TW, Cấp Xã xem họ tên, năm sinh, SĐT; Cấp Tỉnh (mọi phòng ban), Admin xem đủ trường liên hệ; Cấp Tỉnh – HCTH cập
// nhật liên hệ ngoài tỉnh; Admin quản lý toàn bộ. Không ngữ cảnh KN (menu cũ, ứng dụng khác — Q-072): quản trị nền tảng,
// quản trị TTKN tỉnh xem đủ; vai KN khác xem ba trường.
var KNContactAccess = contactport.Access{
	FullBy:           []string{authz.KNTinh, authz.KNAdmin},
	BasicBy:          []string{authz.KNTW, authz.KNXa},
	ManageBy:         []string{authz.KNAdmin},
	ManageDeptBy:     []string{authz.KNTinh},
	ManageDepartment: partyport.DeptHanhChinhTongHop,
	RealmFull:        []string{authz.RolePlatformAdmin, "kn_province_admin"},
	RealmBasic:       []string{"kn_national_admin", "kn_officer", accountsport.RoleKNUser},
	RealmManage:      []string{authz.RolePlatformAdmin},
}

// KNContactStaff nối hồ sơ nhân sự pf/hrm (port.Directory, cổng AG_HRM_RECORDS) làm nguồn cán bộ thuộc tỉnh của Contact.
type KNContactStaff struct{ Dir hrmport.Directory }

var _ contactport.Staff = KNContactStaff{}

// Staff thực hiện contactport.Staff: hồ sơ chưa nghỉ việc; cán bộ xã → cấp Xã, đơn vị = đơn vị quản lý (HS-37).
func (s KNContactStaff) Staff(ctx context.Context) ([]contactport.StaffEntry, error) {
	if s.Dir == nil {
		return nil, contactport.ErrStaffDisabled
	}
	xs, err := s.Dir.StaffDirectory(ctx)
	if errors.Is(err, hrmport.ErrRecordsDisabled) {
		return nil, contactport.ErrStaffDisabled
	}
	if err != nil {
		return nil, err
	}
	out := make([]contactport.StaffEntry, 0, len(xs))
	for _, x := range xs {
		e := contactport.StaffEntry{SubjectRef: x.SubjectRef, AccountRef: x.AccountRef, Level: contactport.LevelTinh,
			Active: x.Status != "left", Unit: x.Unit, Title: x.PositionName}
		if e.Title == "" {
			e.Title = x.Title
		}
		if x.Type == "can_bo_xa" {
			e.Level, e.Unit, e.Commune = contactport.LevelXa, x.MgmtUnit, x.Commune
		}
		out = append(out, e)
	}
	return out, nil
}

// KNContactLabels tra tên đơn vị nội bộ (pf/accounts, Nút của ngữ cảnh người gọi) và tên xã (pf/masterdata).
type KNContactLabels struct {
	Units      accountsport.InternalUnits
	AdminUnits partyport.AdminUnits
}

var _ contactport.Labels = KNContactLabels{}

// UnitNames thực hiện contactport.Labels. Không có ngữ cảnh KN gắn Nút (Admin hệ thống) → rỗng (hiện mã).
func (l KNContactLabels) UnitNames(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	p, ok := authz.FromContext(ctx)
	if l.Units == nil || !ok || p.KN == nil || p.KN.UnitRef == uuid.Nil {
		return out, nil
	}
	xs, err := l.Units.UnitInternalUnits(ctx, p.KN.UnitRef, true)
	if err != nil {
		return out, err
	}
	for _, x := range xs {
		out[x.Code] = x.Name
	}
	return out, nil
}

// CommuneNames thực hiện contactport.Labels.
func (l KNContactLabels) CommuneNames(ctx context.Context, codes []string) (map[string]string, error) {
	out := map[string]string{}
	if l.AdminUnits == nil {
		return out, nil
	}
	now := time.Now()
	for _, c := range codes {
		if u, err := l.AdminUnits.Get(ctx, c, now); err == nil {
			out[c] = u.Name
		}
	}
	return out, nil
}
