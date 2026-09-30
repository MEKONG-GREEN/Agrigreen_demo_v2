package adapter

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// PlotSeasons cài đặt port.HarvestCalendar khi chưa cấu hình (đóng: Unavailable) — kiểm ngày
// gặt proto FM-01 vẫn cho ghi nhưng gắn cờ unavailable.
func (Unconfigured) PlotSeasons(context.Context, uuid.UUID) ([]port.SeasonDates, error) {
	return nil, unavailable("chưa cấu hình AG_PLATFORM_API_URL / tài khoản dịch vụ MKG")
}

// FakeHarvestCalendar là port.HarvestCalendar trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95).
type FakeHarvestCalendar struct {
	mu    sync.Mutex
	plots map[uuid.UUID][]port.SeasonDates
	// Down: mô phỏng nền tảng không gọi được.
	Down bool
	// Calls đếm số lần gọi.
	Calls int
}

// NewFakeHarvestCalendar tạo bản giả rỗng (mọi thửa: chưa chia sẻ ngày gặt → NotFound).
func NewFakeHarvestCalendar() *FakeHarvestCalendar {
	return &FakeHarvestCalendar{plots: map[uuid.UUID][]port.SeasonDates{}}
}

// Set đặt vụ của thửa (thay vụ cùng season_ref). expected / open là YYYY-MM-DD, open rỗng =
// chưa mở gặt.
func (f *FakeHarvestCalendar) Set(plot, season uuid.UUID, expected, open, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := port.SeasonDates{SeasonRef: season, PlotRef: plot, ExpectedHarvestDate: expected, HarvestOpenDate: open, Status: status}
	xs := f.plots[plot]
	for i, x := range xs {
		if x.SeasonRef == season {
			xs[i] = s
			return
		}
	}
	f.plots[plot] = append(xs, s)
}

// PlotSeasons cài đặt port.HarvestCalendar.
func (f *FakeHarvestCalendar) PlotSeasons(_ context.Context, plot uuid.UUID) ([]port.SeasonDates, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Down {
		return nil, port.ErrDirectoryUnavailable
	}
	xs, ok := f.plots[plot]
	if !ok {
		return nil, port.ErrDirectoryNotFound
	}
	return append([]port.SeasonDates(nil), xs...), nil
}
