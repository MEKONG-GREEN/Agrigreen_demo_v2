package extension

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Hợp đồng chủ KN cho đơn vị nội bộ, ủy quyền có hạn và phạm vi nhân sự (KNSPLIT-2-ACCOUNTS-UNIT-DELEGATION-KN #195)
// theo accounts/port.UnitDelegationPolicy. Định nghĩa (loại đơn vị, loại Nút, seed, quyền ủy quyền, thời hạn, vai nhận)
// là dữ liệu chủ KN; quyết định chỉ đọc phiên chủ KN đã được middleware giải ở server (port.RequireSession) — không
// đọc Principal.KN, header, payload hay vai realm để cấp quyền. Không I/O: pf/accounts lấy đủ giá trị trước giao dịch
// và vẫn tự kiểm tenant (RLS), người giao, người nhận, khóa idempotency, thu hồi/hết hạn. Gói này chỉ khai báo; nối
// vào accounts và gốc lắp ráp là việc của #196.

// Giá trị giữ nguyên hành vi hiện hành (parity với port.InternalUnitKinds, seeddata/internal_units_an_giang.json,
// port.Powers, domain.DelegationMaxDays của accounts).
const (
	unitDelegationSeedStatus = "tạm, chờ khách (Q-068)"
	delegationMaxDuration    = 30 * 24 * time.Hour
	unitKindPhong            = "phong"
)

func internalUnitKinds() []accport.CatalogItem {
	return []accport.CatalogItem{
		{Code: "ban_giam_doc", Label: "Ban Giám đốc"},
		{Code: "van_phong", Label: "Văn phòng"},
		{Code: unitKindPhong, Label: "Phòng"},
		{Code: "tram", Label: "Trạm"},
		{Code: "trai", Label: "Trại"},
		{Code: "tang_cuong", Label: "Tăng cường"},
	}
}

// internalUnitSeed: Ban Giám đốc + 5 phòng có module (gắn mã phòng ban P2). Trạm, trại, văn phòng, Tăng cường do Admin
// thêm khi khách gửi danh sách (Q-068).
func internalUnitSeed() []accport.InternalUnitSeedItem {
	return []accport.InternalUnitSeedItem{
		{Code: "ban_giam_doc", Kind: "ban_giam_doc", Name: "Ban Giám đốc", SortOrder: 0},
		{Code: "trong_trot", Kind: unitKindPhong, Name: "Phòng Trồng trọt", DepartmentCode: partyport.DeptTrongTrot, SortOrder: 10},
		{Code: "chan_nuoi_thuy_san", Kind: unitKindPhong, Name: "Phòng Chăn nuôi - Thủy sản", DepartmentCode: partyport.DeptChanNuoiThuySan, SortOrder: 20},
		{Code: "ke_hoach_tai_chinh", Kind: unitKindPhong, Name: "Phòng Kế hoạch - Tài chính", DepartmentCode: partyport.DeptKeHoachTaiChinh, SortOrder: 30},
		{Code: "thong_tin_truyen_thong", Kind: unitKindPhong, Name: "Phòng Thông tin Tuyên truyền", DepartmentCode: partyport.DeptThongTinTruyenThong, SortOrder: 40},
		{Code: "hanh_chinh_tong_hop", Kind: unitKindPhong, Name: "Phòng Hành chính - Tổng hợp", DepartmentCode: partyport.DeptHanhChinhTongHop, SortOrder: 50},
	}
}

func delegationPowers() []accport.CatalogItem {
	return []accport.CatalogItem{
		{Code: port.SessionPowerReopenPeriod, Label: "Mở lại kỳ"},
		{Code: port.SessionPowerApprove, Label: "Duyệt, xác thực báo cáo"},
	}
}

func knInternalUnitDefinition() accport.InternalUnitDefinition {
	return accport.InternalUnitDefinition{Kinds: internalUnitKinds(), DepartmentKinds: []string{unitKindPhong},
		// Chỉ Nút TTKN cấp 1/2 có đơn vị nội bộ: KN không khai báo đơn vị cho HTX, MKG hay Nút nền tảng.
		OrgKinds:   []string{string(partyport.OrgTTKNNational), string(partyport.OrgTTKNProvince)},
		SeedStatus: unitDelegationSeedStatus, Seed: internalUnitSeed()}
}

func knDelegationDefinition() accport.DelegationDefinition {
	return accport.DelegationDefinition{Powers: delegationPowers(), MaxDuration: delegationMaxDuration,
		GranteeRoles: []string{port.SessionRoleProvince}}
}

type unitDelegationPolicy struct{ attrs *accport.AttributeCatalog }

// UnitDelegationPolicy dựng adapter KN (instance riêng, không trạng thái toàn cục). Dữ liệu chủ sai → lỗi để gốc lắp
// ráp dừng khởi động, không rơi về bridge cũ.
func UnitDelegationPolicy() (accport.UnitDelegationPolicy, error) {
	attrs, err := AttributeCatalog()
	if err != nil {
		return nil, err
	}
	if err := accport.ValidateInternalUnitDefinition(knInternalUnitDefinition()); err != nil {
		return nil, fmt.Errorf("kn: internal unit definition: %w", err)
	}
	if err := accport.ValidateDelegationDefinition(knDelegationDefinition()); err != nil {
		return nil, fmt.Errorf("kn: delegation definition: %w", err)
	}
	return unitDelegationPolicy{attrs: attrs}, nil
}

func (unitDelegationPolicy) InternalUnitDefinition() accport.InternalUnitDefinition {
	return knInternalUnitDefinition()
}

func (unitDelegationPolicy) DelegationDefinition() accport.DelegationDefinition {
	return knDelegationDefinition()
}

// ownerSession: handled=false chỉ khi request thật sự native (không chọn phiên, không có ràng buộc KN cũ dở dang) —
// cùng quy tắc với DefaultUnit (#60). Phiên đã chọn nhưng hỏng/lạ profile/sai vai → lỗi cuối, không quay về.
func ownerSession(ctx context.Context, roles ...string) (port.SessionContext, bool, error) {
	p, ok := authz.FromContext(ctx)
	if ok && p.SelectedSession == nil && p.KN == nil {
		return port.SessionContext{}, false, nil
	}
	_, k, err := port.RequireSession(ctx, roles...)
	if err != nil {
		return port.SessionContext{}, true, err
	}
	return k, true, nil
}

// AuthorizeDelegationGrant: chỉ lãnh đạo phòng (phiên Cấp Tỉnh, Manager, không chỉ xem, không chế độ hỗ trợ) ủy quyền
// trong đúng phòng ban của phiên, ở đơn vị của phiên. Phòng ban so nguyên văn như cũ (rỗng = phòng của phiên).
func (unitDelegationPolicy) AuthorizeDelegationGrant(ctx context.Context, department string) (accport.DelegationGrantDecision, bool, error) {
	k, handled, err := ownerSession(ctx, port.SessionRoleProvince)
	if err != nil || !handled {
		return accport.DelegationGrantDecision{}, handled, err
	}
	if department == "" {
		department = k.Department
	}
	if !k.Manager || k.ReadOnly() || k.Support != nil || k.Department == "" || department != k.Department || k.UnitRef == uuid.Nil {
		return accport.DelegationGrantDecision{}, true, nil
	}
	return accport.DelegationGrantDecision{Allowed: true, UnitRef: k.UnitRef, Department: department}, true, nil
}

// AuthorizeDelegationRevoke: thu hồi qua Application API cần phiên Cấp Tỉnh; người giao, trạng thái và tenant do
// accounts kiểm.
func (unitDelegationPolicy) AuthorizeDelegationRevoke(ctx context.Context) (bool, error) {
	_, handled, err := ownerSession(ctx, port.SessionRoleProvince)
	return handled, err
}

// PeopleScope (BR-04 HCTH, HS-37): Admin, Cấp Tỉnh phòng HCTH (không chỉ xem) và người đọc mọi phòng → toàn bộ; lãnh
// đạo Cấp Tỉnh có đơn vị nội bộ → người Cấp Tỉnh của đơn vị nội bộ + cán bộ xã được phân công quản lý; còn lại chỉ mình.
// Cờ quyền tính lại từ catalog chủ KN với thuộc tính của phiên; thuộc tính không còn trong catalog → từ chối.
func (u unitDelegationPolicy) PeopleScope(ctx context.Context) (accport.PeopleScopeDecision, bool, error) {
	k, handled, err := ownerSession(ctx)
	if err != nil || !handled {
		return accport.PeopleScopeDecision{}, handled, err
	}
	caps, err := u.sessionCapabilities(k)
	if err != nil {
		return accport.PeopleScopeDecision{}, true, err
	}
	switch {
	case k.Role == port.SessionRoleAdmin,
		k.Role == port.SessionRoleProvince && k.Department == partyport.DeptHanhChinhTongHop && !k.ReadOnly() && !caps.ForceReadOnly,
		caps.ReadsAllDepartments:
		return accport.PeopleScopeDecision{Mode: accport.PeopleScopeAll, InternalUnit: k.InternalUnit}, true, nil
	case k.Role != port.SessionRoleProvince || !caps.Manager || k.InternalUnit == "" || k.UnitRef == uuid.Nil:
		return accport.PeopleScopeDecision{Mode: accport.PeopleScopeSelf, InternalUnit: k.InternalUnit}, true, nil
	}
	return accport.PeopleScopeDecision{Mode: accport.PeopleScopeInternalUnit, UnitRef: k.UnitRef, InternalUnit: k.InternalUnit,
		MemberRole: port.SessionRoleProvince, OfficerRole: port.SessionRoleCommune}, true, nil
}

// sessionCapabilities kiểm nhóm/nhiệm vụ của phiên còn trong catalog chủ (như chuẩn hóa thuộc tính cũ) rồi lấy cờ quyền.
func (u unitDelegationPolicy) sessionCapabilities(k port.SessionContext) (accport.AttributeCapabilities, error) {
	role, ok := u.attrs.Role(k.Role)
	if !ok {
		return accport.AttributeCapabilities{}, accport.ErrOwnerAttributesRevoked
	}
	for _, d := range k.Duties {
		if d != "" && u.attrs.DutyLabel(d) == "" {
			return accport.AttributeCapabilities{}, accport.ErrOwnerAttributesRevoked
		}
	}
	if (k.ProvinceGroup != "" && u.attrs.GroupLabel(k.ProvinceGroup) == "") ||
		(!role.GroupAndDuties && (k.ProvinceGroup != "" || slices.ContainsFunc(k.Duties, func(d string) bool { return d != "" }))) {
		return accport.AttributeCapabilities{}, accport.ErrOwnerAttributesRevoked
	}
	caps, err := u.attrs.Capabilities(k.Role, k.MgmtLevel, k.ProvinceGroup)
	if errors.Is(err, accport.ErrAttributeCatalog) {
		return accport.AttributeCapabilities{}, err
	}
	if err != nil {
		return accport.AttributeCapabilities{}, accport.ErrOwnerAttributesRevoked
	}
	return caps, nil
}
