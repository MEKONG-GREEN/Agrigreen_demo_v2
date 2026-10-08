package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Cổng pf/party cung cấp cho pf/geo (marker HTX) và cổng pf/party cần từ pf/geo (thống kê thửa đã số hóa): cả hai
// khai ở pf/party (lõi) để phụ thuộc chỉ đi xuống (R-MOD-05) — pf/geo hiện thực PlotStats qua gốc lắp ráp.

// CoopMarker là điểm vị trí HTX (không có dữ liệu cá nhân).
type CoopMarker struct {
	Ref         uuid.UUID
	Code        string
	Name        string
	CommuneCode string
	Lon, Lat    decimal.Decimal
}

// CoopMarkers cho geo đọc điểm HTX làm marker/dự phòng khi HTX chưa có ranh giới
// (GIS US-BND-02 "gắn Marker"). Tenant lấy từ context; không kiểm phạm vi.
type CoopMarkers interface {
	MarkersInBBox(ctx context.Context, minLon, minLat, maxLon, maxLat float64, limit int) ([]CoopMarker, error)
}

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
