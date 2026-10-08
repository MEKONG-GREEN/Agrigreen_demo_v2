package port

import (
	"context"

	"github.com/google/uuid"
)

// SeasonActivity là số vụ chưa xong và nhật ký chờ duyệt của một HTX (vụ ở pf/cooperative,
// nối ở gốc lắp ráp — party không import cooperativeport): cảnh báo phạm vi ảnh hưởng trước
// khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1). Tenant lấy từ context.
type SeasonActivity interface {
	CoopActivity(ctx context.Context, coop uuid.UUID) (openSeasons, pendingLogs int64, err error)
}
