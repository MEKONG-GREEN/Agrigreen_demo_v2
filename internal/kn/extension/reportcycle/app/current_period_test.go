package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
)

// KN-VN-DATE (#149): from_date là ngày lịch VN (cột DATE, pgx quét thành 00:00 UTC = 07:00 VN). Kỳ tuần mới phải bắt đầu
// từ 00:00 VN Thứ Hai, không phải 07:00 VN.
var (
	// Kỳ tuần trước (Thứ Hai 19/10 – Chủ Nhật 25/10/2026) và kỳ tuần này (Thứ Hai 26/10 – Chủ Nhật 01/11/2026).
	prevWeek = store.Period{ID: uuid.MustParse("00000000-0000-0000-0000-000000000019"), Slot: 1,
		From: domain.Date(2026, 10, 19), To: domain.Date(2026, 10, 25)}
	thisWeek = store.Period{ID: uuid.MustParse("00000000-0000-0000-0000-000000000026"), Slot: 2,
		From: domain.Date(2026, 10, 26), To: domain.Date(2026, 11, 1)}

	monday0030VN  = time.Date(2026, 10, 25, 17, 30, 0, 0, time.UTC)  // Thứ Hai 26/10 00:30 giờ VN
	monday0000VN  = time.Date(2026, 10, 25, 17, 0, 0, 0, time.UTC)   // Thứ Hai 26/10 00:00 giờ VN (mốc biên)
	monday0700VN  = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)    // Thứ Hai 26/10 07:00 giờ VN
	sunday2359VN  = time.Date(2026, 10, 25, 16, 59, 59, 0, time.UTC) // Chủ Nhật 25/10 23:59:59 giờ VN
	sunday2359VNb = time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC)  // Chủ Nhật 25/10 23:59 giờ VN
)

func TestCurrentPeriodUsesVietnamDate(t *testing.T) {
	ps := []store.Period{prevWeek, thisWeek} // PeriodsOfBatch: theo slot
	for _, tc := range []struct {
		name string
		now  time.Time
		want store.Period
	}{
		{"Thứ Hai 00:30 VN → tuần mới", monday0030VN, thisWeek},
		{"Thứ Hai 00:00 VN → tuần mới", monday0000VN, thisWeek},
		{"Thứ Hai 07:00 VN → tuần mới", monday0700VN, thisWeek},
		{"Chủ Nhật 23:59 VN → tuần trước", sunday2359VNb, prevWeek},
		{"Chủ Nhật 23:59:59 VN → tuần trước", sunday2359VN, prevWeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := currentPeriod(ps, tc.now); got.ID != tc.want.ID {
				t.Fatalf("currentPeriod(%s) = kỳ %s, muốn %s", tc.now.Format(time.RFC3339), got.From.Format(time.DateOnly),
					tc.want.From.Format(time.DateOnly))
			}
		})
	}
}

func TestDefaultDashboardPeriodUsesVietnamDate(t *testing.T) {
	ps := []store.Period{thisWeek, prevWeek} // ListPeriods: from_date giảm dần
	for _, tc := range []struct {
		name         string
		now          time.Time
		consolidated map[uuid.UUID]bool
		want         store.Period
	}{
		{"Thứ Hai 00:30 VN, tuần mới chưa tổng hợp → tuần mới (kỳ gần nhất đã bắt đầu)", monday0030VN,
			map[uuid.UUID]bool{}, thisWeek},
		{"Thứ Hai 00:30 VN, tuần mới đã tổng hợp → tuần mới", monday0030VN,
			map[uuid.UUID]bool{thisWeek.ID: true, prevWeek.ID: true}, thisWeek},
		{"Thứ Hai 00:30 VN, chỉ tuần trước có tổng hợp → tuần trước", monday0030VN,
			map[uuid.UUID]bool{prevWeek.ID: true}, prevWeek},
		{"Chủ Nhật 23:59 VN → tuần trước (tuần mới chưa bắt đầu)", sunday2359VNb,
			map[uuid.UUID]bool{thisWeek.ID: true}, prevWeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := defaultDashboardPeriod(ps, tc.now, func(id uuid.UUID) (bool, error) { return tc.consolidated[id], nil })
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.ID != tc.want.ID {
				t.Fatalf("defaultDashboardPeriod(%s) = %+v, muốn kỳ %s", tc.now.Format(time.RFC3339), got,
					tc.want.From.Format(time.DateOnly))
			}
		})
	}
	// Chưa kỳ nào bắt đầu (Chủ Nhật 23:59 VN, chỉ có tuần mới) → nil ("Chưa có dữ liệu").
	got, err := defaultDashboardPeriod([]store.Period{thisWeek}, sunday2359VNb, func(uuid.UUID) (bool, error) { return true, nil })
	if err != nil || got != nil {
		t.Fatalf("chưa kỳ nào bắt đầu: got %+v, err %v; muốn nil", got, err)
	}
}
