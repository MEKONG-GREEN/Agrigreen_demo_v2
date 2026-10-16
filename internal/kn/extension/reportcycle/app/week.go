package app

import (
	"context"
	"fmt"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

// ReportWeekOf trả tuần báo cáo (lịch kỳ hệ thống W11.03 — quy tắc chia kỳ có hiệu lực tại ngày day của tenant ctx)
// chứa ngày day: khóa "YYYY-Www" (cùng Span.Key của kỳ tuần), năm của số tuần, ngày đầu, ngày cuối. Bên đọc: pf/price
// (W11.76 — khóa tuần lưu lúc duyệt bảng giá) qua gốc lắp ráp. D8: theo lịch tuần của phòng Báo giá
// (domain.DeptThongTinTruyenThong — mặc định = lịch chung Thứ Tư–Thứ Ba từ 04/11/2026).
func (s *Service) ReportWeekOf(ctx context.Context, day time.Time) (key string, year int, from, to time.Time) {
	w := s.CalendarAt(ctx, domain.DeptThongTinTruyenThong, day).Rule.WeekOf(day)
	return fmt.Sprintf("%d-W%02d", w.Year, w.No), w.Year, w.From, w.To
}
