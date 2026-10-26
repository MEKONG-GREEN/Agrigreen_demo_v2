package notify_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type digestRecipientMail struct {
	to            []string
	subject, body string
}
type digestRecipientMailer struct {
	fail      bool
	attempts  int
	delivered []digestRecipientMail
}

func (m *digestRecipientMailer) Send(_ context.Context, to []string, subject, body string) error {
	m.attempts++
	if m.fail {
		return errors.New("fixture SMTP failure")
	}
	m.delivered = append(m.delivered, digestRecipientMail{slices.Clone(to), subject, body})
	to[0] = "mailer-mutated@example.vn"
	return nil
}
func recipientDigestModule(f *fixture, m *digestRecipientMailer, resolver notifyport.DigestRecipients) *notify.Module {
	return notify.New(f.appDB, f.clk, notify.Deps{Kinds: []notifyport.KindRegistration{extension.NotificationKinds()},
		Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil),
		Digests: []notifyport.DigestRegistration{extension.NotificationDigest()}, Mailer: m,
		DigestTo: []string{"global@example.vn"}, DigestRecipients: resolver})
}
func recipientResolver(t *testing.T, entries string) notifyport.DigestRecipients {
	t.Helper()
	r, err := extension.NewDigestRecipients(entries)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func digestRecipientJob(t *testing.T, f *fixture, tenants ...uuid.UUID) func() error {
	t.Helper()
	list := notify.DigestTenants(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.OpsJobs))
	jobs := f.mod.Jobs(func(ctx context.Context) ([]uuid.UUID, error) {
		all, err := list(ctx)
		if err != nil {
			return nil, err
		}
		var mine []uuid.UUID
		for _, tid := range all {
			if slices.Contains(tenants, tid) {
				mine = append(mine, tid)
			}
		}
		return mine, nil
	}, nil)
	if len(jobs) != 1 {
		t.Fatal("missing registered digest job")
	}
	return func() error { return jobs[0].Run(context.Background()) }
}
func queueRecipientDigest(t *testing.T, f *fixture, tid uuid.UUID) uuid.UUID {
	t.Helper()
	ref := ids.New()
	if !f.process(extport.NotifyConsumerDirective, f.event(tid, extport.EventDirectiveIssued, extport.DirectiveIssuedV1{DirectiveID: ref, Severity: notifyport.SeverityUrgent})) {
		t.Fatal("queue event not committed")
	}
	return ref
}
func recipientDigestCounts(t *testing.T, f *fixture, tid uuid.UUID) (total, sent int) {
	t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.appDB, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*),count(*) FILTER(WHERE sent_at IS NOT NULL) FROM pf_notify.email_digest_item`).Scan(&total, &sent)
	}); err != nil {
		t.Fatal(err)
	}
	return
}
func TestKNDigestTwoTenantRecipientsAndContent(t *testing.T) {
	f := newFixture(t)
	other := ids.New()
	mail := &digestRecipientMailer{}
	resolver := recipientResolver(t, fmt.Sprintf(`[{"tenant_id":%q,"to":["center-a@example.vn"]},{"tenant_id":%q,"to":["center-b@example.vn"]}]`, f.tenant, other))
	f.mod = recipientDigestModule(f, mail, resolver)
	a := queueRecipientDigest(t, f, f.tenant)
	b := queueRecipientDigest(t, f, other)
	run := digestRecipientJob(t, f, f.tenant, other)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if len(mail.delivered) != 2 || mail.attempts != 2 {
		t.Fatal("digest duplicate or missing")
	}
	for _, m := range mail.delivered {
		switch {
		case slices.Equal(m.to, []string{"center-a@example.vn"}):
			if !strings.Contains(m.body, a.String()) || strings.Contains(m.body, b.String()) {
				t.Fatal("tenant A content crossed")
			}
		case slices.Equal(m.to, []string{"center-b@example.vn"}):
			if !strings.Contains(m.body, b.String()) || strings.Contains(m.body, a.String()) {
				t.Fatal("tenant B content crossed")
			}
		default:
			t.Fatal("digest used global/foreign recipients")
		}
	}
	for _, tid := range []uuid.UUID{f.tenant, other} {
		if n, s := recipientDigestCounts(t, f, tid); n != 1 || s != 1 {
			t.Fatal("tenant queue not marked once")
		}
	}
	got, _ := resolver(context.Background(), f.tenant)
	if got[0] != "center-a@example.vn" {
		t.Fatal("Mailer mutated recipient snapshot")
	}
}
func TestKNDigestMissingMappingKeepsQueueAndNeverFallsBack(t *testing.T) {
	f := newFixture(t)
	mail := &digestRecipientMailer{}
	f.mod = recipientDigestModule(f, mail, recipientResolver(t, ""))
	queueRecipientDigest(t, f, f.tenant)
	run := digestRecipientJob(t, f, f.tenant)
	if err := run(); !errors.Is(err, extension.ErrDigestRecipientsMissing) {
		t.Fatal("missing mapping swallowed")
	}
	if n, s := recipientDigestCounts(t, f, f.tenant); n != 1 || s != 0 || mail.attempts != 0 {
		t.Fatal("unmapped tenant sent/marked via global fallback")
	}
	// An injected resolver that returns an empty list is equally authoritative.
	f.mod = recipientDigestModule(f, mail, func(context.Context, uuid.UUID) ([]string, error) { return nil, nil })
	if err := digestRecipientJob(t, f, f.tenant)(); err == nil || mail.attempts != 0 {
		t.Fatal("empty resolver used global fallback")
	}
}
func TestKNDigestSMTPFailureRetriesPendingWithoutDuplicates(t *testing.T) {
	f := newFixture(t)
	mail := &digestRecipientMailer{fail: true}
	f.mod = recipientDigestModule(f, mail, recipientResolver(t, fmt.Sprintf(`[{"tenant_id":%q,"to":["center@example.vn"]}]`, f.tenant)))
	queueRecipientDigest(t, f, f.tenant)
	run := digestRecipientJob(t, f, f.tenant)
	if err := run(); err == nil {
		t.Fatal("SMTP failure swallowed")
	}
	if n, s := recipientDigestCounts(t, f, f.tenant); n != 1 || s != 0 {
		t.Fatal("SMTP error marked sent")
	}
	mail.fail = false
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if n, s := recipientDigestCounts(t, f, f.tenant); n != 1 || s != 1 || mail.attempts != 2 || len(mail.delivered) != 1 {
		t.Fatal("SMTP retry not idempotent")
	}
}
func TestKNDigestContinuesAfterTwoHundredItems(t *testing.T) {
	f := newFixture(t)
	mail := &digestRecipientMailer{}
	f.mod = recipientDigestModule(f, mail, recipientResolver(t, fmt.Sprintf(`[{"tenant_id":%q,"to":["center@example.vn"]}]`, f.tenant)))
	refs := make([]uuid.UUID, 201)
	for i := range refs {
		refs[i] = queueRecipientDigest(t, f, f.tenant)
	}
	run := digestRecipientJob(t, f, f.tenant)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if n, s := recipientDigestCounts(t, f, f.tenant); n != 201 || s != 200 {
		t.Fatal("first digest page count")
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if n, s := recipientDigestCounts(t, f, f.tenant); n != 201 || s != 201 || len(mail.delivered) != 2 {
		t.Fatal("remaining digest page duplicated/lost")
	}
	body := mail.delivered[0].body + mail.delivered[1].body
	for _, ref := range refs {
		if strings.Count(body, ref.String()) != 1 {
			t.Fatal("digest reference omitted or duplicated")
		}
	}
}
func TestGenericDigestNilResolverPreservesGlobalRecipients(t *testing.T) {
	f := newFixture(t)
	mail := &digestRecipientMailer{}
	f.mod = recipientDigestModule(f, mail, nil)
	queueRecipientDigest(t, f, f.tenant)
	if err := digestRecipientJob(t, f, f.tenant)(); err != nil {
		t.Fatal(err)
	}
	if len(mail.delivered) != 1 || !slices.Equal(mail.delivered[0].to, []string{"global@example.vn"}) {
		t.Fatal("generic nil-resolver compatibility changed")
	}
}
