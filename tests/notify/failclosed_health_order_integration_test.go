package notify_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// P0-NOTIFY-FAILCLOSED (#127, Codex P2): bản gửi tạo sớm (T0) có thể được xử lý MUỘN hơn một lô mới đã gửi thành công
// (T1). Khi kênh bị tắt rồi lô cũ mới được thử (T2) → dropped. Tình trạng kênh phải dựa vào thời điểm thử/kết quả
// (finished_at, ghi cùng giao dịch Finish*Delivery), không phải created_at — lần thử mới nhất bị tắt ⇒ "disabled" +
// cảnh báo. Ca tái hiện chuyển từ test reviewer của Codex (TestReviewerHealthUsesDisabledAttemptOrder127).
func TestDeliveryHealthUsesAttemptTime_P0NotifyFailClosed(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	a.device(fcmToken)
	ctx := context.Background()
	batchOf := func(source uuid.UUID) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT d.batch_id FROM pf_notify.push_delivery d
			  JOIN pf_notify.notification n ON n.tenant_id = d.tenant_id AND n.id = d.notification_id
			  WHERE n.source_event_id = $1`, source).Scan(&id)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	deliver := func(id uuid.UUID) {
		t.Helper()
		e := f.event(f.tenant, port.EventPushRequested, port.PushRequestedV1{BatchID: id, Kind: "x", Deliveries: 1})
		if _, err := inbox.Process(ctx, f.appDB, port.ConsumerPush, e, f.mod.PushHandler()); err != nil {
			t.Fatal(err)
		}
	}

	// T0: lô cũ được tạo, chưa xử lý (pending).
	old, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, old)
	oldBatch := batchOf(old.EventID)
	// T1: lô mới tạo và gửi thành công (kênh giả của fixture).
	f.clk.Advance(time.Minute)
	newer, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, newer)
	deliver(batchOf(newer.EventID))
	// T2: kênh đẩy TẮT (thiếu FCM), lô cũ mới được thử → dropped.
	f.clk.Advance(time.Minute)
	f.useChannels(map[string]string{"AG_ENV": "staging", "AG_DEPLOY_ENV": "demo", "AG_WEB_OPS_URL": "https://app.demo.agrigreen.vn"})
	deliver(oldBatch)

	rows := f.deliveries("pf_notify.push_delivery")
	if len(rows) != 2 || rows[0].status != "dropped" || rows[0].lastError != "channel_disabled:fcm_not_configured" || rows[1].status != "sent" {
		t.Fatalf("lịch sử kiểm toán không đúng: %+v", rows)
	}
	admin := f.user(authz.Scope{Units: []string{ward1}}, authz.KNAdmin)
	r := admin.do(f.admin.URL, http.MethodGet, "/v1/notify/delivery-health", "", nil)
	if r.status != http.StatusOK {
		t.Fatalf("delivery-health HTTP=%d body=%+v", r.status, r.body)
	}
	h, _ := r.body["push"].(map[string]any)
	if h["status"] != "disabled" || h["warning"] == nil || h["disabled_reason"] != "fcm_not_configured" {
		t.Fatalf("lần thử mới nhất bị tắt nhưng delivery-health báo khác: %+v", h)
	}
	// Mốc tắt là thời điểm thử T2 (sau lần gửi thành công T1), không phải created_at T0.
	at, _ := time.Parse(time.RFC3339Nano, h["last_disabled_at"].(string))
	sent, _ := time.Parse(time.RFC3339Nano, h["last_sent_at"].(string))
	if !at.After(sent) {
		t.Fatalf("last_disabled_at=%v phải sau last_sent_at=%v", at, sent)
	}
}
