package port

import (
	"context"

	"github.com/google/uuid"
)

// FarmerIdentity là cổng hẹp tới kho định danh cho tên nông dân của thửa KN (KN v7.7 FN-XA-03.05).
// pf/geo không import vault client trực tiếp: cmd/api-public dựng pf/geo cho sản phẩm hub_capacity và
// không được kéo vault kể cả gián tiếp (RB-3, ADR-0022 mục 1). Bộ điều hợp: internal/platform/geo/port/farmerid
// (gốc lắp ráp api-platform nối bằng geo.Module.UseKN).
type FarmerIdentity interface {
	// RegisterFarmer ghi họ tên vào kho định danh (chủ thể "người", mục đích extension.contact), trả ref;
	// tên đã có → ref đã có, không lỗi. Lỗi kho trả nguyên (problem 422 khi kho từ chối dữ liệu).
	RegisterFarmer(ctx context.Context, fullName, areaCode string) (uuid.UUID, error)
	// FarmerLabel trả nhãn đã che của nông dân; có thể trả nhãn kèm lỗi (nhãn dự phòng của kho).
	FarmerLabel(ctx context.Context, ref uuid.UUID) (string, error)
}
