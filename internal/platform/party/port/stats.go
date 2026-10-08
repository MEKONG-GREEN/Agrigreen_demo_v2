package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// UnitCooperatives là số HTX đang hoạt động và số hộ thành viên đang mở của một
// đơn vị hành chính (xã của HTX). Không có dữ liệu cá nhân.
type UnitCooperatives struct {
	UnitCode     string
	Cooperatives int64
	Households   int64
}

// CooperativeStats là cổng đọc thống kê HTX cho module khác (web-backend-gaps
// G-02: bảng điều hành gộp). Tenant lấy từ context. Không kiểm phạm vi — module
// gọi tự giới hạn danh sách đơn vị theo phạm vi của mình.
//
// Thay thế (W3.2): bên tiêu thụ tầng domain dùng cooperativeport.Cooperatives (ByUnit); cổng này còn cho bên tiêu thụ
// tầng dưới và bộ chuyển cmd/internal/coopwire, xóa ở W3.3 (docs/design/cooperative.md mục 4). Không ghi
// "Deprecated:" để staticcheck không chặn các chỗ dùng hợp lệ còn lại.
type CooperativeStats interface {
	// CooperativesByUnit trả thống kê theo mã đơn vị; units nil = mọi đơn vị.
	// Đơn vị không có HTX không có trong kết quả.
	CooperativesByUnit(ctx context.Context, units []string) ([]UnitCooperatives, error)
}

// CooperativeSite là một HTX đang hoạt động kèm xã, vị trí hồ sơ và số hộ thành viên
// đang mở — nguồn #2, #3 (dự phòng), #6 của mô phỏng chọn Hub (qua gốc lắp ráp). Không
// có dữ liệu cá nhân.
type CooperativeSite struct {
	Ref         uuid.UUID
	Code        string
	CommuneCode string
	Lon, Lat    *decimal.Decimal // nil: hồ sơ HTX chưa có toạ độ
	Members     int64
}

// CooperativeSites liệt kê HTX đang hoạt động của tenant (tenant lấy từ context,
// không kiểm phạm vi — module gọi tự giới hạn).
//
// Thay thế (W3.2): bên tiêu thụ tầng domain dùng cooperativeport.Cooperatives (Active); cổng này còn cho bên tiêu thụ
// tầng dưới và bộ chuyển cmd/internal/coopwire, xóa ở W3.3 (docs/design/cooperative.md mục 4). Không ghi
// "Deprecated:" để staticcheck không chặn các chỗ dùng hợp lệ còn lại.
type CooperativeSites interface {
	ActiveCooperatives(ctx context.Context) ([]CooperativeSite, error)
}

// SeasonActivity là số vụ chưa xong và nhật ký chờ duyệt của một HTX (pf/cropcycle,
// nối ở gốc lắp ráp — party không import cropcycle): cảnh báo phạm vi ảnh hưởng trước
// khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1). Tenant lấy từ context.
type SeasonActivity interface {
	CoopActivity(ctx context.Context, coop uuid.UUID) (openSeasons, pendingLogs int64, err error)
}
