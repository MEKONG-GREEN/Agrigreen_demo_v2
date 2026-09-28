// Package port là giao diện công khai DUY NHẤT của module pf/accounts mà module
// khác và gốc lắp ráp được import (R-02).
//
// pf/accounts quản trị tài khoản cán bộ trong Keycloak qua Admin REST (KN
// US-ADM-01..03, HTX US-ACC-01/02, CGH FN-01). Họ tên, email, SĐT, tên đăng nhập
// CHỈ nằm trong Keycloak; module chỉ giữ id người dùng Keycloak, org_ref và ảnh
// chụp vai (R-04 áp cho nhân sự). Kích hoạt tài khoản bằng liên kết một lần của
// Keycloak (execute-actions-email), không bao giờ gửi mật khẩu tạm (ADR-0010).
package port

import "github.com/google/uuid"

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RoleAccountAdmin: quản trị tài khoản toàn quốc — tạo, khóa/mở khóa, phân quyền (KN US-ADM-01).
	RoleAccountAdmin = "account_admin"
	// RoleAccountAdminProvince: quản trị tài khoản của một tỉnh — chỉ cấp kn_officer/coop_manager
	// cho tổ chức trong tỉnh của mình (phạm vi theo org_ref, ADR-0009).
	RoleAccountAdminProvince = "account_admin_province"
)

// Required action của Keycloak mà module đặt khi tạo tài khoản.
const (
	ActionVerifyEmail    = "VERIFY_EMAIL"
	ActionUpdatePassword = "UPDATE_PASSWORD"
	ActionConfigureTOTP  = "CONFIGURE_TOTP"
)

// AllowedActions là các required action được gửi qua liên kết kích hoạt.
var AllowedActions = []string{ActionVerifyEmail, ActionUpdatePassword, ActionConfigureTOTP}

// LockCause là nguyên nhân tài khoản đang bị khóa.
type LockCause string

// Nguyên nhân khóa. Kích hoạt lại tổ chức chỉ mở LockOrgDeactivated (HTX US-HTXSTATUS-02 AC-2).
const (
	LockManual         LockCause = "manual"
	LockOrgDeactivated LockCause = "org_deactivated"
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventUserCreated  = "pf.accounts.user.created"
	EventUserLocked   = "pf.accounts.user.locked"
	EventUserUnlocked = "pf.accounts.user.unlocked"
)

// OrgStatusConsumer là tên consumer (hàng đợi) nhận pf.party.org.deactivated/reactivated.
const OrgStatusConsumer = "pf.accounts.org_status"

// UserCreatedV1 là payload của pf.accounts.user.created. Không chứa dữ liệu cá nhân.
//
//ag:event pf.accounts.user.created v1
type UserCreatedV1 struct {
	UserRef   uuid.UUID `json:"user_ref"`
	OrgRef    uuid.UUID `json:"org_ref"`
	Roles     []string  `json:"roles"`
	ActorRef  uuid.UUID `json:"actor_ref"`
	EventTime string    `json:"event_time"`
	Seq       int64     `json:"seq"`
}

// UserLockedV1 là payload của pf.accounts.user.locked (khóa thủ công hoặc do tổ chức bị vô hiệu hóa).
//
//ag:event pf.accounts.user.locked v1
type UserLockedV1 struct {
	UserRef   uuid.UUID `json:"user_ref"`
	OrgRef    uuid.UUID `json:"org_ref"`
	Cause     string    `json:"cause"`
	ActorRef  uuid.UUID `json:"actor_ref"`
	EventTime string    `json:"event_time"`
	Seq       int64     `json:"seq"`
}

// UserUnlockedV1 là payload của pf.accounts.user.unlocked.
//
//ag:event pf.accounts.user.unlocked v1
type UserUnlockedV1 struct {
	UserRef   uuid.UUID `json:"user_ref"`
	OrgRef    uuid.UUID `json:"org_ref"`
	PrevCause string    `json:"prev_cause"`
	ActorRef  uuid.UUID `json:"actor_ref"`
	EventTime string    `json:"event_time"`
	Seq       int64     `json:"seq"`
}
