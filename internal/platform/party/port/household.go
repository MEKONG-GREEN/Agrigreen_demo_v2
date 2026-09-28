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
