package notify_test

import (
	"testing"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

// Reviewer-only probe. A logging transport sends no message and must never
// increment the persisted sent status or give the admin a successful channel.
func TestReviewerLogModeNeverCountsAsSent127(t *testing.T) {
	t.Run("push", func(t *testing.T) {
		f := newFixture(t)
		f.useChannels(map[string]string{"AG_ENV": "local", "AG_DEPLOY_ENV": "dev", "AG_MAILER": "log", "AG_PUSHER": "log"})
		a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
		a.device(fcmToken)
		env, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
		f.process(extport.NotifyConsumerDirective, env)
		if err := f.flushPush(f.tenant); err != nil {
			t.Fatal(err)
		}
		rows := f.deliveries("pf_notify.push_delivery")
		if len(rows) != 1 {
			t.Fatalf("want one delivery, got %+v", rows)
		}
		if rows[0].status == "sent" {
			t.Fatalf("logging-only push persisted as sent: %+v", rows[0])
		}
	})
	t.Run("email", func(t *testing.T) {
		f := newFixture(t)
		f.useChannels(map[string]string{"AG_ENV": "local", "AG_DEPLOY_ENV": "dev", "AG_MAILER": "log", "AG_PUSHER": "log"})
		sc := authz.Scope{Units: []string{ward1}}
		ctv := f.user(sc, authz.KNXa)
		owner := f.user(sc, authz.KNTinh)
		f.desk.officers = []uuid.UUID{owner.actor}
		f.emails.set(ctv.actor, "review-probe@example.test")
		f.process(priceport.NotifyConsumerPrice, f.event(f.tenant, priceport.EventVerificationRequested,
			priceport.VerificationRequestedV1{Profile: "kn", Day: "2026-10-24", PointCode: "XBG0001", PointName: "Review probe",
				TicketID: uuid.New(), ItemCodes: []string{"MH00001"}, RespondBy: "2026-10-24T06:30:00Z", OfficerRef: &ctv.actor, RequesterRef: owner.actor}))
		if err := f.flushEmail(f.tenant); err != nil {
			t.Fatal(err)
		}
		rows := f.deliveries("pf_notify.email_delivery")
		if len(rows) != 1 {
			t.Fatalf("want one delivery, got %+v", rows)
		}
		if rows[0].status == "sent" {
			t.Fatalf("logging-only email persisted as sent: %+v", rows[0])
		}
	})
}
