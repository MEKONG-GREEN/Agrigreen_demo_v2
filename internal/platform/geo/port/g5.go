package port

import (
	"context"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// G5 (sản phẩm dữ liệu cho bên thứ ba, ADR-0022). Cổng MỚI, thêm vào — không đổi Plots.
// ---------------------------------------------------------------------------

// PlotFacts tra dữ kiện tối thiểu (xã, HTX, nông hộ, diện tích, trạng thái) của NHIỀU thửa một lần
// cho sản phẩm tổng hợp collection_volume của pf/cropcycle (số nông hộ phân biệt cho ngưỡng k).
// pf/geo là chủ quan hệ thửa ↔ nông hộ. Tenant lấy từ context (tenant.WithID — tenant chủ dữ liệu);
// không kiểm phạm vi người gọi. Thửa không có trong tenant không có trong kết quả. Kết quả chỉ để
// TỔNG HỢP — module gọi không được trả mã thửa/nông hộ ra ngoài.
//
// Thay thế (W3.2): bên tiêu thụ tầng domain dùng cooperativeport.Plots (Facts); cổng này còn cho bên tiêu thụ
// tầng dưới và bộ chuyển cmd/internal/coopwire, xóa ở W3.3 (docs/design/cooperative.md mục 4). Không ghi
// "Deprecated:" để staticcheck không chặn các chỗ dùng hợp lệ còn lại.
type PlotFacts interface {
	PlotFacts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]Plot, error)
}
