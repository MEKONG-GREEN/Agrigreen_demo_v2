package app

import (
	"context"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Khóa rỗng = kỳ vừa kết thúc, tính theo NGÀY NGHIỆP VỤ VN (KN-VN-DATE) — Clock cố định: cuối tháng 28/29/30/31, tháng 2
// năm nhuận 2028 và năm thường, biên quý/năm, 00:00–07:00 giờ VN (UTC vẫn là ngày hôm trước).
func TestBM11DefaultPeriodAtBoundaries(t *testing.T) {
	vn := func(y int, m time.Month, d, h, mi int) time.Time {
		return time.Date(y, m, d, h, mi, 0, 0, clock.Zone())
	}
	utc := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, time.UTC) }
	cases := []struct {
		name string
		at   time.Time
		typ  string
		want string
	}{
		{"28/10 đối chứng", vn(2026, 10, 28, 10, 0), domain.BM11Month, "2026-09"},
		{"30/10", vn(2026, 10, 30, 10, 0), domain.BM11Month, "2026-09"},
		{"31/10 (tháng 9 có 30 ngày)", vn(2026, 10, 31, 10, 0), domain.BM11Month, "2026-09"},
		{"31/12 (tháng 11 có 30 ngày)", vn(2026, 12, 31, 23, 59), domain.BM11Month, "2026-11"},
		{"31/01 qua năm", vn(2027, 1, 31, 8, 0), domain.BM11Month, "2026-12"},
		{"01/01 qua năm", vn(2027, 1, 1, 0, 0), domain.BM11Month, "2026-12"},
		{"28/02 năm thường", vn(2027, 2, 28, 10, 0), domain.BM11Month, "2027-01"},
		{"28/03 năm thường", vn(2027, 3, 28, 10, 0), domain.BM11Month, "2027-02"},
		{"29/03 năm thường", vn(2027, 3, 29, 10, 0), domain.BM11Month, "2027-02"},
		{"30/03 năm thường", vn(2027, 3, 30, 10, 0), domain.BM11Month, "2027-02"},
		{"31/03 năm thường", vn(2027, 3, 31, 10, 0), domain.BM11Month, "2027-02"},
		{"29/02 năm nhuận", vn(2028, 2, 29, 10, 0), domain.BM11Month, "2028-01"},
		{"29/03 năm nhuận", vn(2028, 3, 29, 10, 0), domain.BM11Month, "2028-02"},
		{"30/03 năm nhuận", vn(2028, 3, 30, 10, 0), domain.BM11Month, "2028-02"},
		{"31/03 năm nhuận", vn(2028, 3, 31, 10, 0), domain.BM11Month, "2028-02"},
		{"31/05 (tháng 4 có 30 ngày)", vn(2027, 5, 31, 10, 0), domain.BM11Month, "2027-04"},
		{"31/07 (tháng 6 có 30 ngày)", vn(2027, 7, 31, 10, 0), domain.BM11Month, "2027-06"},
		{"00:00 01/11 VN = 17:00 31/10 UTC", utc(2026, 10, 31, 17, 0), domain.BM11Month, "2026-10"},
		{"23:59 31/10 VN = 16:59 31/10 UTC", utc(2026, 10, 31, 16, 59), domain.BM11Month, "2026-09"},
		{"06:59 01/12 VN = 23:59 30/11 UTC", utc(2026, 11, 30, 23, 59), domain.BM11Month, "2026-11"},
		{"07:00 01/03/2028 VN = 00:00 01/03 UTC", utc(2028, 3, 1, 0, 0), domain.BM11Month, "2028-02"},
		{"06:00 01/03/2028 VN = 23:00 29/02 UTC", utc(2028, 2, 29, 23, 0), domain.BM11Month, "2028-02"},

		{"quý: 31/05", vn(2027, 5, 31, 10, 0), domain.BM11Quarter, "2027-Q1"},
		{"quý: 31/08", vn(2026, 8, 31, 10, 0), domain.BM11Quarter, "2026-Q2"},
		{"quý: 30/11", vn(2026, 11, 30, 10, 0), domain.BM11Quarter, "2026-Q3"},
		{"quý: 01/01 qua năm", vn(2027, 1, 1, 0, 0), domain.BM11Quarter, "2026-Q4"},
		{"quý: 31/03 năm nhuận", vn(2028, 3, 31, 23, 59), domain.BM11Quarter, "2027-Q4"},
		{"quý: 01:00 01/04 VN = 18:00 31/03 UTC", utc(2027, 3, 31, 18, 0), domain.BM11Quarter, "2027-Q1"},
		{"quý: 00:00 01/01/2027 VN = 17:00 31/12 UTC", utc(2026, 12, 31, 17, 0), domain.BM11Quarter, "2026-Q4"},
		{"quý: 23:59 31/12 VN = 16:59 31/12 UTC", utc(2026, 12, 31, 16, 59), domain.BM11Quarter, "2026-Q3"},

		{"năm: 00:00 01/01/2027 VN", utc(2026, 12, 31, 17, 0), domain.BM11Year, "2026"},
		{"năm: 23:59 31/12/2026 VN", utc(2026, 12, 31, 16, 59), domain.BM11Year, "2025"},
		{"năm: 29/02/2028", vn(2028, 2, 29, 10, 0), domain.BM11Year, "2027"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bm11PrevKey(tc.typ, tc.at); got != tc.want {
				t.Fatalf("bm11PrevKey(%s, %s) = %s, muốn %s", tc.typ, tc.at.UTC().Format(time.RFC3339), got, tc.want)
			}
			s := Service{Clock: clock.NewFixed(tc.at)}
			p, err := s.bm11Period(context.Background(), tc.typ, "")
			if err != nil {
				t.Fatal(err)
			}
			if p.Key != tc.want {
				t.Fatalf("bm11Period(%s, \"\") tại %s = %s, muốn %s", tc.typ, tc.at.UTC().Format(time.RFC3339), p.Key, tc.want)
			}
			// Kỳ mặc định phải kết thúc trước ngày nghiệp vụ hiện tại.
			today := tc.at.In(clock.Zone())
			if !p.To.Before(domain.Date(today.Year(), today.Month(), today.Day())) {
				t.Fatalf("kỳ %s (đến %s) chưa kết thúc tại %s", p.Key, p.To.Format("02/01/2006"), today.Format("02/01/2006 15:04"))
			}
		})
	}
}
