// Package domain: thực thể và quy tắc của pf/masterdata — đơn vị hành chính có
// hiệu lực theo thời gian, danh mục dùng chung, tham số có phiên bản và phê duyệt.
// Không phụ thuộc hạ tầng; thời gian nhận qua tham số (R-12).
package domain

import (
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// FieldError gắn lỗi với tên trường — handler ánh xạ sang problem theo trường.
type FieldError = port.FieldError

// Day là ngày nghiệp vụ (giờ Việt Nam) biểu diễn bằng time.Time 00:00 UTC — đúng
// với cách PostgreSQL trả cột date. Không mang ý nghĩa thời điểm.
func Day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// DayOf trả ngày nghiệp vụ của thời điểm t theo giờ Việt Nam.
func DayOf(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return Day(d.Year, d.Month, d.Day)
}

// DayStart trả thời điểm UTC bắt đầu ngày nghiệp vụ d (00:00 giờ Việt Nam).
func DayStart(d time.Time) time.Time {
	return clock.StartOfBusinessDate(clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
}

// ParseDay đọc ngày dạng YYYY-MM-DD.
func ParseDay(s string) (time.Time, error) {
	return time.Parse(time.DateOnly, s)
}
