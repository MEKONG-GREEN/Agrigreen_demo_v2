package notify_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// useChannels dựng lại module của fixture với kênh thật từ notify.ChannelsFromEnvWith (như cmd/worker) thay kênh giả.
func (f *fixture) useChannels(env map[string]string) notify.Channels {
	f.t.Helper()
	ch, err := notify.ChannelsFromEnvWith(func(k string) string { return env[k] }, slog.New(slog.NewTextHandler(f.logs, nil)))
	if err != nil {
		f.t.Fatal(err)
	}
	appPool := pgtest.Get(f.t).Pool(f.t, migrations.PF, dbadmin.AppPlatform)
	f.mod = notify.New(appPool, f.clk, notify.Deps{Kinds: []port.KindRegistration{extension.NotificationKinds(), price.NotificationKinds()},
		Digests:   []port.DigestRegistration{extension.NotificationDigest()},
		Builders:  append(append(price.NotificationBuilders(f.desk), extension.TaskNotificationBuilders(f.staff)...), extension.DirectiveNotificationBuilders(f.staff, fakeUnits{})...),
		Renderers: extension.NotificationRenderers(), Consumers: []port.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers()},
		Scope: f.scope, Devices: f.devices, Staff: f.staff, Units: fakeUnits{},
		Pusher: ch.Pusher, Mailer: ch.Mailer, EmailMailer: ch.EmailMailer, LinkBase: ch.LinkBase, Emails: f.emails,
		Log: slog.New(slog.NewTextHandler(f.logs, nil))})
	return ch
}

type deliveryRow struct {
	status, lastError string
	attempts          int
}

func (f *fixture) deliveries(table string) []deliveryRow {
	f.t.Helper()
	var out []deliveryRow
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT status, coalesce(last_error, ''), attempts FROM `+table+` ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r deliveryRow
			if err := rows.Scan(&r.status, &r.lastError, &r.attempts); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// P0-NOTIFY-FAILCLOSED (#127): máy demo/VPS không cấu hình SMTP/FCM — email theo người nhận và thông điệp đẩy được
// ghi "dropped" với lý do channel_disabled:<lý do>, KHÔNG BAO GIỜ "sent"; không thử lại (consumer không trả lỗi); quản
// trị thấy cảnh báo ở GET /v1/notify/delivery-health; vai thường bị từ chối.
func TestDeliveriesWithoutSMTPOrFCMAreRecordedDisabled_P0NotifyFailClosed(t *testing.T) {
	f := newFixture(t)
	ch := f.useChannels(map[string]string{"AG_ENV": "staging", "AG_DEPLOY_ENV": "demo", "AG_WEB_OPS_URL": "https://app.demo.agrigreen.vn"})
	if !ch.Email.Disabled() || !ch.Push.Disabled() || ch.Mailer != nil {
		t.Fatalf("kênh phải tắt: email=%+v push=%+v", ch.Email, ch.Push)
	}

	// Đẩy: cán bộ có thiết bị + token, chỉ đạo khẩn → lần gửi bị bỏ (fcm_not_configured), không gọi kênh đẩy nào.
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	a.device(fcmToken)
	env, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatalf("kênh tắt không được thử lại: %v", err)
	}
	push := f.deliveries("pf_notify.push_delivery")
	if len(push) != 1 || push[0].status != "dropped" || push[0].lastError != "channel_disabled:fcm_not_configured" || push[0].attempts != 1 {
		t.Fatalf("lần gửi đẩy phải dropped/channel_disabled:fcm_not_configured: %+v", push)
	}
	if n := len(f.pusher.messages()); n != 0 {
		t.Fatalf("không được gửi thông điệp đẩy: %d", n)
	}

	// Email: yêu cầu xác minh giá → CTV có email → bản gửi bị bỏ (smtp_not_configured), không gửi, không thử lại.
	sc := authz.Scope{Units: []string{ward1}}
	ctv := f.user(sc, authz.KNXa)
	owner := f.user(sc, authz.KNTinh)
	f.desk.officers = []uuid.UUID{owner.actor}
	f.emails.set(ctv.actor, "nguyen.van.ctv@mekong.vn")
	req := f.event(f.tenant, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn", Day: "2026-10-24",
		PointCode: "XBG0001", PointName: "Mỹ Chánh", TicketID: uuid.New(), ItemCodes: []string{"MH00001"},
		RespondBy: "2026-10-24T06:30:00Z", OfficerRef: &ctv.actor, RequesterRef: owner.actor})
	f.process(priceport.NotifyConsumerPrice, req)
	if rows := f.emailRows(priceport.KindPriceVerifyRequest); len(rows) != 1 || rows[ctv.actor].Status != "pending" {
		t.Fatalf("bản gửi email được xếp (để ghi đúng kết quả): %+v", rows)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatalf("kênh tắt không được thử lại: %v", err)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	mail := f.deliveries("pf_notify.email_delivery")
	if len(mail) != 1 || mail[0].status != "dropped" || mail[0].lastError != "channel_disabled:smtp_not_configured" || mail[0].attempts != 1 {
		t.Fatalf("email phải dropped/channel_disabled:smtp_not_configured, không sent: %+v", mail)
	}
	if m := f.mailer.to("nguyen.van.ctv@mekong.vn"); len(m) != 0 {
		t.Fatalf("không được gửi email: %+v", m)
	}
	if k := kindsOf(ctv.inbox()); len(k) != 1 {
		t.Fatalf("thông báo trong ứng dụng vẫn có: %v", k)
	}

	// Quản trị thấy kênh tắt + cảnh báo; cán bộ thường bị từ chối.
	adm := f.user(authz.Scope{Units: []string{ward1}}, authz.KNAdmin)
	r := adm.do(f.admin.URL, http.MethodGet, "/v1/notify/delivery-health", "", nil)
	if r.status != http.StatusOK {
		t.Fatalf("delivery-health: %d %v", r.status, r.body)
	}
	for name, reason := range map[string]string{"email": "smtp_not_configured", "push": "fcm_not_configured"} {
		h, _ := r.body[name].(map[string]any)
		if h["status"] != "disabled" || h["disabled_reason"] != reason || h["sent"] != float64(0) || h["disabled"] != float64(1) {
			t.Fatalf("%s: %v", name, h)
		}
		if w, _ := h["warning"].(string); !strings.Contains(w, "KHÔNG được gửi") {
			t.Fatalf("%s thiếu cảnh báo: %v", name, h)
		}
	}
	if r := a.do(f.admin.URL, http.MethodGet, "/v1/notify/delivery-health", "", nil); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ thường không xem tình trạng kênh: %d", r.status)
	}
}

// SMTP có nhưng AG_WEB_OPS_URL chưa đặt ngoài máy phát triển → không gửi email chứa liên kết tương đối: bản gửi bị bỏ
// với lý do link_base_missing (không chạm máy chủ thư).
func TestEmailWithoutLinkBaseIsDisabled_P0NotifyFailClosed(t *testing.T) {
	f := newFixture(t)
	ch := f.useChannels(map[string]string{"AG_ENV": "production", "AG_SMTP_HOST": "smtp.invalid", "AG_SMTP_FROM": "no-reply@agrigreen.vn"})
	if ch.Email.Reason != "link_base_missing" || ch.Mailer == nil {
		t.Fatalf("email theo người nhận tắt vì thiếu liên kết, email tóm tắt giữ SMTP: %+v digest=%v", ch.Email, ch.Mailer != nil)
	}
	sc := authz.Scope{Units: []string{ward1}}
	ctv := f.user(sc, authz.KNXa)
	owner := f.user(sc, authz.KNTinh)
	f.desk.officers = []uuid.UUID{owner.actor}
	f.emails.set(ctv.actor, "nguyen.van.ctv@mekong.vn")
	f.process(priceport.NotifyConsumerPrice, f.event(f.tenant, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn",
		Day: "2026-10-24", PointCode: "XBG0001", PointName: "Mỹ Chánh", TicketID: uuid.New(), ItemCodes: []string{"MH00001"},
		RespondBy: "2026-10-24T06:30:00Z", OfficerRef: &ctv.actor, RequesterRef: owner.actor}))
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	mail := f.deliveries("pf_notify.email_delivery")
	if len(mail) != 1 || mail[0].status != "dropped" || mail[0].lastError != "channel_disabled:link_base_missing" {
		t.Fatalf("email phải dropped/link_base_missing: %+v", mail)
	}
}
