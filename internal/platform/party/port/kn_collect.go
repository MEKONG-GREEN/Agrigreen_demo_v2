package port

import (
	"context"

	"github.com/google/uuid"
)

// CoopMachineCounts (tùy chọn, KN v7.7 FN-XA-03.04 — pf/mechanization) trả tổng số máy đang khai báo của một
// HTX cho cảnh báo trước khi vô hiệu hóa HTX (FN-XA-03.03: "vô hiệu hóa HTX có thửa, máy → cảnh báo").
// Tenant lấy từ context. Không kiểm phạm vi người gọi — pf/party đã kiểm.
type CoopMachineCounts interface {
	CoopMachineTotal(ctx context.Context, cooperativeRef uuid.UUID) (int, error)
}
