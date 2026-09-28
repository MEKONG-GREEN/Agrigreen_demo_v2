package port

import (
	"context"

	"github.com/google/uuid"
)

// StaffNames là cổng đọc tên hiển thị trong danh bạ cán bộ của pf/extension: tên của
// cán bộ (không phải nông hộ) theo actor_ref, để module khác hiện tên người
// đặt/duyệt tham số, người được giao (web-backend-gaps G-07, G-09). Tenant lấy
// từ context. Không kiểm vai — module gọi đã kiểm vai của nghiệp vụ của nó.
// actor_ref chưa có trong danh bạ thì không có trong kết quả.
type StaffNames interface {
	DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error)
}
