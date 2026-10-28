package domain

import (
	"errors"
	"testing"
	"time"
)

// KN-VN-DATE (#149): member_since là ngày lịch VN (DATE, parse time.DateOnly = 00:00 UTC). "Không ở tương lai" phải so
// với ngày VN của now — nhập hôm nay lúc 00:00–07:00 VN không bị từ chối; ngày mai vẫn bị từ chối.
func TestMemberSinceFutureCheckUsesVietnamDate(t *testing.T) {
	day := func(d int) *time.Time {
		v := time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC)
		return &v
	}
	monday0030 := time.Date(2026, 10, 25, 17, 30, 0, 0, time.UTC) // Thứ Hai 26/10 00:30 VN
	sunday2359 := time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC) // Chủ Nhật 25/10 23:59 VN
	for _, tc := range []struct {
		name  string
		now   time.Time
		since *time.Time
		ok    bool
	}{
		{"Thứ Hai 00:30 VN, vào HTX hôm nay", monday0030, day(26), true},
		{"Thứ Hai 00:30 VN, vào HTX ngày mai", monday0030, day(27), false},
		{"Chủ Nhật 23:59 VN, vào HTX Thứ Hai", sunday2359, day(26), false},
		{"Chủ Nhật 23:59 VN, vào HTX Chủ Nhật", sunday2359, day(25), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateHousehold(nil, tc.since, tc.now)
			var fe FieldError
			switch {
			case tc.ok && err != nil:
				t.Fatalf("muốn hợp lệ, nhận %v", err)
			case !tc.ok && (!errors.As(err, &fe) || fe.Field != "member_since"):
				t.Fatalf("muốn lỗi member_since, nhận %v", err)
			}
		})
	}
}
