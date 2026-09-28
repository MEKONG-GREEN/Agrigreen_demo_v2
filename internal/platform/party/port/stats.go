package port

import "context"

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
type CooperativeStats interface {
	// CooperativesByUnit trả thống kê theo mã đơn vị; units nil = mọi đơn vị.
	// Đơn vị không có HTX không có trong kết quả.
	CooperativesByUnit(ctx context.Context, units []string) ([]UnitCooperatives, error)
}
