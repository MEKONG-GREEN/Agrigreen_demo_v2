package notify_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"testing"
	"time"
)

const deliveryFixtureEvent = "fixture.delivery.requested"
const deliveryFixtureConsumer = "fixture.delivery"
const deliveryFixtureKind = "fixture_delivery"

func deliveryFixtureModule(f *fixture, drafts func() []port.NotificationDraft, email bool) *notify.Module {
	deps := notify.Deps{Scope: f.scope, Devices: f.devices, Pusher: f.pusher,
		Kinds:     []port.KindRegistration{{Kinds: []string{deliveryFixtureKind}}},
		Consumers: []port.ConsumerRegistration{{Definitions: []port.ConsumerDefinition{{Name: deliveryFixtureConsumer, Order: 1, Events: []string{deliveryFixtureEvent}}}}},
		Renderers: []port.EventRendererRegistration{{Events: []string{deliveryFixtureEvent}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) { return drafts(), nil }}},
	}
	if email {
		deps.Emails = f.emails
		deps.EmailMailer = f.mailer
	}
	return notify.New(f.appDB, f.clk, deps)
}

func TestExplicitDeliveryChannelsAllCombinationsAndLegacy(t *testing.T) {
	f := newFixture(t)
	var draft port.NotificationDraft
	f.mod = deliveryFixtureModule(f, func() []port.NotificationDraft { return []port.NotificationDraft{draft} }, true)
	for mask := 1; mask <= 8; mask++ {
		t.Run(fmt.Sprintf("channels_%d", mask), func(t *testing.T) {
			actor := f.user(authz.Scope{Units: []string{ward1}})
			foreign := f.asActor(ids.New(), ids.New(), actor.actor)
			actor.device(fmt.Sprintf("delivery-channel-token-%d", mask))
			addr := fmt.Sprintf("fixture-%d@example.invalid", mask)
			f.emails.set(actor.actor, addr)
			channels := &port.DeliveryChannels{InApp: mask&1 != 0, Push: mask&2 != 0, Email: mask&4 != 0}
			wantApp, wantPush, wantEmail := channels.InApp, channels.Push, channels.Email
			if mask == 8 {
				channels = nil
				wantApp, wantPush, wantEmail = true, true, true
			}
			title, body, err := (port.TextTemplate{Title: "Nhắc {{form}}", Body: "Kỳ {{period}}; liên hệ {{redact}}"}).Render(map[string]string{"form": "BM-04", "period": "Tuần 3", "redact": "0901234567"})
			if err != nil {
				t.Fatal(err)
			}
			draft = port.NotificationDraft{RecipientRef: &actor.actor, Kind: deliveryFixtureKind, Severity: port.SeverityUrgent, Title: title, Body: body, DeepLink: "/fixture", CreatedAt: f.clk.Now(), Email: true, Channels: channels}
			event := f.event(f.tenant, deliveryFixtureEvent, map[string]string{"fixture": "channels"})
			if !f.process(deliveryFixtureConsumer, event) || f.process(deliveryFixtureConsumer, event) {
				t.Fatal("replay not idempotent")
			}
			wantVisible := 0
			if wantApp {
				wantVisible = 1
			}
			got := actor.inbox()
			if len(got) != wantVisible || len(foreign.inbox()) != 0 {
				t.Fatal("inbox/tenant channel selection", got)
			}
			unread := actor.get("/v1/notifications/unread-count")
			if unread.status != http.StatusOK || unread.body["unread"] != float64(wantVisible) {
				t.Fatal("unread channel selection", unread)
			}
			changes, _, err := f.mod.SyncSource().Changes(actor.ctx(), authz.Scope{Units: []string{ward1}}, port.EntityNotification, 0, 100)
			if err != nil || len(changes) != wantVisible {
				t.Fatal("sync channel selection", err, len(changes))
			}
			var id uuid.UUID
			var inApp bool
			var persistedBody string
			var pushes, emails, created int
			if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
				if err := tx.QueryRow(context.Background(), "SELECT id,in_app,body FROM pf_notify.notification WHERE source_event_id=$1", event.EventID).Scan(&id, &inApp, &persistedBody); err != nil {
					return err
				}
				if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.push_delivery WHERE notification_id=$1 AND urgent", id).Scan(&pushes); err != nil {
					return err
				}
				if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.email_delivery WHERE notification_id=$1", id).Scan(&emails); err != nil {
					return err
				}
				return tx.QueryRow(context.Background(), "SELECT count(*) FROM shared.outbox WHERE event_type=$1 AND payload->>'source_event_id'=$2", port.EventNotificationCreated, event.EventID.String()).Scan(&created)
			}); err != nil {
				t.Fatal(err)
			}
			wantPushes, wantEmails := 0, 0
			if wantPush {
				wantPushes = 1
			}
			if wantEmail {
				wantEmails = 1
			}
			if inApp != wantApp || pushes != wantPushes || emails != wantEmails || created != wantVisible {
				t.Fatalf("queues/visibility/outbox: %v %d %d %d", inApp, pushes, emails, created)
			}
			if strings.Contains(persistedBody, "0901234567") || !strings.Contains(persistedBody, "***") {
				t.Fatal("template bypassed redaction")
			}
			if !wantApp {
				if r := actor.post("/v1/notifications/"+id.String()+"/read", nil); r.status != http.StatusNotFound {
					t.Fatal("hidden row readable", r)
				}
			}
			if r := actor.post("/v1/notifications/read-all", nil); r.status != http.StatusOK || r.body["marked"] != float64(wantVisible) {
				t.Fatal("read-all touched hidden transport row", r)
			}
			if err := f.flushEmail(f.tenant); err != nil {
				t.Fatal(err)
			}
			if err := f.flushEmail(f.tenant); err != nil {
				t.Fatal(err)
			}
			if len(f.mailer.to(addr)) != wantEmails {
				t.Fatal("email disabled or duplicated")
			}
			if err := f.flushPush(f.tenant); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidOrUnconfiguredDeliveryChoiceRollsBackAndRetries(t *testing.T) {
	f := newFixture(t)
	actor := f.user(authz.Scope{Units: []string{ward1}})
	good := port.NotificationDraft{RecipientRef: &actor.actor, Kind: deliveryFixtureKind, Title: "Good", DeepLink: "/fixture", CreatedAt: f.clk.Now()}
	bad := good
	drafts := func() []port.NotificationDraft { return []port.NotificationDraft{good, bad} }
	f.mod = deliveryFixtureModule(f, drafts, false)
	for _, mode := range []string{"empty", "topic-email", "unconfigured-email"} {
		t.Run(mode, func(t *testing.T) {
			bad = good
			switch mode {
			case "empty":
				bad.Channels = &port.DeliveryChannels{}
			case "topic-email":
				bad.RecipientRef = nil
				bad.Channels = &port.DeliveryChannels{Email: true}
			case "unconfigured-email":
				bad.Channels = &port.DeliveryChannels{Email: true}
			}
			event := f.event(f.tenant, deliveryFixtureEvent, map[string]string{"fixture": mode})
			if _, err := inbox.Process(context.Background(), f.appDB, deliveryFixtureConsumer, event, f.mod.EventHandler()); err == nil {
				t.Fatal("invalid delivery accepted")
			}
			var notices, outboxes int
			if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
				if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.notification WHERE source_event_id=$1", event.EventID).Scan(&notices); err != nil {
					return err
				}
				return tx.QueryRow(context.Background(), "SELECT count(*) FROM shared.outbox WHERE payload->>'source_event_id'=$1", event.EventID.String()).Scan(&outboxes)
			}); err != nil {
				t.Fatal(err)
			}
			if notices != 0 || outboxes != 0 {
				t.Fatal("invalid choice committed sibling")
			}
			bad = good
			bad.Channels = &port.DeliveryChannels{InApp: true}
			if !f.process(deliveryFixtureConsumer, event) || f.process(deliveryFixtureConsumer, event) {
				t.Fatal("corrected event could not retry once")
			}
		})
	}
}
