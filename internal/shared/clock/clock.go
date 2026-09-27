// Package clock cung cấp thời gian cho toàn hệ thống (R-12).
//
// Cấm gọi time.Now() trực tiếp trong các package domain/ — nhận Clock qua
// tham số để kiểm thử được. Thời gian lưu trữ luôn là UTC; ngày nghiệp vụ
// tính theo giờ Việt Nam.
package clock

import (
	"sync"
	"time"
	_ "time/tzdata" // nhúng dữ liệu múi giờ: máy Windows và image distroless không có sẵn
)

// BusinessZone là múi giờ tính ngày nghiệp vụ.
const BusinessZone = "Asia/Ho_Chi_Minh"

var (
	zoneOnce sync.Once
	zone     *time.Location
)

// Zone trả về múi giờ Asia/Ho_Chi_Minh.
func Zone() *time.Location {
	zoneOnce.Do(func() {
		loc, err := time.LoadLocation(BusinessZone)
		if err != nil {
			panic("clock: không nạp được múi giờ " + BusinessZone + ": " + err.Error())
		}
		zone = loc
	})
	return zone
}

// Clock là nguồn thời gian được tiêm vào domain và app.
type Clock interface {
	Now() time.Time
}

// System là đồng hồ thật, luôn trả về UTC.
type System struct{}

// Now trả về thời điểm hiện tại theo UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed là đồng hồ cố định cho kiểm thử.
type Fixed struct {
	mu sync.Mutex
	t  time.Time
}

// NewFixed tạo đồng hồ cố định tại t.
func NewFixed(t time.Time) *Fixed { return &Fixed{t: t.UTC()} }

// Now trả về thời điểm đã đặt.
func (f *Fixed) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Advance dời đồng hồ đi d.
func (f *Fixed) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// Date là ngày nghiệp vụ (không có giờ), theo giờ Việt Nam.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// String trả về dạng YYYY-MM-DD.
func (d Date) String() string {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

// BusinessDate trả về ngày nghiệp vụ của thời điểm t theo giờ Việt Nam.
// Ví dụ 2026-12-02T18:30:00Z là ngày 2026-12-03 ở Việt Nam.
func BusinessDate(t time.Time) Date {
	y, m, d := t.In(Zone()).Date()
	return Date{Year: y, Month: m, Day: d}
}

// StartOfBusinessDate trả về thời điểm UTC bắt đầu ngày nghiệp vụ d.
func StartOfBusinessDate(d Date) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, Zone()).UTC()
}
