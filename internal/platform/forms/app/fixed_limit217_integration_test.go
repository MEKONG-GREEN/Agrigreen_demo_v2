package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestFormsFixedLimitRejectsPublicationOverride217(t *testing.T) {
	f := newUnitFx(t)
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	owner := f.asDept(ids.New(), "tinh", "ke_hoach", ids.New())
	templates, err := f.svc.InstallFixtures(admin, "khtc", "")
	f.must(err, "templates")
	for _, maximum := range []int{0, 1, 9} {
		_, err = f.svc.Publish(owner, PublishInput{Profile: "khtc", TemplateID: templates[1].ID,
			Label: "Fixed limit", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"trong_trot"}, MaxSubmissions: &maximum}, "")
		if !isProblem(err, "validation_failed") {
			t.Fatalf("publication bypass max=%d must reject without writes; got %v", maximum, err)
		}
	}
	if n := f.scalar(`SELECT count(*) FROM pf_forms.period WHERE tenant_id=$1`, f.tid); n != 0 {
		t.Fatalf("rejected publication wrote %d periods", n)
	}
}

func fixedLegacy217(t *testing.T, maximum int) (*fx, string, uuid.UUID, context.Context, context.Context, string) {
	t.Helper()
	f := newUnitFx(t)
	profile := khtcProfile
	profile.Name = "fixed217_" + strings.ReplaceAll(ids.New().String(), "-", "")[:20]
	profile.FixedMaxSubmissions = 0
	profile.AutoLockAtLimit = false
	profile.MaxSubmissions = maximum
	var err error
	f.svc, err = NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "legacy owner")
	owner := f.asDept(ids.New(), "tinh", "ke_hoach", ids.New())
	member := f.asDept(ids.New(), "tinh", "trong_trot", ids.New())
	tpls, err := f.svc.InstallFixtures(f.as(f.tid, uuid.Nil, "admin", uuid.Nil), profile.Name, "")
	f.must(err, "template")
	p, err := f.svc.Publish(owner, PublishInput{Profile: profile.Name, TemplateID: tpls[1].ID, Label: "legacy", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"trong_trot", "chan_nuoi"}}, "")
	f.must(err, "legacy publication")
	var original string
	f.must(f.env.AdminPool(t, migrations.PF).QueryRow(f.ctx, `SELECT encode(row_hash,'hex') FROM pf_forms.period_spec WHERE tenant_id=$1 AND period_id=$2`, f.tid, p.Period.ID).Scan(&original), "historical spec hash")
	profile.FixedMaxSubmissions = 3
	profile.AutoLockAtLimit = true
	f.svc, err = NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{profile})
	f.must(err, "fixed owner")
	return f, profile.Name, p.Period.ID, owner, member, original
}

func TestFormsFixedLimitLegacyAndDirectSQL217(t *testing.T) {
	for _, legacy := range []int{0, 1, 9} {
		t.Run(fmt.Sprintf("legacy%d", legacy), func(t *testing.T) {
			f, profile, pid, owner, member, original := fixedLegacy217(t, legacy)
			p, err := f.svc.Period(owner, profile, pid)
			f.must(err, "effective period")
			if p.Spec.MaxSubmissions != 3 || !p.Spec.AutoLockAtLimit {
				t.Fatalf("effective policy: %+v", p.Spec)
			}
			var last SubmissionView
			for n := int64(0); n < 3; n++ {
				last, err = f.svc.SubmitWith(member, profile, pid, SubmitInput{Unit: "trong_trot", Values: fx02, BaseVersion: &n}, fmt.Sprintf("legacy%d-send%d", legacy, n))
				f.must(err, "native submission")
				if n < 2 {
					_, err = f.svc.Review(owner, profile, last.Submission.ID, "returned", "Bổ sung", fmt.Sprintf("return%d", n))
					f.must(err, "review before cap")
				}
			}
			if last.Submission.Version != 3 || last.Status != domain.SubLocked {
				t.Fatalf("third acknowledgement: %+v", last)
			}
			p, err = f.svc.Period(owner, profile, pid)
			f.must(err, "native locked projection")

			unit := unitOf(t, p, "trong_trot")
			if unit.Status != domain.SubLocked {
				t.Fatalf("third must project locked: %+v", p.Units)
			}
			task := TaskDataOf(Task{Period: p.Period, Spec: p.Spec, State: p.State, Unit: unit}, nil)
			if task.Status != domain.SubLocked || task.Open {
				t.Fatalf("auto-locked task remains open: %+v", task)
			}
			history, err := f.svc.Submissions(member, profile, pid, "trong_trot")
			f.must(err, "locked native history")
			if len(history) != 3 || history[0].Status != domain.SubLocked {
				t.Fatalf("native history lacks final lock: %+v", history)
			}
			for _, decision := range []string{"accepted", "returned"} {
				_, err = f.svc.Review(owner, profile, last.Submission.ID, decision, "Bổ sung", "blocked-review-"+decision)
				f.want(err, "forms.submission_limit", "review at cap")
				err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
					r := domain.Review{ID: ids.New(), Decision: decision, Reviewer: ids.New(), Note: "direct", At: f.clk.Now()}
					return store.InsertReview(f.ctx, tx, f.tid, last.Submission.ID, &r, nil, "direct-review-"+decision)
				})
				if code, _ := pgCode(err); code != "AGF03" {
					t.Fatalf("direct review %s: %v", decision, err)
				}
			}
			_, err = f.svc.Submit(member, profile, pid, "trong_trot", fx02, "fourth")
			f.want(err, "forms.submission_limit", "fourth native submission")
			err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
				// Faked profile and GUC cannot select a weaker rule; canonical period owns it.
				if _, e := tx.Exec(f.ctx, `SELECT set_config('app.forms_max_submissions','0',true)`); e != nil {
					return e
				}
				s := store.Submission{ID: ids.New(), PeriodID: pid, Unit: "trong_trot", Values: fx02, Submitter: ids.New(), EventTime: f.clk.Now()}
				return store.InsertSubmission(f.ctx, tx, f.tid, &s, "direct-fourth")
			})
			if code, _ := pgCode(err); code != "AGF03" {
				t.Fatalf("direct fourth max%d: %v", legacy, err)
			}
			var after string
			f.must(f.env.AdminPool(t, migrations.PF).QueryRow(f.ctx, `SELECT encode(row_hash,'hex') FROM pf_forms.period_spec WHERE tenant_id=$1 AND period_id=$2`, f.tid, pid).Scan(&after), "unchanged spec")
			if after != original {
				t.Fatal("historical spec was mutated")
			}
			if n := f.scalar(`SELECT count(*) FROM pf_forms.submission WHERE tenant_id=$1 AND period_id=$2`, f.tid, pid); n != 3 {
				t.Fatalf("rejections changed submissions: %d", n)
			}
			if n := f.scalar(`SELECT count(*) FROM pf_workflow.request WHERE tenant_id=$1 AND kind='forms.submission'`, f.tid); n != 2 {
				t.Fatalf("auto-lock created orphan review request: %d", n)
			}
			if n := f.scalar(`SELECT count(*) FROM pf_forms.submission_receipt WHERE tenant_id=$1 AND period_id=$2`, f.tid, pid); n != 3 {
				t.Fatalf("rejections changed receipts: %d", n)
			}
		})
	}
}

func TestFormsFixedLimitConcurrentFinalReceipt217(t *testing.T) {
	f, profile, pid, owner, member, _ := fixedLegacy217(t, 9)
	for n := int64(0); n < 2; n++ {
		sb, err := f.svc.SubmitWith(member, profile, pid, SubmitInput{Unit: "trong_trot", Values: fx02, BaseVersion: &n}, fmt.Sprintf("initial%d", n))
		f.must(err, "initial send")
		_, err = f.svc.Review(owner, profile, sb.Submission.ID, "returned", "Bổ sung", fmt.Sprintf("initial-review%d", n))
		f.must(err, "initial review")
	}
	type result struct {
		view SubmissionView
		err  error
		key  string
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, key := range []string{"final-a", "final-b"} {
		go func(key string) {
			<-start
			base := int64(2)
			v, err := f.svc.SubmitWith(member, profile, pid, SubmitInput{Unit: "trong_trot", Values: fx02, BaseVersion: &base}, key)
			results <- result{v, err, key}
		}(key)
	}
	close(start)
	var winner result
	wins := 0
	for range 2 {
		r := <-results
		if r.err == nil {
			winner = r
			wins++
		} else if !isProblem(r.err, "forms.submission_limit") && !isProblem(r.err, "forms.submission_conflict") {
			t.Fatal(r.err)
		}
	}
	if wins != 1 || winner.view.Submission.Version != 3 {
		t.Fatalf("wanted one final winner, got %d %+v", wins, winner)
	}
	// Simulate a lost ACK: replay binds the original request, not current latest.
	base := int64(2)
	in := SubmitInput{Unit: "trong_trot", Values: fx02, BaseVersion: &base}
	replay, err := f.svc.SubmitWith(member, profile, pid, in, winner.key)
	f.must(err, "final lost ACK replay")
	if !replay.Replayed || replay.Submission.ID != winner.view.Submission.ID || replay.Status != domain.SubLocked {
		t.Fatalf("wrong receipt: %+v", replay)
	}
	changed := in
	changed.Values = map[string]string{"pest": "khác", "area_infected": "9"}
	_, err = f.svc.SubmitWith(member, profile, pid, changed, winner.key)
	f.want(err, "conflict", "changed receipt payload")
	other := f.asDept(ids.New(), "tinh", "trong_trot", ids.New())
	_, err = f.svc.SubmitWith(other, profile, pid, in, winner.key)
	f.want(err, "conflict", "changed receipt actor")
	foreign := f.asDept(ids.New(), "tinh", "chan_nuoi", ids.New())
	_, err = f.svc.SubmitWith(foreign, profile, pid, in, winner.key)
	f.want(err, "forbidden", "current target before replay")
	p, _ := authz.FromContext(member)
	readOnly := fixtureContext(f.ctx, p, port.ResolvedActor{Role: "tinh", Department: "trong_trot", ReadOnly: true, Node: ids.New()})
	_, err = f.svc.SubmitWith(readOnly, profile, pid, in, winner.key)
	f.want(err, "forbidden", "current read-only before replay")

	// Expiring and purging the ordinary ledger-idem cache cannot erase the receipt identity.
	_, err = f.env.AdminPool(t, migrations.PF).Exec(f.ctx, `DELETE FROM shared.ledger_idem WHERE ledger='pf_forms.submission_receipt' AND tenant_id=$1 AND idem_key='initial0'`, f.tid)
	f.must(err, "simulate ordinary ledger-idem TTL purge")
	// Replaying the FIRST successful key still returns version1 after version3 exists.
	firstBase := int64(0)
	first, err := f.svc.SubmitWith(member, profile, pid, SubmitInput{Unit: "trong_trot", Values: fx02, BaseVersion: &firstBase}, "initial0")
	f.must(err, "original receipt")
	if first.Submission.Version != 1 || !first.Replayed {
		t.Fatalf("latest row substituted for receipt: %+v", first)
	}
	if n := f.scalar(`SELECT count(*) FROM pf_forms.submission WHERE tenant_id=$1 AND period_id=$2`, f.tid, pid); n != 3 {
		t.Fatalf("concurrency/replay count=%d", n)
	}
}

func TestFormsFixedPolicyRegistrationAndGenericCompatibility217(t *testing.T) {
	f := newUnitFx(t)
	for _, edit := range []func(*port.Profile){func(p *port.Profile) { p.FixedMaxSubmissions = 0; p.AutoLockAtLimit = false }, func(p *port.Profile) { p.FixedMaxSubmissions = 9 }, func(p *port.Profile) { p.AutoLockAtLimit = false }} {
		p := khtcProfile
		edit(&p)
		if _, err := NewService(f.own, f.clk, nil, []port.Profile{p}); err == nil {
			t.Fatalf("conflicting/removal configuration accepted: %+v", p)
		}
	}
	tpls, err := f.svc.InstallFixtures(f.as(f.tid, uuid.Nil, "admin", uuid.Nil), "fx", "")
	f.must(err, "generic template")
	owner := f.as(f.tid, ids.New(), "tinh", ids.New(), "U1")
	member := f.as(f.tid, ids.New(), "xa", ids.New(), "U1")
	p, err := f.svc.Publish(owner, PublishInput{Profile: "fx", TemplateID: tpls[1].ID, Label: "Unlimited", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "")
	f.must(err, "generic publication")
	var last SubmissionView
	for range 4 {
		last, err = f.svc.Submit(member, "fx", p.Period.ID, "U1", fx02, "")
		f.must(err, "generic unlimited send")
	}
	if last.Submission.Version != 4 || last.Status != domain.SubSubmitted {
		t.Fatalf("unrelated profile changed: %+v", last)
	}
	_, err = f.svc.Review(owner, "fx", last.Submission.ID, "accepted", "", "")
	f.must(err, "generic confirmation remains allowed")
}

func TestFormsSubmitReceiptAtomicRollback217(t *testing.T) {
	f, profile, pid, _, member, _ := fixedLegacy217(t, 0)
	admin := f.env.AdminPool(t, migrations.PF)
	name := "reject_receipt217_" + strings.ReplaceAll(f.tid.String(), "-", "")[:12]
	// A database failure AFTER native/meta writes must roll back the whole operation.
	_, err := admin.Exec(f.ctx, `CREATE FUNCTION pf_forms.`+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.idem_key='rollback217' THEN RAISE EXCEPTION 'controlled receipt failure'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER `+name+` BEFORE INSERT ON pf_forms.submission_receipt FOR EACH ROW EXECUTE FUNCTION pf_forms.`+name+`()`)
	f.must(err, "install own DB failure")
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON pf_forms.submission_receipt; DROP FUNCTION IF EXISTS pf_forms.`+name+`()`)
	})
	_, err = f.svc.Submit(member, profile, pid, "trong_trot", fx02, "rollback217")
	if err == nil {
		t.Fatal("expected actual database failure")
	}
	for _, table := range []string{"submission", "submission_meta", "submission_receipt"} {
		if n := f.scalar(`SELECT count(*) FROM pf_forms.`+table+` WHERE tenant_id=$1 AND period_id=$2`, f.tid, pid); n != 0 {
			t.Fatalf("atomic rollback %s has %d rows", table, n)
		}
	}
	_, err = admin.Exec(f.ctx, `DROP TRIGGER `+name+` ON pf_forms.submission_receipt; DROP FUNCTION pf_forms.`+name+`()`)
	f.must(err, "remove own fault")
	v, err := f.svc.Submit(member, profile, pid, "trong_trot", fx02, "rollback217")
	f.must(err, "same operation after rollback")
	if v.Submission.Version != 1 {
		t.Fatalf("failed operation consumed attempt: %+v", v)
	}
}
