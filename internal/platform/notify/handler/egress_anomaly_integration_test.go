package handler_test

import (
	"context"
	"strings"
	"testing"
	"time"

	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
)

// ADR-0024 (L-08, X-06; mục 18 DLP): pf.metering.egress.anomaly_detected → consumer
// pf.notify.egress_anomaly → thông báo khẩn cho người nhận DLP của tenant CHỦ DỮ LIỆU (tenant phong
// bì): metering_viewer + platform_admin (tạm, chờ khách Q5-5). Cán bộ thường không nhận; người của
// tenant khác (vd tenant đơn vị vận hành / bên thứ ba) không nhận. Giao lặp không báo hai lần (R-61).
// Nội dung chỉ có kênh, sản phẩm, loại bên nhận, số bản ghi, ngưỡng — không dữ liệu cá nhân.
func TestEgressAnomalyNotifiesDLPRecipients_ADR0024(t *testing.T) {
	f := newFixture(t)
	viewer := f.user(authz.Scope{All: true}, meteringport.RoleMeteringViewer)
	admin := f.user(authz.Scope{All: true}, authz.RolePlatformAdmin)
	officer := f.user(authz.Scope{All: true}, partyport.RoleKNOfficer)
	other := f.userIn(ids.New(), ids.New(), authz.Scope{All: true}, meteringport.RoleMeteringViewer, authz.RolePlatformAdmin)
	egressID, client := ids.New(), ids.New()
	ev := f.event(f.tenant, meteringport.EventEgressAnomaly, meteringport.EgressAnomalyV1{
		EgressEventID: egressID.String(), Channel: string(meter.ChannelPublicAPI), Product: "collection_volume",
		CounterpartyKind: string(meter.CounterpartyAPIClient), CounterpartyRef: client.String(), BusinessDate: "2026-10-02",
		DailyRecords: 60000, Threshold: 50000, Basis: "absolute", Mode: string(meter.ModeEnforce), DetectedAt: f.clk.Now().Format(time.RFC3339)})
	if !f.process(port.ConsumerEgressAnomaly, ev) {
		t.Fatal("lần giao đầu phải được xử lý")
	}
	if f.process(port.ConsumerEgressAnomaly, ev) {
		t.Fatal("giao lặp phải bị inbox bỏ qua (R-61)")
	}
	for name, c := range map[string]caller{"metering_viewer": viewer, "platform_admin": admin} {
		in := c.inbox()
		if len(in) != 1 || in[0]["kind"] != port.KindEgressAnomaly || in[0]["severity"] != port.SeverityUrgent || in[0]["topic"] != true ||
			in[0]["deep_link"] != "/metering/egress/anomalies" {
			t.Fatalf("%s nhận đúng một cảnh báo DLP: %v", name, in)
		}
		body := in[0]["body"].(string)
		if !strings.Contains(body, "public_api") || !strings.Contains(body, "60000") || !strings.Contains(body, "50000") ||
			!strings.Contains(body, "chế độ chặn") || strings.Contains(body, client.String()) {
			t.Fatalf("%s: nội dung cảnh báo (chỉ mã kênh/số, không mã bên nhận): %q", name, body)
		}
	}
	if len(officer.inbox()) != 0 {
		t.Fatal("cán bộ khuyến nông không nhận cảnh báo DLP")
	}
	if len(other.inbox()) != 0 {
		t.Fatal("tenant khác không nhận cảnh báo DLP của tenant chủ dữ liệu")
	}
	// Payload hỏng → lỗi (đi DLQ sau giới hạn giao lại, R-62), không thông báo.
	bad := f.event(f.tenant, meteringport.EventEgressAnomaly, "không phải đối tượng")
	if _, err := inbox.Process(context.Background(), f.pool, port.ConsumerEgressAnomaly, bad, f.mod.EventHandler()); err == nil {
		t.Fatal("payload hỏng phải trả lỗi")
	}
}
