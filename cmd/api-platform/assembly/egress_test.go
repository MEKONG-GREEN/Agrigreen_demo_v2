package assembly

import (
	"testing"

	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
)

// ADR-0024 mục 2 (R-01): chế độ điểm kiểm soát xuất chọn bằng cấu hình AG_EGRESS_MODE; rỗng →
// observe (ĐTK 1); giá trị lạ → lỗi khởi động (không âm thầm chạy observe ở Phase K).
func TestEgressModeFromEnv_ADR0024(t *testing.T) {
	for v, want := range map[string]meteringport.Mode{"": meteringport.ModeObserve, "observe": meteringport.ModeObserve, "enforce": meteringport.ModeEnforce} {
		got, err := EgressModeFromEnv(func(k string) string {
			if k != meteringport.EnvEgressMode {
				t.Fatalf("biến môi trường lạ: %s", k)
			}
			return v
		})
		if err != nil || got != want {
			t.Fatalf("%q → %q %v, muốn %q", v, got, err, want)
		}
	}
	if _, err := EgressModeFromEnv(func(string) string { return "block" }); err == nil {
		t.Fatal("giá trị lạ phải lỗi khởi động")
	}
}
