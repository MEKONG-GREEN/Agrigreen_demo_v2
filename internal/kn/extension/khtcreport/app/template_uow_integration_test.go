package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"net/http"
	"os"
	"testing"
	"time"

	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	reportstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// This is a same-module unit-of-work test. A one-connection pool makes any
// accidental nested transaction in the publication fence fail by timeout.
func TestW1165_NewDraftPublicationFenceOneConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	env := pgtest.Get(t)
	config := env.Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	database := db.ForModule(pool, db.Module("pf/extension"))
	tid, actor := ids.New(), ids.New()
	selected, err := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: port.SessionRoleAdmin, Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx = tenant.WithID(authz.WithPrincipal(ctx, authz.Principal{TenantID: tid, ActorRef: actor, SelectedSession: &selected}), tid)
	sections, _ := json.Marshal(domain.Sections("mau_1"))
	legacy, err := templatedomain.LegacySnapshot("mau_1", domain.RuleVersion, sections, WordTemplate("mau_1"))
	if err != nil {
		t.Fatal(err)
	}
	svc := templateapp.Service{DB: database, Clock: clock.NewFixed(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)), LegacyTemplates: map[string]port.KHTCTemplateSnapshot{"mau_1": legacy}}
	old := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	content := domain.Draft{Data: domain.Data{Template: "mau_1", Period: domain.Period{From: start}}}
	candidate, err := svc.Select(ctx, "mau_1", start)
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.Save(ctx, "mau_1", templateapp.SaveInput{Definition: templatedomain.Seed("mau_1")}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Publish(ctx, "mau_1", templateapp.PublishInput{BaseVersion: version.Version, VersionID: version.ID, AppliesFrom: "2026-10-07"}, ids.New().String()); err != nil {
		t.Fatal(err)
	}
	err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error { return checkNewFixedSelection(ctx, tx, tid, &candidate, content) })
	var conflict *problem.Problem
	if !errors.As(err, &conflict) || conflict.Code != ErrVersion.Code || conflict.Status != http.StatusConflict {
		t.Fatalf("legacy candidate survived effective publication: %v", err)
	}
	var draftCount int
	if err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM pf_extension.khtc_report_draft").Scan(&draftCount)
	}); err != nil || draftCount != 0 {
		t.Fatal("rejected selection wrote a draft", err, draftCount)
	}
	before, err := svc.Select(ctx, "mau_1", old)
	if err != nil {
		t.Fatal(err)
	}
	before.LegacyUntil = nil // candidate read before the first publication
	content.Data.Period.From = old
	if err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error { return checkNewFixedSelection(ctx, tx, tid, &before, content) }); err != nil {
		t.Fatal(err)
	}
	if before.LegacyUntil == nil || !before.LegacyUntil.Equal(start) {
		t.Fatal("first publication boundary not captured")
	}
	content.Data.Period.From = start
	candidate, err = svc.Select(ctx, "mau_1", start)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error { return checkNewFixedSelection(ctx, tx, tid, &candidate, content) }); err != nil {
		t.Fatal(err)
	}
	candidate.Hash = "bad"
	if err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error { return checkNewFixedSelection(ctx, tx, tid, &candidate, content) }); err == nil {
		t.Fatal("forged pinned hash accepted")
	}
}

// Public-port test adapter pauses after the source selection transaction commits.
// It lets a real publisher race the first save without a production hook.
type gatedFixedSource struct {
	port.KHTCFixedTemplates
	reached chan struct{}
	release chan struct{}
}

func (p gatedFixedSource) Select(ctx context.Context, key string, start time.Time) (port.KHTCTemplateSnapshot, error) {
	value, err := p.KHTCFixedTemplates.Select(ctx, key, start)
	select {
	case p.reached <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return value, err
	case <-ctx.Done():
		return port.KHTCTemplateSnapshot{}, ctx.Err()
	}
}

type emptySubmittedForms struct{ forms.Entry }

func (emptySubmittedForms) Submissions(context.Context, string, uuid.UUID, string) ([]forms.SubmissionRef, error) {
	return []forms.SubmissionRef{}, nil
}
func TestW1165_ConcurrentPublicationFirstSaveNoWriteAndExistingPin(t *testing.T) {
	base, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(base, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	database := db.ForModule(pool, db.Module("pf/extension"))
	tid, actor := ids.New(), ids.New()
	caller := func(role, dept string) context.Context {
		session, e := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: role, Department: dept, OrgRef: ids.New(), Scope: authz.Scope{All: true}})
		if e != nil {
			t.Fatal(e)
		}
		return tenant.WithID(authz.WithPrincipal(base, authz.Principal{TenantID: tid, ActorRef: actor, SelectedSession: &session}), tid)
	}
	admin, province := caller(port.SessionRoleAdmin, ""), caller(port.SessionRoleProvince, "ke_hoach_tai_chinh")
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	no, key := 41, "2026-W41"
	period := periodstore.Period{ID: ids.New(), Type: "week", Year: 2026, No: &no, Key: &key, Name: "Kỳ kiểm tra publication race", From: start, To: start.AddDate(0, 0, 6), Cutoff: start.AddDate(0, 0, 6), DueAt: start.AddDate(0, 0, 7), ExportDueAt: start.AddDate(0, 0, 8), MaxSubmissions: 3, Departments: []string{"trong_trot"}, FormsPeriodRef: ids.New(), TemplateCode: perioddomain.TemplateCode, Rule: perioddomain.RuleVersion, ActorRef: actor, EventTime: start}
	if err = tenant.WithTenantTx(admin, database, tid, func(tx pgx.Tx) error { return periodstore.InsertPeriod(admin, tx, tid, &period, ids.New().String()) }); err != nil {
		t.Fatal(err)
	}
	sections, _ := json.Marshal(domain.Sections("mau_1"))
	legacy, err := templatedomain.LegacySnapshot("mau_1", domain.RuleVersion, sections, WordTemplate("mau_1"))
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFixed(start)
	publisher := &templateapp.Service{DB: database, Clock: clk, LegacyTemplates: map[string]port.KHTCTemplateSnapshot{"mau_1": legacy}}
	version, err := publisher.Save(admin, "mau_1", templateapp.SaveInput{Definition: templatedomain.Seed("mau_1")}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	gate := gatedFixedSource{KHTCFixedTemplates: publisher, reached: make(chan struct{}, 1), release: make(chan struct{})}
	svc := Service{DB: database, Clock: clk, Forms: emptySubmittedForms{}, Templates: gate}
	done := make(chan error, 1)
	go func() {
		_, e := svc.Save(province, period.ID, SaveInput{BaseVersion: 0}, ids.New().String())
		done <- e
	}()
	select {
	case <-gate.reached:
	case err = <-done:
		t.Fatalf("save ended before selection gate: %v", err)
	case <-base.Done():
		t.Fatal(base.Err())
	}
	_, err = publisher.Publish(admin, "mau_1", templateapp.PublishInput{BaseVersion: 1, VersionID: version.ID, AppliesFrom: "2026-10-07"}, ids.New().String())
	close(gate.release)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-base.Done():
		t.Fatal(base.Err())
	}
	var conflict *problem.Problem
	if !errors.As(err, &conflict) || conflict.Code != ErrVersion.Code || conflict.Status != http.StatusConflict {
		t.Fatal("stale first save not rejected with409", err)
	}
	var prior *reportstore.Draft
	if err = tenant.WithTenantTx(province, database, tid, func(tx pgx.Tx) error {
		var e error
		prior, e = reportstore.LatestDraft(province, tx, period.ID)
		return e
	}); err != nil || prior != nil {
		t.Fatal("conflict persisted draft", err, prior)
	}
	svc.Templates = publisher
	first, err := svc.Save(province, period.ID, SaveInput{BaseVersion: 0}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if first.Content.TemplateSnapshot == nil || first.Content.TemplateSnapshot.ID != version.ID {
		t.Fatal("retry did not pin publication")
	}
	changed := templatedomain.Seed("mau_1")
	changed.Nodes = append(changed.Nodes, templatedomain.Node{ID: "later", Parent: "B", Order: 100, Title: "Bản sau", Repeat: "none", Body: "Không được ghim lại lịch sử"})
	second, err := publisher.Save(admin, "mau_1", templateapp.SaveInput{BaseVersion: 1, Definition: changed}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.Publish(admin, "mau_1", templateapp.PublishInput{BaseVersion: 2, VersionID: second.ID, AppliesFrom: "2026-10-07"}, ids.New().String()); err != nil {
		t.Fatal(err)
	}
	revised, err := svc.Save(province, period.ID, SaveInput{BaseVersion: first.Version, Texts: map[string]string{"B": "Nội dung revision mới"}}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if revised.Version != first.Version+1 || revised.Content.TemplateSnapshot.ID != version.ID || revised.Content.TemplateSnapshot.Hash != first.Content.TemplateSnapshot.Hash {
		t.Fatal("existing revision rebound latest publication")
	}
}
