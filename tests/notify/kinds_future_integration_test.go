package notify_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func TestRegisteredFutureKindEmailAndUnknownAtomicRetry(t *testing.T) {
	f := newFixture(t)
	first := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
	second := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
	f.emails.set(first.actor, "first@example.invalid")
	f.emails.set(second.actor, "second@example.invalid")
	selected := "undeclared_kind"
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{
		Scope: f.scope, Emails: f.emails, EmailMailer: f.mailer,
		Kinds:     []port.KindRegistration{{Kinds: []string{"fixture_kind"}}},
		Consumers: []port.ConsumerRegistration{kindFixtureConsumer()},
		Renderers: []port.EventRendererRegistration{{Events: []string{"fixture.kind.created"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
			return []port.NotificationDraft{
				{RecipientRef: &first.actor, Kind: "fixture_kind", Title: "Fixture", DeepLink: "/fixture", Email: true, CreatedAt: f.clk.Now()},
				{RecipientRef: &second.actor, Kind: selected, Title: "Fixture", DeepLink: "/fixture", Email: true, CreatedAt: f.clk.Now()},
			}, nil
		}}},
	})
	event := f.event(f.tenant, "fixture.kind.created", map[string]string{"fixture": "future-kind"})
	observe := func() (int, int, int) {
		t.Helper()
		var notifications, email, created int
		if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
			if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.notification WHERE source_event_type='fixture.kind.created'").Scan(&notifications); err != nil {
				return err
			}
			if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.email_delivery WHERE kind='fixture_kind'").Scan(&email); err != nil {
				return err
			}
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM shared.outbox WHERE event_type=$1 AND payload->>'source_event_id'=$2", port.EventNotificationCreated, event.EventID.String()).Scan(&created)
		}); err != nil {
			t.Fatal(err)
		}
		return notifications, email, created
	}
	if _, err := inbox.Process(context.Background(), f.appDB, "fixture.kind", event, f.mod.EventHandler()); err == nil {
		t.Fatal("unregistered second draft must roll back the entire event")
	}
	if n, email, created := observe(); n != 0 || email != 0 || created != 0 {
		t.Fatalf("partial transaction leaked: %d/%d/%d", n, email, created)
	}
	selected = "fixture_kind"
	if !f.process("fixture.kind", event) || f.process("fixture.kind", event) {
		t.Fatal("retry must consume exactly once")
	}
	if n, email, created := observe(); n != 2 || email != 2 || created != 2 {
		t.Fatalf("registered future kind not persisted once: %d/%d/%d", n, email, created)
	}
	for _, actor := range []caller{first, second} {
		got := actor.inbox()
		if len(got) != 1 || got[0]["kind"] != "fixture_kind" {
			t.Fatalf("future kind inbox: %v", got)
		}
	}
	other := f.userIn(ids.New(), ids.New(), authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
	if got := other.inbox(); len(got) != 0 {
		t.Fatal("future kind crossed tenant")
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	if len(f.mailer.to("first@example.invalid")) != 1 || len(f.mailer.to("second@example.invalid")) != 1 {
		t.Fatal("future kind email not sent exactly once")
	}
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		var valid int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.notification WHERE kind='fixture_kind' AND source_event_id=$1 AND source_event_type=$2", event.EventID, event.EventType).Scan(&valid); err != nil {
			return err
		}
		if valid != 2 {
			t.Fatal("source provenance not preserved")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
