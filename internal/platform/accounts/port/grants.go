package port

import "slices"

// Ma trận cấp vai (KN US-ADM-01, CGH FN-01).
//
// Người gọi chỉ cấp, thu hồi hoặc thao tác (khóa, mở khóa, gửi lại liên kết) trên
// tài khoản khi đồng thời:
//
//  1. có một vai quản trị tài khoản trong bảng Grants;
//  2. MỌI vai (do module quản lý) mà tài khoản đích đang có hoặc sẽ có đều nằm
//     trong tập vai người gọi được cấp — quản trị tỉnh không khóa được cán bộ
//     TTKN tỉnh hay quản trị toàn quốc;
//  3. tổ chức (org_ref) của tài khoản đích nằm trong phạm vi của người gọi theo
//     cây TW → Tỉnh → Xã (party/port.ScopeResolver, ADR-0009); tổ chức cấp quốc
//     gia (không có mã đơn vị hành chính) chỉ người có phạm vi toàn quốc thao tác;
//  4. với vai có ràng buộc loại tổ chức (RoleOrgKinds), tổ chức phải đúng loại —
//     vì phạm vi dữ liệu của người dùng được tính từ loại tổ chức.
//
// Bảng:
//
//	platform_admin          → mọi vai trong ManagedRoles (phạm vi toàn quốc)
//	account_admin           → ManagedRoles trừ account_admin, param_approver, vault_eraser
//	account_admin_province  → kn_officer, coop_manager — chỉ tổ chức trong tỉnh của mình
//
// Vai platform_admin, offline_access, default-roles-agrigreen và tài khoản dịch vụ
// (cgh_sync) KHÔNG cấp được qua API: chỉ quản trị realm cấp trong console Keycloak.
// TTKN QG và TTKN tỉnh (kn_national_admin, kn_province_admin) không quản trị tài
// khoản (KN US-ADM-01 AC-4).

// ManagedRoles là các vai realm mà module được cấp/thu hồi. Vai khác của người
// dùng (offline_access, vai mặc định, vai do quản trị realm cấp) không bị đụng tới.
var ManagedRoles = []string{
	RoleAccountAdmin, RoleAccountAdminProvince,
	"param_setter", "param_approver", "masterdata_admin", "registry_admin", "reports_admin",
	"consent_writer", "consent_reader",
	"kn_national_admin", "kn_province_admin", "kn_officer", "coop_manager",
	"cgh_viewer", "cgh_admin",
	"plot_editor", "plot_viewer", "geo_admin",
	"content_editor_national", "content_editor_province", "content_reviewer",
	"mrv_collector", "mrv_reviewer", "mrv_viewer", "mrv_exporter",
	"device_admin", "sync_conflict_resolver",
	"vault_registrar", "vault_display", "vault_evidence_viewer", "vault_eraser", "vault_pubkey_sync",
}

// MFARoles là vai đánh dấu [MFA] trong realm: người giữ vai phải cấu hình TOTP
// (required action CONFIGURE_TOTP khi tạo hoặc khi được cấp thêm vai).
var MFARoles = []string{
	"platform_admin", "param_setter", "param_approver", "masterdata_admin", "registry_admin", "reports_admin",
	"cgh_admin", "geo_admin", "device_admin", "sync_conflict_resolver", "vault_eraser",
	RoleAccountAdmin, RoleAccountAdminProvince,
}

// RoleOrgKinds: vai chỉ gắn được cho người thuộc tổ chức có loại tương ứng (giá trị
// là party/port.OrgKind). Vai không có trong bảng gắn được cho mọi loại tổ chức.
var RoleOrgKinds = map[string][]string{
	"kn_national_admin":       {"ttkn_national"},
	"kn_province_admin":       {"ttkn_province"},
	"kn_officer":              {"extension_team"},
	"coop_manager":            {"cooperative"},   // HTX US-ACC-01 AC-7: "Vui lòng chọn HTX liên kết."
	RoleAccountAdminProvince:  {"ttkn_province"}, // phạm vi tỉnh tính từ TTKN tỉnh
	"cgh_viewer":              {"dcrd", "mkg"},   // Cục KTHT (CGH FN-01)
	"content_editor_province": {"ttkn_province"},
}

// Grants là ma trận cấp vai theo vai quản trị của người gọi.
var Grants = map[string][]string{
	"platform_admin":         ManagedRoles,
	RoleAccountAdmin:         without(ManagedRoles, RoleAccountAdmin, "param_approver", "vault_eraser"),
	RoleAccountAdminProvince: {"kn_officer", "coop_manager"},
}

// AdminRoles là các vai được mở chức năng quản trị tài khoản.
var AdminRoles = []string{"platform_admin", RoleAccountAdmin, RoleAccountAdminProvince}

func without(all []string, drop ...string) []string {
	out := make([]string, 0, len(all))
	for _, r := range all {
		if !slices.Contains(drop, r) {
			out = append(out, r)
		}
	}
	return out
}

// Grantable trả tập vai mà người giữ các vai callerRoles được cấp (hợp của các dòng).
func Grantable(callerRoles []string) []string {
	var out []string
	for _, cr := range callerRoles {
		for _, r := range Grants[cr] {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// IsManaged cho biết vai do module quản lý.
func IsManaged(role string) bool { return slices.Contains(ManagedRoles, role) }

// NeedsMFA cho biết trong roles có vai [MFA].
func NeedsMFA(roles []string) bool {
	for _, r := range roles {
		if slices.Contains(MFARoles, r) {
			return true
		}
	}
	return false
}
