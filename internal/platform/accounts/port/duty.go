package port

import (
	"context"

	"github.com/google/uuid"
)

// RoleHolders (gói NOTIF) cho consumer / job của worker qua gốc lắp ráp: tài khoản có lần gán vai role còn hiệu lực ở
// phòng ban dept (rỗng = mọi) — người nhận thông báo. Chỉ mã tham chiếu, không trùng, đã sắp. Tenant truyền tường minh
// (worker không có Principal).
type RoleHolders interface {
	// RoleHolders trả người có lần gán vai role.
	RoleHolders(ctx context.Context, tenantID uuid.UUID, role, dept string) ([]uuid.UUID, error)
	// DutyHolders trả người có lần gán vai role mang cờ nhiệm vụ duty (W11.07, vd authz.DutyPriceAdmin — QTNV báo giá).
	DutyHolders(ctx context.Context, tenantID uuid.UUID, role, dept, duty string) ([]uuid.UUID, error)
}
