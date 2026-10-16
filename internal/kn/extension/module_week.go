package extension

import (
	"context"
	"time"
)

// ReportWeekOf trả tuần báo cáo theo lịch kỳ hệ thống (W11.03) chứa ngày day của tenant ctx — gốc lắp ráp nối cho
// pf/price (W11.76: khóa tuần lưu lúc duyệt bảng giá ngày).
func (m *Module) ReportWeekOf(ctx context.Context, day time.Time) (key string, year int, from, to time.Time) {
	return m.rc.ReportWeekOf(ctx, day)
}
