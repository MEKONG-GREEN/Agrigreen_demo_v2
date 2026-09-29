package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrHouseholdNotFound: không có nông hộ.
var ErrHouseholdNotFound = errors.New("party: không có nông hộ")

// HouseholdMembership là thuộc tính KHÔNG định danh của một nông hộ mà module
// khác cần để kiểm quy tắc (vd geo: chỉ gán thửa cho thành viên của HTX sở hữu thửa).
type HouseholdMembership struct {
	Ref            uuid.UUID
	CooperativeRef uuid.UUID // HTX hiện tại; Nil nếu chưa là thành viên
	CommuneCode    string
	Active         bool
}

// Households cho module khác tra thành viên HTX của nông hộ. Tenant lấy từ context.
type Households interface {
	Membership(ctx context.Context, ref uuid.UUID) (HouseholdMembership, error) // không có → ErrHouseholdNotFound
}

// Sự kiện nông hộ (schema trong contracts/events/). Consumer pf/notify báo cán bộ
// khuyến nông của xã (KN US-HTX-05: nông hộ đăng ký từ App HTX mang đúng mã định
// danh chung household_ref — không tạo bản ghi trùng).
const (
	EventHouseholdRegistered        = "pf.party.household.registered"
	EventHouseholdMembershipChanged = "pf.party.household.membership_changed"
)

// Nguồn đăng ký nông hộ (trường source của HouseholdRegisteredV1).
const (
	HouseholdSourceHTXApp = "htx_app" // quản lý HTX trên App HTX
	HouseholdSourceKNApp  = "kn_app"  // cán bộ khuyến nông / quản trị
	HouseholdSourceImport = "import"  // nhập tệp hàng loạt
)

// Loại thay đổi thành viên HTX (trường change của HouseholdMembershipChangedV1).
const (
	MembershipJoined      = "joined"
	MembershipLeft        = "left"
	MembershipTransferred = "transferred"
)

// HouseholdRegisteredV1 là payload của pf.party.household.registered: nông hộ MỚI
// được ghi ở pf_party (không phát khi trùng chỉ mục mù — dùng lại mã cũ). Không có
// dữ liệu cá nhân.
//
//ag:event pf.party.household.registered v1
type HouseholdRegisteredV1 struct {
	HouseholdRef   uuid.UUID  `json:"household_ref"`
	CommuneCode    string     `json:"commune_code"`
	CooperativeRef *uuid.UUID `json:"cooperative_ref"`
	MemberSince    *string    `json:"member_since"`
	Source         string     `json:"source"`
	ActorRef       uuid.UUID  `json:"actor_ref"`
	EventTime      string     `json:"event_time"`
}

// HouseholdMembershipChangedV1 là payload của pf.party.household.membership_changed:
// nông hộ vào, rời hoặc chuyển HTX. Không có dữ liệu cá nhân (lý do không đi kèm).
//
//ag:event pf.party.household.membership_changed v1
type HouseholdMembershipChangedV1 struct {
	HouseholdRef       uuid.UUID  `json:"household_ref"`
	CommuneCode        string     `json:"commune_code"`
	PrevCooperativeRef *uuid.UUID `json:"prev_cooperative_ref"`
	CooperativeRef     *uuid.UUID `json:"cooperative_ref"`
	Change             string     `json:"change"`
	EffectiveDate      string     `json:"effective_date"`
	ActorRef           uuid.UUID  `json:"actor_ref"`
	EventTime          string     `json:"event_time"`
}
