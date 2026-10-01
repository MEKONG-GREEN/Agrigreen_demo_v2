package port

// KN v7.7 gói P1 (docs/brd/kn-v7.7-plan.md §3 P1): gán vai trò nhiều vai × Nút × phòng ban × xã phụ
// trách, ngữ cảnh phiên (X-AG-Context), chế độ hỗ trợ của Admin (FN-AD-04.07).

import (
	"context"
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

// Cấp quản lý (FN-AD-04.04) — danh mục TẠM, chờ khách (Q-05). Mã ghi sổ; nhãn hiển thị.
var MgmtLevels = []MgmtLevel{
	{Code: "chuyen_vien", Label: "Chuyên viên"},
	{Code: "pho_truong_phong", Label: "Phó trưởng phòng"},
	{Code: "truong_phong", Label: "Trưởng phòng"},
	{Code: "pho_giam_doc", Label: "Phó Giám đốc"},
	{Code: "giam_doc", Label: "Giám đốc"},
}

// MgmtLevel là một mục của danh mục cấp quản lý.
type MgmtLevel struct {
	Code  string
	Label string
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
