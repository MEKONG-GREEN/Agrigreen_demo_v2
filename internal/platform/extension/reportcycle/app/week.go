package app

import (
	"context"
	"fmt"
	"time"
)

// ReportWeekOf trả tuần báo cáo (lịch kỳ hệ thống W11.03 — quy tắc chia kỳ có hiệu lực tại ngày day của tenant ctx)
// chứa ngày day: khóa "YYYY-Www" (cùng Span.Key của kỳ tuần), năm của số tuần, ngày đầu, ngày cuối. Bên đọc: pf/price
// (W11.76 — khóa tuần lưu lúc duyệt bảng giá) qua gốc lắp ráp.
func (s *Service) ReportWeekOf(ctx context.Context, day time.Time) (key string, year int, from, to time.Time) {
	w := s.periodRule(ctx, day).WeekOf(day)
	return fmt.Sprintf("%d-W%02d", w.Year, w.No), w.Year, w.From, w.To
}
