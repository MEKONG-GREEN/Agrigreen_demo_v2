package notify_test

// BF-NOTIFY-KN-ADAPTER (#121): renderer CNTS của KN nối cổng kênh pf/notify trên PostgreSQL thật.
//   - Nhắc theo cấu hình biểu (#108, channels_explicit): chỉ "in_app" → hộp thư, KHÔNG push; in_app+push → push; email chỉ
//     người nhận cụ thể.
//   - Sự kiện nhắc cũ (không đánh dấu) có channels ["in_app"] → hành vi cũ: hộp thư + push.
//   - Cảnh báo Khẩn ổ dịch (#111): hộp thư + push Khẩn + email người nhận cụ thể; tenant khác không nhận.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// cntsQueues đếm hàng đợi push/email của thông báo sinh từ sự kiện nguồn.
func cntsQueues(t *testing.T, f *fixture, event uuid.UUID) (inApp bool, pushes, urgent, emails int) {
	t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(context.Background(), "SELECT id,in_app FROM pf_notify.notification WHERE source_event_id=$1", event).Scan(&id, &inApp); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), "SELECT count(*), count(*) FILTER (WHERE urgent) FROM pf_notify.push_delivery WHERE notification_id=$1", id).Scan(&pushes, &urgent); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.email_delivery WHERE notification_id=$1", id).Scan(&emails)
	}); err != nil {
		t.Fatal(err)
	}
	return
}

func TestCNTSReminderAndOutbreakChannels_BF_NOTIFY_KN_ADAPTER(t *testing.T) {
	f := newFixture(t)
	reminder := func(a caller, chs []string, explicit bool) uuid.UUID {
		t.Helper()
		p := extport.CNTSFormRemindedV1{PeriodRef: ids.New(), TemplateCode: "CNTS-BM04", PeriodLabel: "Tuần 44", Unit: ward1, CommuneCode: ward1,
			Slot: extport.CNTSReminderBefore, DueAt: f.clk.Now().Add(24 * time.Hour).Format(time.RFC3339), Message: "Nhắc nộp BM-04 kỳ Tuần 44",
			Channels: chs, ChannelsExplicit: explicit, RecipientRefs: []uuid.UUID{a.actor}, Department: "chan_nuoi_thuy_san",
			EventTime: f.clk.Now().Format(time.RFC3339)}
		e := f.event(f.tenant, extport.EventCNTSFormReminded, p)
		if !f.process(extport.NotifyConsumerCNTS, e) || f.process(extport.NotifyConsumerCNTS, e) {
			t.Fatal("nhắc: inbox không idempotent")
		}
		return e.EventID
	}
	cases := []struct {
		name      string
		chs       []string
		explicit  bool
		push, eml int
	}{
		{"explicit_in_app_only_no_push", []string{"in_app"}, true, 0, 0},
		{"explicit_push", []string{"in_app", "push"}, true, 1, 0},
		{"explicit_email_no_push", []string{"in_app", "email"}, true, 0, 1},
		{"legacy_unmarked_in_app_pushes", []string{"in_app"}, false, 1, 0},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
			a.device("cnts-channel-token-" + c.name)
			f.emails.set(a.actor, "cnts-"+c.name+"@example.invalid")
			ev := reminder(a, c.chs, c.explicit)
			inApp, pushes, _, emails := cntsQueues(t, f, ev)
			if !inApp || pushes != c.push || emails != c.eml || len(a.inbox()) != 1 {
				t.Fatalf("#%d %s: in_app=%v push=%d email=%d inbox=%d", i, c.name, inApp, pushes, emails, len(a.inbox()))
			}
		})
	}

	officer := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	officer.device("cnts-outbreak-push-token")
	f.emails.set(officer.actor, "cnts-outbreak@example.invalid")
	foreign := f.asActor(ids.New(), ids.New(), officer.actor, partyport.RoleKNProvinceAdmin)
	alert := extport.CNTSAlertChangedV1{AlertRef: ids.New(), Action: "raised", Kind: "outbreak", Level: "urgent",
		Title: "Ổ dịch mới · bệnh LMLM trên lon · xã " + ward1 + " · đàn 120 con, chết 3", ProvinceCode: stringRef(prov91),
		RecipientRefs: []uuid.UUID{officer.actor}, DueAt: f.clk.Now().Add(24 * time.Hour).Format(time.RFC3339), EventTime: f.clk.Now().Format(time.RFC3339)}
	e := f.event(f.tenant, extport.EventCNTSAlertChanged, alert)
	if !f.process(extport.NotifyConsumerCNTS, e) || f.process(extport.NotifyConsumerCNTS, e) {
		t.Fatal("ổ dịch: inbox không idempotent")
	}
	inApp, pushes, urgent, emails := cntsQueues(t, f, e.EventID)
	got := officer.inbox()
	if !inApp || pushes != 1 || urgent != 1 || emails != 1 || len(got) != 1 || got[0]["severity"] != "urgent" || len(foreign.inbox()) != 0 {
		t.Fatalf("Khẩn ổ dịch: in_app=%v push=%d urgent=%d email=%d inbox=%v foreign=%d", inApp, pushes, urgent, emails, got, len(foreign.inbox()))
	}
}
