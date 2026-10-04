package port

// Bật module theo đơn vị tham gia (W1.4, R-MOD-08, migration pf 000088). "Đơn vị tham gia" là tenant
// (kiến trúc v4.1 RB-2). Module `enable: always` (module.yaml) luôn bật; module `per_tenant` bật mặc
// định — chỉ tắt khi quản trị ghi sự kiện 'disabled'.

import (
	"context"

	"github.com/google/uuid"
)

// Trạng thái bật của một module trong một tenant.
const (
	ModuleEnabled  = "enabled"
	ModuleDisabled = "disabled"
)

// ModuleEnablement là cổng đọc trạng thái bật module cho gốc lắp ráp (middleware route), pf/accounts
// (chặn cấp vai của module đã tắt) và menu. Kết quả có thể được lưu đệm vài giây; ghi trong cùng tiến
// trình xóa đệm ngay.
type ModuleEnablement interface {
	// ModuleEnabled cho biết module có đang bật cho tenant không. Module luôn bật và module không có
	// trong danh mục (modreg) trả true — cổng chỉ chặn module per_tenant đã tắt.
	ModuleEnabled(ctx context.Context, tenantID uuid.UUID, module string) (bool, error)
	// DisabledModules trả tập module đã tắt của tenant (rỗng = mọi module bật).
	DisabledModules(ctx context.Context, tenantID uuid.UUID) (map[string]bool, error)
}
