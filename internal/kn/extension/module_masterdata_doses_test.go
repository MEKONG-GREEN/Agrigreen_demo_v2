package extension

import (
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

func TestCNTS_ParseDoses(t *testing.T) {
	ds, err := catalogParseDoses(" VX_GUMBORO/2@28, VX_ND_F/1@3 ,VX_GUMBORO/1@7")
	if err != nil {
		t.Fatal(err)
	}
	if got := catalogFormatDoses(ds); got != "VX_ND_F/1@3,VX_GUMBORO/1@7,VX_GUMBORO/2@28" {
		t.Fatalf("sắp theo ngày tuổi: %s", got)
	}
	for _, bad := range []string{"", "VX/1", "VX@3", "VX/0@3", "VX/1@-1", "VX/1@4000", "V X/1@3", "VX/1@3,VX/1@5"} {
		if _, err := catalogParseDoses(bad); err == nil {
			t.Fatalf("%q phải lỗi", bad)
		}
	}
	d := port.Dose{Vaccine: "VX", Shot: 1, AgeDays: 17}
	if got := d.ExpectedDate(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 1); !got.Equal(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ngày dự kiến = nhận giống + ngày tuổi − tuổi lúc nhận: %v", got)
	}
}
