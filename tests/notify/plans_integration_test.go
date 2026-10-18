package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type planTestMailer struct {
	fail                bool
	attempts, delivered int
}

func (m *planTestMailer) Send(context.Context, []string, string, string) error {
	m.attempts++
	if m.fail {
		return errors.New("fixture mail failure")
	}
	m.delivered++
	return nil
}
func planDigestCounts(t *testing.T, f *fixture, tid, ref uuid.UUID) (total, sent int) {
	t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.appDB, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*), count(*) FILTER (WHERE sent_at IS NOT NULL) FROM pf_notify.email_digest_item WHERE directive_id=$1", ref).Scan(&total, &sent)
	}); err != nil {
		t.Fatal(err)
	}
	return
}
func TestNotificationPlanAtomicQueueRetryAndFormatterIsolation(t *testing.T) {
	f := newFixture(t)
	actor := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
	foreign := f.asActor(ids.New(), ids.New(), actor.actor, partyport.RoleKNOfficer)
	reference := ids.New()
	invalid := true
	mailer := &planTestMailer{}
	registrations := []notifyport.DigestRegistration{{Events: []string{extport.EventDirectiveIssued}, Job: notifyport.DigestJobDefinition{Name: "fixture_digest", Every: time.Hour, Timeout: time.Minute}, Format: func(entries []notifyport.DigestDraft) (string, string) {
		if len(entries) != 1 || entries[0].Reference != reference {
			t.Fatal("unexpected queue formatter input")
		}
		entries[0].Reference = uuid.Nil
		entries[0].AudienceUnits[0] = "corrupt"
		return "Fixture digest", "Fixture body"
	}}}
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope, Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Digests: registrations, Mailer: mailer, DigestTo: []string{"fixture@example.vn"}, Builders: []notifyport.EventBuilderRegistration{{Events: []string{extport.EventDirectiveIssued}, Plan: func(context.Context, uuid.UUID, string, json.RawMessage, time.Time) (notifyport.NotificationPlan, error) {
		link := "/fixture"
		if invalid {
			link = "https://invalid"
		}
		return notifyport.NotificationPlan{Drafts: []notifyport.NotificationDraft{{RecipientRef: &actor.actor, Kind: notifyport.KindSyncConflict, Title: "Fixture", Body: "Fixture", DeepLink: link, CreatedAt: f.clk.Now()}}, Digests: []notifyport.DigestDraft{{Reference: reference, AudienceUnits: []string{prov91}, IssuedAt: f.clk.Now()}}}, nil
	}}}})
	registrations[0].Events[0] = "corrupt"
	registrations[0].Job.Name = "corrupt"
	registrations[0].Job.Every = 0
	registrations[0].Format = nil
	event := f.event(f.tenant, extport.EventDirectiveIssued, map[string]string{"fixture": "plan"})
	if _, err := inbox.Process(context.Background(), f.appDB, extport.NotifyConsumerDirective, event, f.mod.EventHandler()); err == nil {
		t.Fatalf("invalid plan: %v", err)
	}
	if total, _ := planDigestCounts(t, f, f.tenant, reference); total != 0 || len(actor.inbox()) != 0 {
		t.Fatal("invalid notification committed digest or notification")
	}
	invalid = false
	if !f.process(extport.NotifyConsumerDirective, event) || f.process(extport.NotifyConsumerDirective, event) {
		t.Fatal("plan retry/replay not idempotent")
	}
	if total, sent := planDigestCounts(t, f, f.tenant, reference); total != 1 || sent != 0 {
		t.Fatalf("queue %d/%d", total, sent)
	}
	if total, _ := planDigestCounts(t, f, foreign.tid, reference); total != 0 || len(foreign.inbox()) != 0 {
		t.Fatal("plan escaped tenant")
	}
	rows := actor.inbox()
	if len(rows) != 1 {
		t.Fatalf("notifications: %v", rows)
	}
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		var eventID uuid.UUID
		var eventType string
		if err := tx.QueryRow(context.Background(), "SELECT source_event_id,source_event_type FROM pf_notify.notification WHERE recipient_ref=$1", actor.actor).Scan(&eventID, &eventType); err != nil {
			return err
		}
		if eventID != event.EventID || eventType != event.EventType {
			t.Fatal("plan forged provenance")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	jobs := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)
	if len(jobs) != 1 || jobs[0].Name != "fixture_digest" || jobs[0].Timeout != time.Minute {
		t.Fatal("digest metadata aliases registration")
	}
	mailer.fail = true
	if err := jobs[0].Run(context.Background()); err == nil {
		t.Fatal("mail failure swallowed")
	}
	if total, sent := planDigestCounts(t, f, f.tenant, reference); total != 1 || sent != 0 {
		t.Fatal("failed mail marked queue sent")
	}
	mailer.fail = false
	if err := jobs[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := jobs[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if total, sent := planDigestCounts(t, f, f.tenant, reference); total != 1 || sent != 1 || mailer.attempts != 2 || mailer.delivered != 1 {
		t.Fatalf("formatter mutated mark refs / retry %d/%d/%d/%d", total, sent, mailer.attempts, mailer.delivered)
	}
}
