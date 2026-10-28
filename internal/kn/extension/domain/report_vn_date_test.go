package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// KN-VN-DATE (#149): sown_on là ngày lịch VN (DATE, 00:00 UTC). "Không ở tương lai" phải so với ngày VN của now — nhập
// ngày hôm nay lúc 00:00–07:00 VN không được bị từ chối, ngày mai vẫn bị từ chối.
func TestSownOnFutureCheckUsesVietnamDate(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	day := func(y int, m time.Month, d int) *time.Time {
		v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return &v
	}
	for _, tc := range []struct {
		name   string
		now    time.Time
		sownOn *time.Time
		ok     bool
	}{
		{"Thứ Hai 00:30 VN, sạ hôm nay", time.Date(2026, 10, 25, 17, 30, 0, 0, time.UTC), day(2026, 10, 26), true},
		{"Thứ Hai 00:30 VN, sạ ngày mai", time.Date(2026, 10, 25, 17, 30, 0, 0, time.UTC), day(2026, 10, 27), false},
		{"Chủ Nhật 23:59 VN, sạ Thứ Hai", time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC), day(2026, 10, 26), false},
		{"Chủ Nhật 23:59 VN, sạ Chủ Nhật", time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC), day(2026, 10, 25), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			area := decimal.RequireFromString("1.5")
			r := Report{Kind: KindSowing, CommuneCode: "30280", Period: "2026-10", CooperativeRef: uuid.New(),
				SeasonCode: "DX", VarietyCode: "OM5451", SownAreaHa: &area, SownOn: tc.sownOn}
			err := r.Validate(tc.now, zone)
			var fe FieldError
			switch {
			case tc.ok && err != nil:
				t.Fatalf("muốn hợp lệ, nhận %v", err)
			case !tc.ok && (!errors.As(err, &fe) || fe.Field != "sown_on"):
				t.Fatalf("muốn lỗi sown_on, nhận %v", err)
			}
		})
	}
}
