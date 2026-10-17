package notify_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

var errFixtureDirectiveLookup = errors.New("fixture directive directory unavailable")

type retryDirectiveDirectory struct {
	fail      bool
	recipient uuid.UUID
	tenants   []uuid.UUID
}

func (d *retryDirectiveDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, _ []string) ([]port.StaffRef, error) {
	d.tenants = append(d.tenants, tid)
	if d.fail {
		return nil, errFixtureDirectiveLookup
	}
	return []port.StaffRef{{ActorRef: d.recipient}, {ActorRef: d.recipient}, {ActorRef: uuid.Nil}}, nil
}
func TestDirectiveOwnedPlanQueuesWithoutAudienceAndRetriesLookup(t *testing.T) {
	t.Run("no-audience", func(t *testing.T) {
		f := newFixture(t)
		mail := &fakeMailer{}
		f.mod = notify.New(f.appDB, f.clk, notify.Deps{Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil), Digests: []port.DigestRegistration{extension.NotificationDigest()}, Mailer: mail, DigestTo: []string{"fixture@example.vn"}})
		reference := ids.New()
		event := f.event(f.tenant, extport.EventDirectiveIssued, extport.DirectiveIssuedV1{DirectiveID: reference, Severity: port.SeverityUrgent})
		if !f.process(extport.NotifyConsumerDirective, event) || f.process(extport.NotifyConsumerDirective, event) {
			t.Fatal("urgent no-audience replay")
		}
		if total, sent := planDigestCounts(t, f, f.tenant, reference); total != 1 || sent != 0 {
			t.Fatal("urgent no-audience digest lost")
		}
		normalRef := ids.New()
		f.process(extport.NotifyConsumerDirective, f.event(f.tenant, extport.EventDirectiveIssued, extport.DirectiveIssuedV1{DirectiveID: normalRef, Severity: port.SeverityNormal}))
		if total, _ := planDigestCounts(t, f, f.tenant, normalRef); total != 0 {
			t.Fatal("normal directive queued digest")
		}
		var notices int
		if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.notification").Scan(&notices)
		}); err != nil {
			t.Fatal(err)
		}
		if notices != 0 {
			t.Fatal("empty audience created notification")
		}
		for _, job := range f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil) {
			if err := job.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := job.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if total, sent := planDigestCounts(t, f, f.tenant, reference); total != 1 || sent != 1 || len(mail.sent) != 1 {
			t.Fatal("owner digest not sent exactly once")
		}
	})
	t.Run("lookup-retry", func(t *testing.T) {
		f := newFixture(t)
		actor := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
		directory := &retryDirectiveDirectory{fail: true, recipient: actor.actor}
		f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope, Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(directory, nil), Digests: []port.DigestRegistration{extension.NotificationDigest()}})
		reference := ids.New()
		event := f.event(f.tenant, extport.EventDirectiveIssued, extport.DirectiveIssuedV1{DirectiveID: reference, Severity: port.SeverityUrgent, AudienceUnits: []string{prov91}})
		if _, err := inbox.Process(context.Background(), f.appDB, extport.NotifyConsumerDirective, event, f.mod.EventHandler()); !errors.Is(err, errFixtureDirectiveLookup) {
			t.Fatalf("lookup identity: %v", err)
		}
		if total, _ := planDigestCounts(t, f, f.tenant, reference); total != 0 || len(actor.inbox()) != 0 {
			t.Fatal("lookup failure wrote partial plan")
		}
		directory.fail = false
		if !f.process(extport.NotifyConsumerDirective, event) || f.process(extport.NotifyConsumerDirective, event) {
			t.Fatal("owner directive retry/dedup")
		}
		if total, _ := planDigestCounts(t, f, f.tenant, reference); total != 1 || len(actor.inbox()) != 1 {
			t.Fatal("owner directive delivery")
		}
		if len(directory.tenants) != 2 {
			t.Fatal("lookup repeated after inbox replay")
		}
		for _, tid := range directory.tenants {
			if tid != f.tenant {
				t.Fatal("lookup tenant lost")
			}
		}
	})
}
