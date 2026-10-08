package port

import (
	"context"

	"github.com/google/uuid"
)

// Danh bạ cán bộ để pf/notify tìm người nhận: pf/notify khai cổng nó cần, pf/extension (chủ danh bạ trực)
// hiện thực qua gốc lắp ráp — phụ thuộc chỉ đi xuống (R-MOD-05).

// StaffRef là một cán bộ trong danh bạ trực, CHỈ gồm mã tham chiếu và đơn vị —
// không có tên hiển thị (R-14): module khác không cần tên để gửi thông báo.
type StaffRef struct {
	ActorRef     uuid.UUID
	UnitCode     string // xã, hoặc mã tỉnh với cán bộ cấp tỉnh
	ProvinceCode string
}

// ProvinceLevel cho biết cán bộ thuộc cấp tỉnh (TTKN tỉnh).
func (s StaffRef) ProvinceLevel() bool { return s.UnitCode != "" && s.UnitCode == s.ProvinceCode }

// StaffDirectory là cổng đọc danh bạ cán bộ cho module khác (vd pf/notify tìm
// người nhận thông báo). Tenant truyền tường minh vì consumer của worker không có
// Principal; module gọi phải tự bảo đảm tenant lấy từ phong bì sự kiện đã xác thực.
type StaffDirectory interface {
	// ActiveStaff trả cán bộ đang hoạt động có đơn vị (xã) hoặc tỉnh thuộc units.
	// Mã tỉnh khớp mọi cán bộ của tỉnh (cả cấp xã). units rỗng → rỗng.
	ActiveStaff(ctx context.Context, tenantID uuid.UUID, units []string) ([]StaffRef, error)
}
