package port

// KN v7.7 gói P1 (docs/brd/kn-v7.7-plan.md §3 P1): gán vai trò nhiều vai × Nút × phòng ban × xã phụ
// trách, ngữ cảnh phiên (X-AG-Context), chế độ hỗ trợ của Admin (FN-AD-04.07).

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// RoleKNUser là vai realm "cổng vào" bề mặt KN v7.7: mọi tài khoản KN có vai này; vai nghiệp vụ (Cấp
// Quốc gia/Tỉnh/Xã/Admin) nằm trong sổ gán vai trò, không trong token (gap §4.5 phương án A).
const RoleKNUser = "kn_user"

// Vai KN v7.7 (bí danh của authz.KN*).
const (
	KNTW    = authz.KNTW
	KNTinh  = authz.KNTinh
	KNXa    = authz.KNXa
	KNAdmin = authz.KNAdmin
)

// KNRolesAll là mọi vai KN hợp lệ.
var KNRolesAll = authz.KNRoles

// Mã cấp quản lý (FN-AD-04.04; W11.07 mở rộng theo Q-059 — danh mục chung cho mọi module: Lãnh đạo đơn vị,
// Trưởng phòng, Phó trưởng phòng, Phụ trách, Chuyên viên - Cán bộ). Mã là định danh ghi sổ, không đổi.
const (
	MgmtChuyenVien     = "chuyen_vien"
	MgmtPhoTruongPhong = "pho_truong_phong"
	MgmtTruongPhong    = "truong_phong"
	MgmtPhuTrach       = "phu_trach"       // W11.07: phụ trách đơn vị (trạm, trại, phòng chưa có trưởng)
	MgmtLanhDaoDonVi   = "lanh_dao_don_vi" // W11.07: xem mọi phòng ban của đơn vị, chỉ đọc (BR-04 HCTH, Q-059)
	MgmtPhoGiamDoc     = "pho_giam_doc"
	MgmtGiamDoc        = "giam_doc"
)

// MgmtCatalogVersion là phiên bản danh mục cấp quản lý / nhóm Cấp Tỉnh / cờ nhiệm vụ (đổi khi khách trả lời
// Q-059 — danh mục là tham số có phiên bản trong mã, không phải dữ liệu nhập tay).
const MgmtCatalogVersion = "accounts.kn-roles/2"

// Cấp quản lý — danh mục TẠM, chờ khách (Q-05 → Q-059). Mã ghi sổ; nhãn hiển thị; Manager = có quyền của
// lãnh đạo (≥ phó trưởng phòng). Thứ tự = thứ tự hiển thị.
var MgmtLevels = []MgmtLevel{
	{Code: MgmtChuyenVien, Label: "Chuyên viên - Cán bộ"},
	{Code: MgmtPhuTrach, Label: "Phụ trách", Manager: true},
	{Code: MgmtPhoTruongPhong, Label: "Phó trưởng phòng", Manager: true},
	{Code: MgmtTruongPhong, Label: "Trưởng phòng", Manager: true},
	{Code: MgmtLanhDaoDonVi, Label: "Lãnh đạo đơn vị", Manager: true, ReadsAllDepartments: true},
	{Code: MgmtPhoGiamDoc, Label: "Phó Giám đốc", Manager: true},
	{Code: MgmtGiamDoc, Label: "Giám đốc", Manager: true},
}

// MgmtLevel là một mục của danh mục cấp quản lý.
type MgmtLevel struct {
	Code    string
	Label   string
	Manager bool // có quyền của lãnh đạo (Q-067: menu "Nhân sự của phòng"; mặc định có mo_lai_ky, duyet_bao_cao)
	// ReadsAllDepartments: chọn được mọi phòng ban (Đang áp dụng) của đơn vị ở chế độ chỉ xem (Q-059).
	ReadsAllDepartments bool
}

// MgmtLevelOf trả mục của mã (ok = false nếu không biết).
func MgmtLevelOf(code string) (MgmtLevel, bool) {
	for _, l := range MgmtLevels {
		if l.Code == code {
			return l, true
		}
	}
	return MgmtLevel{}, false
}

// Nhóm Cấp Tỉnh (D4 X3; BRD CNTS v3.7 §1.1: viên chức, cán bộ tổng hợp, chủ nhiệm CT/DA, Lãnh đạo phòng, Ban
// Giám đốc "chỉ xem"). Thuộc tính của lần gán kn_tinh — không phải vai mới.
const (
	GroupVienChuc     = "vien_chuc"
	GroupTongHop      = "tong_hop"
	GroupChuNhiem     = "chu_nhiem"
	GroupLanhDaoPhong = "lanh_dao_phong"
	GroupBanGiamDoc   = "ban_giam_doc"
)

// CatalogItem là mục của danh mục mã → nhãn.
type CatalogItem struct {
	Code  string
	Label string
}

// ProvinceGroups là 5 nhóm Cấp Tỉnh.
var ProvinceGroups = []CatalogItem{
	{Code: GroupVienChuc, Label: "Viên chức"},
	{Code: GroupTongHop, Label: "Cán bộ tổng hợp"},
	{Code: GroupChuNhiem, Label: "Chủ nhiệm chương trình/dự án"},
	{Code: GroupLanhDaoPhong, Label: "Lãnh đạo phòng"},
	{Code: GroupBanGiamDoc, Label: "Ban Giám đốc (chỉ xem)"},
}

// Duties là cờ nhiệm vụ trong vai Cấp Tỉnh.
var Duties = []CatalogItem{
	{Code: authz.DutyPriceAdmin, Label: "Quản trị nghiệp vụ báo giá (QTNV)"},
	{Code: authz.DutyFormOwner, Label: "Người phụ trách biểu"},
}

// Powers là quyền nghiệp vụ ủy quyền có hạn được.
var Powers = []CatalogItem{
	{Code: authz.PowerReopenPeriod, Label: "Mở lại kỳ"},
	{Code: authz.PowerApprove, Label: "Duyệt, xác thực báo cáo"},
}

// CatalogLabel trả nhãn của mã trong danh mục (rỗng nếu không có).
func CatalogLabel(items []CatalogItem, code string) string {
	for _, it := range items {
		if it.Code == code {
			return it.Label
		}
	}
	return ""
}

// AssignmentAttrs là thuộc tính phân quyền trong vai (W11.07) của một lần gán — sổ phụ
// pf_accounts.assignment_attr_event cạnh sổ gán vai trò (không thêm cột vào sổ cũ).
type AssignmentAttrs struct {
	AssignmentID  uuid.UUID
	ProvinceGroup string      // chỉ kn_tinh
	Duties        []string    // chỉ kn_tinh
	InternalUnit  string      // mã đơn vị nội bộ (pf/party); kn_xa: đơn vị quản lý (HS-37)
	Facilities    []uuid.UUID // chỉ kn_xa: giới hạn cơ sở
	Households    []uuid.UUID // chỉ kn_xa: giới hạn hộ
	Seq           int64       // 0 = chưa có thuộc tính
	UpdatedBy     uuid.UUID
	UpdatedAt     time.Time
}

// IsManager: cấp quản lý có quyền lãnh đạo, hoặc nhóm Cấp Tỉnh "Lãnh đạo phòng".
func IsManager(mgmtLevel, provinceGroup string) bool {
	l, _ := MgmtLevelOf(mgmtLevel)
	return l.Manager || provinceGroup == GroupLanhDaoPhong
}

// Delegation là một lần ủy quyền có hạn (sổ pf_accounts.delegation_event): người giao (lãnh đạo phòng) giao
// quyền power trong phòng ban cho người nhận (Cấp Tỉnh cùng đơn vị, cùng phòng ban) từ ValidFrom tới ValidTo.
type Delegation struct {
	ID         uuid.UUID
	GrantorRef uuid.UUID
	GranteeRef uuid.UUID
	UnitRef    uuid.UUID
	Department string
	Power      string
	ValidFrom  time.Time
	ValidTo    time.Time
	Revoked    bool
	Reason     string
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// EffectiveAt cho biết ủy quyền còn hiệu lực tại t.
func (d Delegation) EffectiveAt(t time.Time) bool {
	return !d.Revoked && !t.Before(d.ValidFrom) && t.Before(d.ValidTo)
}

// KNPeopleScope là phạm vi nhân sự người gọi được xem (BR-04 HCTH): All (HCTH, Admin), hoặc người thuộc đơn
// vị nội bộ của mình (Members) và cán bộ xã được phân công quản lý (CommuneOfficers, HS-37). Ngoài hai danh
// sách và không All → chỉ chính mình.
type KNPeopleScope struct {
	All             bool
	InternalUnit    string
	Members         []uuid.UUID
	CommuneOfficers []uuid.UUID
}

// Allows cho biết người ref thuộc phạm vi.
func (s KNPeopleScope) Allows(ref uuid.UUID) bool {
	return s.All || slices.Contains(s.Members, ref) || slices.Contains(s.CommuneOfficers, ref)
}

// KNPeople (do P1 cung cấp qua gốc lắp ráp) trả phạm vi nhân sự của ngữ cảnh phiên hiện tại — dùng cho
// pf/hrm (W11.22), Contact, đánh giá (người chấm cán bộ xã theo HS-37).
type KNPeople interface {
	PeopleInScope(ctx context.Context) (KNPeopleScope, error)
}

// MgmtLevelLabel trả nhãn của mã (rỗng nếu không biết).
func MgmtLevelLabel(code string) string {
	for _, l := range MgmtLevels {
		if l.Code == code {
			return l.Label
		}
	}
	return ""
}

// Sự kiện gán vai trò (schema trong contracts/events/).
const (
	EventAssignmentGranted = "pf.accounts.assignment.granted"
	EventAssignmentChanged = "pf.accounts.assignment.changed"
	EventAssignmentRevoked = "pf.accounts.assignment.revoked"
)

// AssignmentGrantedV1 là payload của pf.accounts.assignment.granted. Không có dữ liệu cá nhân: chỉ mã
// tham chiếu, mã phòng ban, mã xã. Ba sự kiện gán vai trò cùng bộ trường.
//
//ag:event pf.accounts.assignment.granted v1
type AssignmentGrantedV1 struct {
	AssignmentID uuid.UUID `json:"assignment_id"`
	UserRef      uuid.UUID `json:"user_ref"`
	Role         string    `json:"role"`
	OrgRef       *string   `json:"org_ref"`
	Departments  []string  `json:"department_codes"`
	Communes     []string  `json:"commune_codes"`
	Active       bool      `json:"active"`
	ActorRef     uuid.UUID `json:"actor_ref"`
	EventTime    string    `json:"event_time"`
	Seq          int64     `json:"seq"`
}

// AssignmentChangedV1 là payload của pf.accounts.assignment.changed (trạng thái SAU khi sửa).
//
//ag:event pf.accounts.assignment.changed v1
type AssignmentChangedV1 struct {
	AssignmentID uuid.UUID `json:"assignment_id"`
	UserRef      uuid.UUID `json:"user_ref"`
	Role         string    `json:"role"`
	OrgRef       *string   `json:"org_ref"`
	Departments  []string  `json:"department_codes"`
	Communes     []string  `json:"commune_codes"`
	Active       bool      `json:"active"`
	ActorRef     uuid.UUID `json:"actor_ref"`
	EventTime    string    `json:"event_time"`
	Seq          int64     `json:"seq"`
}

// AssignmentRevokedV1 là payload của pf.accounts.assignment.revoked (active = false).
//
//ag:event pf.accounts.assignment.revoked v1
type AssignmentRevokedV1 struct {
	AssignmentID uuid.UUID `json:"assignment_id"`
	UserRef      uuid.UUID `json:"user_ref"`
	Role         string    `json:"role"`
	OrgRef       *string   `json:"org_ref"`
	Departments  []string  `json:"department_codes"`
	Communes     []string  `json:"commune_codes"`
	Active       bool      `json:"active"`
	ActorRef     uuid.UUID `json:"actor_ref"`
	EventTime    string    `json:"event_time"`
	Seq          int64     `json:"seq"`
}

// Assignment là một lần gán vai trò hiện hành (bảng chiếu role_assignment_current).
type Assignment struct {
	ID          uuid.UUID
	UserRef     uuid.UUID
	Role        string
	OrgRef      uuid.UUID // Nil với admin
	UnitRef     uuid.UUID // đơn vị sở hữu phòng ban
	Departments []string
	MgmtLevel   string
	Communes    []string
	ValidFrom   time.Time
	ValidTo     *time.Time
	Active      bool
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedBy   uuid.UUID
	UpdatedAt   time.Time
}

// EffectiveAt cho biết lần gán còn hiệu lực tại t.
func (a Assignment) EffectiveAt(t time.Time) bool {
	if !a.Active || t.Before(a.ValidFrom) {
		return false
	}
	return a.ValidTo == nil || t.Before(*a.ValidTo)
}

// Assignments cho module khác (P4 cán bộ phụ trách xã của phòng ban, P5 người duyệt lịch = Trưởng phòng):
// lần gán hiện hành. Tenant lấy từ context.
type Assignments interface {
	// ActiveAssignments trả lần gán còn hiệu lực có vai role (rỗng = mọi vai) và phòng ban dept (rỗng = mọi).
	ActiveAssignments(ctx context.Context, role, dept string) ([]Assignment, error)
}
