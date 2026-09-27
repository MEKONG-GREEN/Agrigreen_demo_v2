package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Household là hồ sơ nông hộ ở pf_party: CHỈ mã tham chiếu và thuộc tính không
// định danh (R-04). Họ tên, SĐT, CCCD, địa chỉ nằm ở kho định danh.
type Household struct {
	Ref              uuid.UUID // = Created.Ref của kho định danh
	CommuneCode      string
	CultivatedAreaHa *decimal.Decimal
	Active           bool
	CooperativeRef   uuid.UUID  // HTX hiện tại; Nil nếu chưa là thành viên
	MemberSince      *time.Time // ngày vào HTX hiện tại
	CreatedAt        time.Time
}

// Lỗi kiểm dữ liệu nông hộ.
var (
	ErrIdentity    = errors.New("họ tên và SĐT của chủ hộ là bắt buộc")
	ErrCooperative = errors.New("cooperative_ref bắt buộc, phải là HTX đang hoạt động")
)

// ValidateHousehold kiểm thuộc tính không định danh của nông hộ so với now.
func ValidateHousehold(area *decimal.Decimal, since *time.Time, now time.Time) error {
	if area != nil && !ValidArea(*area) {
		return FieldError{"cultivated_area_ha", ErrArea}
	}
	if since != nil && since.After(now) {
		return FieldError{"member_since", ErrDate}
	}
	return nil
}
