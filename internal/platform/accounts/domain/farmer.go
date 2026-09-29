package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// FarmerLink là liên kết tài khoản nông dân ↔ nông hộ (không có dữ liệu cá nhân).
type FarmerLink struct {
	ID             uuid.UUID
	UserRef        uuid.UUID
	HouseholdRef   uuid.UUID
	CooperativeRef uuid.UUID
	CommuneCode    string
	LinkedBy       uuid.UUID
	LinkedAt       time.Time
	IdemKey        string
}

// Kênh và lý do thu hồi của lượt kích hoạt.
const (
	GrantChannelLinkQR   = "link_qr"
	GrantRevokedReissued = "reissued"
)

// Trạng thái lượt kích hoạt (tính tại thời điểm xem).
const (
	GrantActive  = "active"
	GrantUsed    = "used"
	GrantRevoked = "revoked"
	GrantExpired = "expired"
)

// Grant là siêu dữ liệu một lượt kích hoạt — KHÔNG có token hay liên kết.
type Grant struct {
	ID           uuid.UUID
	UserRef      uuid.UUID
	Actions      []string
	Channel      string
	IssuedBy     uuid.UUID
	IssuedAt     time.Time
	ExpiresAt    time.Time
	UsedAt       *time.Time
	RevokedAt    *time.Time
	RevokedBy    *uuid.UUID
	RevokeReason string
	ReplacedBy   *uuid.UUID
	IdemKey      string
}

// Status tính trạng thái lượt tại now.
func (g Grant) Status(now time.Time) string {
	switch {
	case g.UsedAt != nil:
		return GrantUsed
	case g.RevokedAt != nil:
		return GrantRevoked
	case !now.Before(g.ExpiresAt):
		return GrantExpired
	}
	return GrantActive
}

// Thông điệp kiểm dữ liệu tài khoản nông dân (HTX US-ACC-01 AC-7, US-ACC-02 AC-2).
const (
	MsgPhoneMissing       = "Thiếu SĐT"
	MsgHouseholdMissing   = "Thiếu mã nông hộ hoặc SĐT không khớp nông hộ nào"
	MsgPhoneHouseholdDiff = "SĐT không khớp với nông hộ đã chọn"
)

// FarmerProfile là dữ liệu tạo tài khoản nông dân — chỉ đi thẳng vào Keycloak.
// Tên đăng nhập = SĐT chuẩn hóa 0xxxxxxxxx (US-LOGIN-01: đăng nhập bằng SĐT).
type FarmerProfile struct {
	Phone     string `pii:"true"`
	FirstName string `pii:"true"`
	LastName  string `pii:"true"`
}

// Normalize chuẩn hóa SĐT (+84 → 0, bỏ khoảng trắng/dấu chấm/gạch) và họ tên.
func (p FarmerProfile) Normalize() (FarmerProfile, error) {
	p.Phone = NormalizePhone(p.Phone)
	p.FirstName, p.LastName = strings.TrimSpace(p.FirstName), strings.TrimSpace(p.LastName)
	switch {
	case p.Phone == "":
		return p, FieldError{Field: "phone", Msg: MsgPhoneMissing}
	case !phoneRe.MatchString(p.Phone):
		return p, FieldError{Field: "phone", Msg: MsgPhoneFormat}
	case utf8.RuneCountInString(p.FirstName) > 100 || utf8.RuneCountInString(p.LastName) > 100:
		return p, FieldError{Field: "full_name", Msg: MsgNameTooLong}
	}
	return p, nil
}

// ValidPhone cho biết s là SĐT đã chuẩn hóa hợp lệ (10 chữ số, bắt đầu bằng 0).
func ValidPhone(s string) bool { return phoneRe.MatchString(s) }
