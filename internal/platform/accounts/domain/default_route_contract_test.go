package domain_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// FN-CH-00.01 / KN-fix: default_route của ngữ cảnh phiên (P1) = trang mặc định của menu P2 = bản giả lập
// web-ops — cả ba kiểm với ca vàng do pf/party sinh (TestWebMenuFixture).
func TestFN_CH_00_01_DefaultRouteMatchesMenuContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../apps/web-ops/src/api/mock/fixtures/kn-menu.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		DefaultRoutes map[string]string `json:"default_routes"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{port.KNTW, port.KNTinh, port.KNXa, port.KNAdmin} {
		if got, want := domain.DefaultRoute(role), fx.DefaultRoutes[role]; got != want || want == "" {
			t.Errorf("%s: P1 default_route %q, menu P2 %q", role, got, want)
		}
	}
}
