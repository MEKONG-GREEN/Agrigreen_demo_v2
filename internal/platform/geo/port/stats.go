package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PlotStat là số thửa đã số hoá (chưa ngừng dùng: nháp + đã gán HTX) và tổng
// diện tích của một nhóm (xã hoặc HTX). Không có dữ liệu cá nhân.
type PlotStat struct {
	CommuneCode    string    // theo xã (DigitizedByCommune)
	CooperativeRef uuid.UUID // theo HTX (DigitizedByCooperative)
	Plots          int64
	AreaHa         decimal.Decimal
}

// PlotStats là cổng đọc thống kê thửa cho module khác (web-backend-gaps G-02,
// G-03). Tenant lấy từ context. Không kiểm phạm vi người gọi — module gọi tự
// giới hạn danh sách xã/HTX theo phạm vi của mình.
type PlotStats interface {
	// DigitizedByCommune trả thống kê theo xã; communes nil = mọi xã của tenant.
	// Xã không có thửa không có trong kết quả.
	DigitizedByCommune(ctx context.Context, communes []string) ([]PlotStat, error)
	// DigitizedByCooperative trả thống kê theo HTX (chỉ HTX có thửa).
	DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]PlotStat, error)
}
