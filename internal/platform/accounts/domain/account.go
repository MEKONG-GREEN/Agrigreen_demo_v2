// Package domain: quy tắc tài khoản của pf/accounts — không phụ thuộc hạ tầng,
// không đọc đồng hồ hệ thống (R-12).
package domain

import (
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// RuleVersion ghi vào mọi bản ghi sổ account_event.
const RuleVersion = "accounts/1"

// MinReasonLen: lý do khóa/mở khóa tối thiểu 20 ký tự (HTX US-HTXSTATUS-01 AC-3).
const MinReasonLen = 20

// Action là loại thao tác ghi sổ.
type Action string

// Các thao tác.
const (
	ActionCreated           Action = "created"
	ActionLocked            Action = "locked"
	ActionUnlocked          Action = "unlocked"
	ActionAssignmentChanged Action = "assignment_changed"
	ActionActionsSent       Action = "actions_sent"
	// ActionDormantWarned: job accounts_dormant báo trước khóa vì không hoạt động (D24).
	ActionDormantWarned Action = "dormant_warned"
	// ActionTOTPReset: quản trị viên gỡ TOTP, người dùng đăng ký lại ở lần đăng nhập sau (D24).
	ActionTOTPReset Action = "totp_reset"
)

// Account là trạng thái tài khoản ở pf (không có dữ liệu cá nhân).
type Account struct {
	UserRef   uuid.UUID
	OrgRef    uuid.UUID
	Roles     []string
	Enabled   bool
	LockCause port.LockCause // rỗng khi đang hoạt động
}

// Event là một dòng sổ account_event.
type Event struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Account    Account
	Action     Action
	Reason     string
	Details    map[string]any
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
}

// StreamKey của sổ theo người dùng.
func StreamKey(user uuid.UUID) string { return "user:" + user.String() }

// FieldError là lỗi kiểm dữ liệu theo trường. Msg là thông điệp hiển thị theo AC
// của backlog; không chứa giá trị người dùng nhập (R-14).
type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

// Thông điệp kiểm dữ liệu (theo AC của backlog KN/HTX).
const (
	MsgReasonShort   = "Lý do phải có ít nhất 20 ký tự."
	MsgEmailFormat   = "Sai định dạng Email"
	MsgEmailMissing  = "Thiếu Email"
	MsgNameMissing   = "Thiếu Họ tên"
	MsgNameTooLong   = "Họ tên tối đa 100 ký tự mỗi phần"
	MsgUsernameBad   = "Tên đăng nhập 3–64 ký tự: chữ thường, số, . _ - @"
	MsgPhoneFormat   = "Sai định dạng SĐT (10 chữ số, bắt đầu bằng 0)"
	MsgRolesMissing  = "Thiếu vai trò"
	MsgRoleUnknown   = "Sai định dạng vai trò"
	MsgActionUnknown = "required action không hỗ trợ"
	MsgCoopRequired  = "Vui lòng chọn HTX liên kết."
)

// CheckReason kiểm lý do khóa/mở khóa.
func CheckReason(reason string) error {
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinReasonLen {
		return FieldError{Field: "reason", Msg: MsgReasonShort}
	}
	return nil
}

var (
	usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{2,63}$`)
	phoneRe    = regexp.MustCompile(`^0[0-9]{9}$`)
)

// Profile là định danh của cán bộ gửi lên — chỉ đi thẳng vào Keycloak.
type Profile struct {
	Username  string `pii:"true"`
	Email     string `pii:"true"`
	FirstName string `pii:"true"`
	LastName  string `pii:"true"`
	Phone     string `pii:"true"`
}

// Normalize chuẩn hóa và kiểm hồ sơ; tên đăng nhập rỗng → dùng email.
func (p Profile) Normalize() (Profile, error) {
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.Username = strings.ToLower(strings.TrimSpace(p.Username))
	p.FirstName = strings.TrimSpace(p.FirstName)
	p.LastName = strings.TrimSpace(p.LastName)
	p.Phone = NormalizePhone(p.Phone)
	switch {
	case p.Email == "":
		return p, FieldError{Field: "email", Msg: MsgEmailMissing}
	case !validEmail(p.Email):
		return p, FieldError{Field: "email", Msg: MsgEmailFormat}
	case p.FirstName == "" && p.LastName == "":
		return p, FieldError{Field: "first_name", Msg: MsgNameMissing}
	case utf8.RuneCountInString(p.FirstName) > 100 || utf8.RuneCountInString(p.LastName) > 100:
		return p, FieldError{Field: "first_name", Msg: MsgNameTooLong}
	}
	if p.Username == "" {
		p.Username = p.Email
	}
	if !usernameRe.MatchString(p.Username) {
		return p, FieldError{Field: "username", Msg: MsgUsernameBad}
	}
	if p.Phone != "" && !phoneRe.MatchString(p.Phone) {
		return p, FieldError{Field: "phone", Msg: MsgPhoneFormat}
	}
	return p, nil
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".") && len(s) <= 254
}

// NormalizePhone bỏ khoảng trắng, dấu chấm, gạch; +84 → 0.
func NormalizePhone(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '.' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if rest, ok := strings.CutPrefix(s, "+84"); ok {
		s = "0" + rest
	}
	return s
}

// SplitFullName tách "Họ tên" kiểu Việt Nam: từ cuối là tên (firstName), phần
// trước là họ và tên đệm (lastName).
func SplitFullName(full string) (first, last string) {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return "", ""
	}
	return parts[len(parts)-1], strings.Join(parts[:len(parts)-1], " ")
}

// NormalizeRoles bỏ trùng, sắp xếp và kiểm vai do module quản lý.
func NormalizeRoles(roles []string) ([]string, error) {
	return NormalizeRolesWithCatalog(roles, port.LegacyRoleCatalog())
}

// NormalizeRolesWithCatalog validates realm roles using one instance catalog.
func NormalizeRolesWithCatalog(roles []string, catalog *port.RoleCatalog) ([]string, error) {
	if !catalog.Valid() {
		return nil, FieldError{Field: "roles", Msg: MsgRoleUnknown}
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !catalog.IsManaged(r) {
			return nil, FieldError{Field: "roles", Msg: MsgRoleUnknown}
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, FieldError{Field: "roles", Msg: MsgRolesMissing}
	}
	slices.Sort(out)
	return out, nil
}

// RequiredActions trả required action khi tạo tài khoản: xác minh email, tự đặt
// mật khẩu, và cấu hình TOTP nếu có vai [MFA] hoặc vai cán bộ KN (D24 — port.LoginTOTPRoles).
func RequiredActions(roles []string) []string {
	return RequiredActionsWithCatalog(roles, port.LegacyRoleCatalog())
}

// RequiredActionsWithCatalog applies the same login policy used by MFA status.
func RequiredActionsWithCatalog(roles []string, catalog *port.RoleCatalog) []string {
	acts := []string{port.ActionVerifyEmail, port.ActionUpdatePassword}
	if !catalog.Valid() || catalog.NeedsLoginTOTP(roles) {
		acts = append(acts, port.ActionConfigureTOTP)
	}
	return acts
}

// CheckActions kiểm danh sách required action gửi lại.
func CheckActions(actions []string) ([]string, error) {
	var out []string
	for _, a := range actions {
		if !slices.Contains(port.AllowedActions, a) {
			return nil, FieldError{Field: "actions", Msg: MsgActionUnknown}
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

// ManagedOf lọc vai do module quản lý (bỏ offline_access, vai mặc định…).
func ManagedOf(roles []string) []string {
	return ManagedOfWithCatalog(roles, port.LegacyRoleCatalog())
}

// ManagedOfWithCatalog leaves unmanaged realm roles outside accounts writes.
func ManagedOfWithCatalog(roles []string, catalog *port.RoleCatalog) []string {
	var out []string
	for _, r := range roles {
		if catalog.IsManaged(r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// RolesFitOrg kiểm ràng buộc vai ↔ loại tổ chức (port.RoleOrgKinds).
func RolesFitOrg(roles []string, orgKind string) error {
	return RolesFitOrgWithCatalog(roles, orgKind, port.LegacyRoleCatalog())
}

// RolesFitOrgWithCatalog checks the registered restrictions before directory writes.
func RolesFitOrgWithCatalog(roles []string, orgKind string, catalog *port.RoleCatalog) error {
	if !catalog.Valid() {
		return FieldError{Field: "roles", Msg: MsgRoleUnknown}
	}
	for _, r := range roles {
		kinds := catalog.OrgKinds(r)
		if len(kinds) > 0 && !slices.Contains(kinds, orgKind) {
			msg := "vai " + r + " chỉ gắn cho tổ chức loại " + strings.Join(kinds, ", ")
			if r == "coop_manager" {
				msg = MsgCoopRequired
			}
			return FieldError{Field: "org_ref", Msg: msg}
		}
	}
	return nil
}
