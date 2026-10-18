package notify_test

import (
	"strings"
	"testing"
	"time"

	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// GIS US-SYNC-02 AC-1: sự kiện đồng bộ vào DLQ → thông báo khẩn cho Admin nền tảng (chủ đề
// toàn quốc, vai platform_admin); cán bộ thường không nhận. Giao lặp không báo hai lần.
// CGH FN-07 AC-02/AC-03: dữ liệu App HTX quá ngưỡng độ tươi → Admin CGH.
func TestIntegrationAlertsNotifyAdmin_GIS_US_SYNC_02_CGH_FN07(t *testing.T) {
	f := newFixture(t)
	admin := f.user(authz.Scope{All: true}, authz.RolePlatformAdmin)
	cgh := f.user(authz.Scope{All: true}, mechport.RoleCGHAdmin)
	off := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	failure := ids.New()
	ev := f.event(f.tenant, syncport.EventIntegrationDeadLettered, syncport.IntegrationDeadLetteredV1{
		FailureID: failure, Consumer: "pf.mechanization.area_sync", Source: "App HTX → Bản đồ CGH (diện tích vụ)", EventID: ids.New(),
		EventType: "pf.cropcycle.coop_season_area.changed", ErrorCode: "validation_failed", DeliveryCount: 5,
		RecordedAt: f.clk.Now().Format(time.RFC3339)})
	f.process(port.ConsumerIntegration, ev)
	f.process(port.ConsumerIntegration, ev)
	in := admin.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindIntegrationDeadLettered || in[0]["severity"] != "urgent" || in[0]["topic"] != true ||
		!strings.Contains(in[0]["body"].(string), "6 lần thử") || !strings.Contains(in[0]["body"].(string), "validation_failed") {
		t.Fatalf("Admin nhận cảnh báo DLQ: %v", in)
	}
	if len(off.inbox()) != 0 || len(cgh.inbox()) != 0 {
		t.Fatal("chỉ Admin nền tảng nhận cảnh báo DLQ")
	}
	f.process(port.ConsumerIntegration, f.event(f.tenant, mechport.EventAreaFeedStale, mechport.AreaFeedStaleV1{
		LastSyncAt: f.clk.Now().Add(-72 * time.Hour).Format(time.RFC3339), ThresholdHours: "48", AlertDate: "2026-09-30",
		DetectedAt: f.clk.Now().Format(time.RFC3339)}))
	if in := cgh.inbox(); len(in) != 1 || in[0]["kind"] != port.KindAreaFeedStale || !strings.Contains(in[0]["body"].(string), "48 giờ") {
		t.Fatalf("Admin CGH nhận cảnh báo độ tươi: %v", in)
	}
}
