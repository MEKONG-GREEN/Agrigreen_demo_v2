package port

import (
	"context"

	"github.com/google/uuid"
)

// XaScope là phạm vi một cán bộ Cấp Xã của phòng ban (gộp các lần gán còn hiệu lực): xã phụ trách và giới hạn cơ
// sở/hộ (W11.07). Facilities và Households cùng rỗng = cả các xã phụ trách.
type XaScope struct {
	ActorRef   uuid.UUID
	Communes   []string
	Facilities []uuid.UUID
	Households []uuid.UUID
}

// CoversFacility cho biết cán bộ được thao tác cơ sở ref (thuộc xã commune): cơ sở có trong danh sách giới hạn, hoặc
// không có giới hạn và xã của cơ sở là xã phụ trách.
func (x XaScope) CoversFacility(ref uuid.UUID, commune string) bool {
	for _, f := range x.Facilities {
		if f == ref {
			return true
		}
	}
	if len(x.Facilities) > 0 || len(x.Households) > 0 {
		return false
	}
	for _, c := range x.Communes {
		if c == commune {
			return true
		}
	}
	return false
}

// XaScopes (W11.46, qua gốc lắp ráp) liệt kê cán bộ Cấp Xã của phòng ban dept kèm phạm vi — lưới lịch trực KSGM
// BM-03 (danh sách viên chức, cảnh báo "viên chức chưa được cấp quyền cơ sở", FN-15 BR-04). Tenant truyền tường minh.
type XaScopes interface {
	XaScopes(ctx context.Context, tenantID uuid.UUID, dept string) ([]XaScope, error)
}
