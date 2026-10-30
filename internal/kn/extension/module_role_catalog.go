package extension

import (
	"errors"
	"fmt"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Vai realm do hệ Khuyến nông sở hữu (KNSPLIT-2-ROLE-CATALOG-KN #95). Tên trùng tên vai trong realm Keycloak hiện hành;
// gói này không tạo/sửa vai realm hay client.
const (
	RealmRoleKNNationalAdmin = "kn_national_admin" // cán bộ TTKN Quốc gia
	RealmRoleKNProvinceAdmin = "kn_province_admin" // cán bộ TTKN tỉnh
	RealmRoleKNOfficer       = "kn_officer"        // cán bộ xã / Tổ KNCĐ
	RealmRoleKNUser          = "kn_user"           // KN v7.7: cổng vào bề mặt KN; vai nghiệp vụ ở sổ gán vai trò
)

// ErrKNRoleCatalog: cấu hình catalog vai KN không hợp lệ (lỗi cấu hình máy chủ, không phải lỗi HTTP).
var ErrKNRoleCatalog = errors.New("kn: invalid role catalog configuration")

// RoleRegistrations trả khai báo vai realm của KN cho accounts/port.ExtendRoleCatalog. Mỗi lần gọi tạo bản sao mới
// (slice độc lập), không đọc/ghi trạng thái toàn cục, không phụ thuộc request hay phiên ứng dụng.
//
// Parity với bridge legacy (accounts/port/grants.go):
//   - cả bốn vai LoginTOTP=true (D24, CEO 13/10/2026 QĐ-4 c), MFARole=false — không phải vai [MFA] của Admin API,
//     không phải AdminRoles (TTKN QG/tỉnh không quản trị tài khoản, KN US-ADM-01 AC-4);
//   - OrgKinds: kn_national_admin → ttkn_national, kn_province_admin → ttkn_province, kn_officer → extension_team;
//     kn_user không hạn loại tổ chức;
//   - Grantors: platform_admin + account_admin cho cả bốn; riêng kn_officer thêm account_admin_province.
func RoleRegistrations() []accport.RoleRegistration {
	both := func(extra ...string) []string {
		return append([]string{"platform_admin", accport.RoleAccountAdmin}, extra...)
	}
	return []accport.RoleRegistration{
		{Role: RealmRoleKNNationalAdmin, LoginTOTP: true, OrgKinds: []string{string(partyport.OrgTTKNNational)}, Grantors: both()},
		{Role: RealmRoleKNProvinceAdmin, LoginTOTP: true, OrgKinds: []string{string(partyport.OrgTTKNProvince)}, Grantors: both()},
		{Role: RealmRoleKNOfficer, LoginTOTP: true, OrgKinds: []string{string(partyport.OrgExtensionTeam)}, Grantors: both(accport.RoleAccountAdminProvince)},
		{Role: RealmRoleKNUser, LoginTOTP: true, Grantors: both()},
	}
}

// RoleCatalog ghép khai báo vai KN vào nền base do gốc lắp ráp chọn — thường là accport.NativeRoleCatalog() (#96).
// Không có fallback: base nil hoặc không hợp lệ, hay base đã chứa vai KN (vd LegacyRoleCatalog) → lỗi, gốc lắp ráp
// phải dừng khởi động thay vì quay về bridge. Base không bị sửa; kết quả là snapshot riêng của lần gọi.
func RoleCatalog(base *accport.RoleCatalog) (*accport.RoleCatalog, error) {
	if !base.Valid() {
		return nil, fmt.Errorf("%w: missing or invalid base", ErrKNRoleCatalog)
	}
	c, err := accport.ExtendRoleCatalog(base, RoleRegistrations()...) // từ chối vai trùng base (không override)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKNRoleCatalog, err)
	}
	return c, nil
}
