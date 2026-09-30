package adapter

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

var _ port.HarvestDates = (*PlatformHTTP)(nil)

// SeasonHarvestDates gọi GET /v1/partner/season-harvest-dates?plot_ref=… (bề mặt partner của
// nền tảng, ADR-0020) bằng tài khoản dịch vụ tenant MKG. 404/403 (chưa chia sẻ, RB-2) →
// ErrDirectoryNotFound; dòng hỏng (ngày sai dạng, kind lạ) bị bỏ qua thay vì làm hỏng cả trang.
func (c *PlatformHTTP) SeasonHarvestDates(ctx context.Context, plotRef uuid.UUID) ([]port.SeasonHarvest, error) {
	var body struct {
		Items []struct {
			PlotRef     uuid.UUID  `json:"plot_ref"`
			SeasonRef   *uuid.UUID `json:"season_ref"`
			HarvestDate string     `json:"harvest_date"`
			Kind        string     `json:"kind"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/v1/partner/season-harvest-dates?plot_ref="+url.QueryEscape(plotRef.String()), &body); err != nil {
		return nil, err
	}
	out := make([]port.SeasonHarvest, 0, len(body.Items))
	for _, it := range body.Items {
		d, err := time.Parse(time.DateOnly, it.HarvestDate)
		if err != nil || (it.Kind != port.HarvestExpected && it.Kind != port.HarvestActual) {
			continue
		}
		if it.PlotRef != uuid.Nil && it.PlotRef != plotRef {
			continue
		}
		out = append(out, port.SeasonHarvest{PlotRef: plotRef, SeasonRef: it.SeasonRef, HarvestDate: d, Kind: it.Kind})
	}
	return out, nil
}

// SeasonHarvestDates cài đặt port.HarvestDates khi chưa cấu hình (đóng: Unavailable).
func (Unconfigured) SeasonHarvestDates(context.Context, uuid.UUID) ([]port.SeasonHarvest, error) {
	return nil, unavailable("chưa cấu hình AG_PLATFORM_API_URL / tài khoản dịch vụ MKG")
}

// FakeHarvestDates là HarvestDates trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95).
type FakeHarvestDates struct {
	mu    sync.RWMutex
	plots map[uuid.UUID][]port.SeasonHarvest
	// Down: mô phỏng nền tảng không gọi được.
	Down bool
	// Calls đếm số lần gọi.
	Calls int
}

// NewFakeHarvestDates tạo bản giả rỗng (mọi thửa: chưa biết ngày gặt).
func NewFakeHarvestDates() *FakeHarvestDates {
	return &FakeHarvestDates{plots: map[uuid.UUID][]port.SeasonHarvest{}}
}

// Set đặt ngày gặt (YYYY-MM-DD) của một vụ cho thửa; season nil = vụ không mã.
func (f *FakeHarvestDates) Set(plot uuid.UUID, season *uuid.UUID, date, kind string) {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		panic(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	xs := f.plots[plot]
	for i, x := range xs {
		if (x.SeasonRef == nil && season == nil) || (x.SeasonRef != nil && season != nil && *x.SeasonRef == *season) {
			xs[i].HarvestDate, xs[i].Kind = d, kind
			return
		}
	}
	f.plots[plot] = append(xs, port.SeasonHarvest{PlotRef: plot, SeasonRef: season, HarvestDate: d, Kind: kind})
}

// SeasonHarvestDates cài đặt port.HarvestDates.
func (f *FakeHarvestDates) SeasonHarvestDates(_ context.Context, plot uuid.UUID) ([]port.SeasonHarvest, error) {
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
	return append([]port.SeasonHarvest(nil), xs...), nil
}
