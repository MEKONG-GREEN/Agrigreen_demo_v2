package port

import (
	"context"
	"github.com/google/uuid"
)

// PriceDesk tìm người nhận Cấp Tỉnh của Báo giá nông sản (pf/notify khai cổng; gốc lắp ráp hiện thực từ pf/accounts —
// lần gán Cấp Tỉnh của phòng ban chủ dữ liệu giá và cờ nhiệm vụ QTNV). Tenant truyền tường minh (consumer của worker
// không có Principal). Chỉ mã tham chiếu — không tên, không email. nil → thông báo chủ đề theo vai Cấp Tỉnh (không email).
type PriceDesk interface {
	// Stewards trả cán bộ Cấp Tỉnh có cờ "Quản trị nghiệp vụ báo giá" của bên dùng profile.
	Stewards(ctx context.Context, tenantID uuid.UUID, profile string) ([]uuid.UUID, error)
	// ProvinceOfficers trả mọi cán bộ Cấp Tỉnh của bên dùng profile (xã chưa phân công — mọi Cấp Tỉnh xử lý).
	ProvinceOfficers(ctx context.Context, tenantID uuid.UUID, profile string) ([]uuid.UUID, error)
}
