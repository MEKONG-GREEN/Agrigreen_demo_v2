package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrHouseholdNotFound: không có nông hộ.
var ErrHouseholdNotFound = errors.New("party: không có nông hộ")

// HouseholdMembership là thuộc tính KHÔNG định danh của một nông hộ mà module
// khác cần để kiểm quy tắc (vd accounts: liên kết tài khoản nông dân với nông hộ của HTX).
type HouseholdMembership struct {
	Ref            uuid.UUID
	CooperativeRef uuid.UUID // HTX hiện tại; Nil nếu chưa là thành viên
	CommuneCode    string
	Active         bool
}

// Households cho module tầng dưới (pf/accounts — lõi, không import cooperativeport) tra thành viên HTX của
// nông hộ. Tenant lấy từ context. Hiện thực ở pf/cooperative, gốc lắp ráp nối (W3.3); bên tiêu thụ tầng
// domain dùng cooperativeport.Households. Sự kiện nông hộ (pf.party.household.*) do pf/cooperative khai
// (tên giữ nguyên, D-8).
type Households interface {
	Membership(ctx context.Context, ref uuid.UUID) (HouseholdMembership, error) // không có → ErrHouseholdNotFound
}
