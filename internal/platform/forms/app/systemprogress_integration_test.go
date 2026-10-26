package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type progressReadObserver struct {
	tenant.Beginner
	readOnly bool
	role     string
}

func (o *progressReadObserver) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	o.readOnly = opts.AccessMode == pgx.ReadOnly
	tx, err := o.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	var mode string
	if err = tx.QueryRow(ctx, `SELECT current_user,current_setting('transaction_read_only')`).Scan(&o.role, &mode); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	o.readOnly = o.readOnly && mode == "on"
	return tx, nil
}

func TestSystemProgressMatchesUserLifecycleWithoutPrincipal(t *testing.T) {
	for _, profile := range []string{"fx", "khtc"} {
		t.Run(profile, func(t *testing.T) {
			f := newUnitFx(t)
			owner, publisher, submitter := ids.New(), ids.New(), ids.New()
			admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
			unit := "U1"
			pub := f.as(f.tid, publisher, "tinh", owner, unit)
			leaf := f.as(f.tid, submitter, "xa", ids.New(), unit)
			if profile == "khtc" {
				unit = "trong_trot"
				pub = f.asDept(publisher, "tinh", "ke_hoach", owner)
				leaf = f.asDept(submitter, "tinh", unit, owner)
			}
			tpls, err := f.svc.InstallFixtures(admin, profile, "")
			f.must(err, "install templates")
			from, to := f.clk.Now().AddDate(0, 0, -4), f.clk.Now().AddDate(0, 0, 2)
			pv, err := f.svc.Publish(pub, PublishInput{Profile: profile, TemplateID: tpls[1].ID, Label: "system progress", DueAt: f.clk.Now().Add(time.Hour), Units: []string{unit}, Period: &PeriodKey{Kind: "week", Key: "2026-W40", From: &from, To: &to, Rule: "fixture"}}, "")
			f.must(err, "publish")
			id := pv.Period.ID
			check := func(stage, wantPeriod, wantUnit string) {
				t.Helper()
				expected, err := (Control{Svc: f.svc}).Progress(pub, profile, id)
				f.must(err, "user progress "+stage)
				observer := &progressReadObserver{Beginner: f.svc.DB}
				original := f.svc.DB
				f.svc.DB = observer
				jobCtx := context.Background()
				got, err := (SystemReader{Svc: f.svc}).SystemProgress(jobCtx, f.tid, profile, id)
				f.svc.DB = original
				f.must(err, "system progress "+stage)
				if _, ok := authz.FromContext(jobCtx); ok {
					t.Fatal("job context acquired a fake Principal")
				}
				if !observer.readOnly || observer.role != "r_forms" {
					t.Fatalf("system reader did not use read-only module role: %v %s", observer.readOnly, observer.role)
				}
				if !reflect.DeepEqual(got.Progress, expected) || got.OwnerRef == nil || *got.OwnerRef != owner || *got.OwnerRef == publisher || got.PublisherLevel != "TINH" {
					t.Fatalf("%s progress or issuer mismatch: %+v / %+v", stage, got, expected)
				}
				if got.Progress.Status != wantPeriod || len(got.Progress.Units) != 1 || got.Progress.Units[0].Status != wantUnit {
					t.Fatalf("%s unexpected status: %+v", stage, got.Progress)
				}
				*got.OwnerRef = uuid.Nil
				got.Progress.Units[0].Unit = "mutated"
				if got.Progress.From != nil {
					*got.Progress.From = time.Time{}
				}
				if got.Progress.To != nil {
					*got.Progress.To = time.Time{}
				}
				if got.Progress.Units[0].LastSubmittedAt != nil {
					*got.Progress.Units[0].LastSubmittedAt = time.Time{}
				}
				if got.Progress.Units[0].ReviewedAt != nil {
					*got.Progress.Units[0].ReviewedAt = time.Time{}
				}
				again, err := (SystemReader{Svc: f.svc}).SystemProgress(jobCtx, f.tid, profile, id)
				f.must(err, "repeat system progress")
				if !reflect.DeepEqual(again.Progress, expected) || again.OwnerRef == nil || *again.OwnerRef != owner {
					t.Fatal("caller mutation changed authoritative state")
				}
			}
			check("new", domain.PeriodOpen, domain.SubNone)
			f.clk.Advance(2 * time.Hour)
			check("overdue", domain.PeriodOpen, domain.SubOverdue)
			err = (Control{Svc: f.svc}).ChangeDue(pub, profile, id, f.clk.Now().Add(time.Hour), "update actual due", "")
			f.must(err, "change due")
			check("changed due", domain.PeriodOpen, domain.SubNone)
			sb, err := f.svc.Submit(leaf, profile, id, unit, fx02, "")
			f.must(err, "submit")
			check("submitted", domain.PeriodOpen, domain.SubSubmitted)
			_, err = f.svc.Review(pub, profile, sb.Submission.ID, "returned", "Please supplement the report", "")
			f.must(err, "return")
			check("returned", domain.PeriodOpen, domain.SubReturned)
			sb, err = f.svc.Submit(leaf, profile, id, unit, fx02, "")
			f.must(err, "resubmit")
			_, err = f.svc.Review(pub, profile, sb.Submission.ID, "accepted", "", "")
			f.must(err, "accept")
			check("accepted", domain.PeriodOpen, domain.SubAccepted)
			_, err = f.svc.Close(pub, profile, id, "")
			f.must(err, "close")
			check("closed", domain.PeriodClosed, domain.SubAccepted)
			other, err := f.svc.Publish(pub, PublishInput{Profile: profile, TemplateID: tpls[1].ID, Label: "cancelled", DueAt: f.clk.Now().Add(time.Hour), Units: []string{unit}}, "")
			f.must(err, "publish cancelled fixture")
			id = other.Period.ID
			_, err = f.svc.Cancel(pub, profile, id, "This period was issued by mistake", "")
			f.must(err, "cancel")
			check("cancelled", domain.PeriodCancelled, domain.SubNone)
		})
	}
}

func TestSystemProgressTenantProfileAndIdentityIsolation(t *testing.T) {
	f := newUnitFx(t)
	owner, publisher := ids.New(), ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	pub := f.as(f.tid, publisher, "tinh", owner, "U1")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "templates")
	pv, err := f.svc.Publish(pub, PublishInput{Profile: "fx", TemplateID: tpls[1].ID, Label: "private", DueAt: f.clk.Now().Add(time.Hour), Units: []string{"U1"}}, "")
	f.must(err, "period")
	reader := SystemReader{Svc: f.svc}
	cases := []struct {
		name    string
		ctx     context.Context
		tid     uuid.UUID
		profile string
		id      uuid.UUID
		want    error
	}{
		{"zero tenant", context.Background(), uuid.Nil, "fx", pv.Period.ID, tenant.ErrMissing},
		{"foreign tenant", context.Background(), ids.New(), "fx", pv.Period.ID, port.ErrPeriodNotFound},
		{"foreign context tenant", tenant.WithID(context.Background(), ids.New()), f.tid, "fx", pv.Period.ID, port.ErrPeriodNotFound},
		{"foreign identity", authz.WithPrincipal(context.Background(), authz.Principal{TenantID: ids.New(), ActorRef: ids.New()}), f.tid, "fx", pv.Period.ID, port.ErrPeriodNotFound},
		{"unknown profile", context.Background(), f.tid, "unknown", pv.Period.ID, port.ErrProfileNotFound},
		{"wrong profile", context.Background(), f.tid, "khtc", pv.Period.ID, port.ErrPeriodNotFound},
		{"zero period", context.Background(), f.tid, "fx", uuid.Nil, port.ErrPeriodNotFound},
		{"unknown period", context.Background(), f.tid, "fx", ids.New(), port.ErrPeriodNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := reader.SystemProgress(c.ctx, c.tid, c.profile, c.id)
			if !errors.Is(err, c.want) || !reflect.DeepEqual(got, port.SystemPeriodProgress{}) {
				t.Fatalf("isolation returned metadata or wrong error: %+v %v", got, err)
			}
		})
	}
	raw := authz.Principal{ActorRef: ids.New(), TenantID: f.tid, OrgRef: ids.New(), Roles: []string{"platform_admin"}, MFA: true}
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tid), raw)
	if _, err := reader.SystemProgress(ctx, f.tid, "fx", pv.Period.ID); err != nil {
		t.Fatal(err)
	}
	after, ok := authz.FromContext(ctx)
	if !ok || !reflect.DeepEqual(raw, after) {
		t.Fatal("system read rewrote authenticated identity")
	}
	if _, err := (Control{Svc: f.svc}).Progress(context.Background(), "fx", pv.Period.ID); err == nil {
		t.Fatal("user Control.Progress lost its authentication gate")
	}
}
