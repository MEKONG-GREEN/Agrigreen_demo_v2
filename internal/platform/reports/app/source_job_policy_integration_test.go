package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }
func TestSourceJobPolicyPostgresLifecycle(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	principal := sourceTestPrincipal()
	ctx := sourceTestContext(principal)
	active, readonly, ownerDown, buildDown := true, false, false, false
	ops := map[port.SourceJobOperation]int{}
	var builtIDs []uuid.UUID
	svc := ownerPolicyService(now, func(c context.Context, a port.SourceJobAccess) error {
		ops[a.Operation]++
		tid, e := tenant.FromContext(c)
		if e != nil || tid != principal.TenantID || a.TenantID != principal.TenantID || a.RequestedBy != principal.ActorRef {
			return problem.Forbidden
		}
		if a.Operation == port.SourceJobBuild {
			if a.ActorRef != uuid.Nil {
				t.Fatal("worker fabricated principal")
			}
		} else {
			p, ok := authz.FromContext(c)
			if !ok || !p.MFA || a.ActorRef != p.ActorRef || p.ActorRef != principal.ActorRef {
				return problem.Forbidden
			}
		}
		if ownerDown {
			return errors.New("private-marker dependency")
		}
		if !active || readonly {
			return problem.Forbidden.WithDetail("private-marker rights")
		}
		a.SourceArgs[2] = 'X'
		return nil
	}, func(_ context.Context, q port.SourceBuildQuery) (port.SourceData, error) {
		if buildDown {
			return port.SourceData{}, errors.New("private-marker build failure")
		}
		if q.JobID == uuid.Nil || q.RequestedBy != principal.ActorRef || q.TenantID != principal.TenantID || q.ArgsVersion != 1 || !bytes.Contains(q.SourceArgs, []byte("private-marker")) {
			t.Fatalf("builder received unbound data: %+v", q)
		}
		builtIDs = append(builtIDs, q.JobID)
		q.SourceArgs[2] = 'X'
		return port.SourceData{Rows: []port.SourceRow{{"amount": {Text: "1"}}}}, nil
	})
	svc.DB = env.Pool(t, migrations.PF, "reports_app")
	objects, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("source-policy-presign-secret-32bytes"), BaseURL: "https://files.test"})
	if err != nil {
		t.Fatal(err)
	}
	svc.Store = objects
	input := ExportInput{TemplateKey: "example.layout", Format: "xlsx", Params: domain.Params{Level: "province"}, IdemKey: "owner-job-01", SourceArgs: json.RawMessage(`{"filter":"private-marker"}`)}
	j, replay, err := svc.RequestExport(ctx, input)
	if err != nil || replay {
		t.Fatalf("request: %v replay %v", err, replay)
	}
	if j.ScopeAll || len(j.ScopeUnits) > 0 || j.SourceArgs == nil {
		t.Fatal("tenant source must retain empty geography")
	}
	var read *domain.Job
	if err = svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error {
		var e error
		read, e = store.JobByID(ctx, tx, j.ID, false)
		return e
	}); err != nil || read == nil || read.ValidateSourceArgs() != nil || !bytes.Equal(j.SourceArgs.Args, read.SourceArgs.Args) {
		t.Fatalf("PG private snapshot roundtrip: %v", err)
	}
	public, _ := json.Marshal(read)
	if bytes.Contains(public, []byte("private-marker")) || bytes.Contains(public, []byte("_reports_source")) {
		t.Fatal("public Job leaked snapshot")
	}
	var outboxBody string
	if err = svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT payload::text FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2 ORDER BY recorded_at DESC LIMIT 1", principal.TenantID, port.EventExportRequested).Scan(&outboxBody)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(outboxBody, "private-marker") || strings.Contains(outboxBody, "source_args") {
		t.Fatal("event leaked source arguments")
	}
	svc.Clock = clock.NewFixed(now.Add(24 * time.Hour))
	cached, replay, err := svc.RequestExport(ctx, input)
	if err != nil || !replay || cached.ID != j.ID || !cached.RequestedAt.Equal(now) {
		t.Fatalf("replay changed snapshot: %v", err)
	}
	changed := input
	changed.SourceArgs = json.RawMessage(`{"filter":"other"}`)
	if _, _, err = svc.RequestExport(ctx, changed); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatalf("different args replay accepted: %v", err)
	}
	if err = svc.CheckExportReplay(ctx, input); err != nil {
		t.Fatal(err)
	}
	// Process through the existing worker, object store, and completion ledger.
	run := func(job domain.Job, envelopeTenant uuid.UUID) error {
		payload, _ := json.Marshal(port.ExportRequestedV1{JobID: job.ID.String(), TemplateKey: job.TemplateKey, Format: job.Format, RequestedBy: job.RequestedBy.String(), RequestedAt: ts(job.RequestedAt)})
		workerCtx := tenant.WithID(context.Background(), envelopeTenant)
		return svc.tx(workerCtx, envelopeTenant, func(tx pgx.Tx) error {
			return svc.HandleExportRequested(workerCtx, tx, outbox.Envelope{
				TenantID: envelopeTenant, EventID: ids.New(), EventType: port.EventExportRequested, Payload: payload,
			})
		})
	}
	if err = run(j, principal.TenantID); err != nil {
		t.Fatal(err)
	}
	if len(builtIDs) != 1 || builtIDs[0] != j.ID {
		t.Fatal("worker did not use actual job identity")
	}
	completed, err := svc.GetExport(ctx, j.ID)
	if err != nil || completed.Status != domain.JobCompleted {
		t.Fatalf("worker status: %v %+v", err, completed)
	}
	if _, err = svc.Download(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	// Revoke or make the owner read-only: all paths, including cache precheck,
	// must stop before returning a stored job or a signed artifact URL.
	for _, mode := range []string{"revoked", "readonly", "dependency"} {
		active, readonly, ownerDown = mode != "revoked", mode == "readonly", mode == "dependency"
		checks := []func() error{
			func() error { _, _, e := svc.RequestExport(ctx, input); return e },
			func() error { return svc.CheckExportReplay(ctx, input) },
			func() error { _, e := svc.GetExport(ctx, j.ID); return e },
			func() error { _, e := svc.MyExports(ctx, uuid.Nil, 10); return e },
			func() error { _, e := svc.Download(ctx, j.ID); return e },
			func() error {
				_, e := svc.buildDataset(tenant.WithID(context.Background(), principal.TenantID), j)
				return e
			},
		}
		for _, check := range checks {
			if e := check(); e == nil || strings.Contains(e.Error(), "private-marker") {
				t.Fatalf("%s returned job/private error: %v", mode, e)
			}
		}
	}
	active, readonly, ownerDown = true, false, false
	foreign := principal
	foreign.ActorRef = uuid.New()
	if _, _, err = svc.RequestExport(sourceTestContext(foreign), input); !errors.Is(err, problem.IdempotencyConflict) {
		t.Fatalf("foreign requester replay: %v", err)
	}
	otherTenant := principal
	otherTenant.TenantID = uuid.New()
	if _, err = svc.GetExport(sourceTestContext(otherTenant), j.ID); !errors.Is(err, ErrExportNotFound) {
		t.Fatalf("cross-tenant leaked job: %v", err)
	}
	// Builder failures remain generic and do not persist the source's error text.
	second := input
	second.IdemKey = "owner-job-02"
	j2, _, err := svc.RequestExport(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	buildDown = true
	if err = run(j2, principal.TenantID); err == nil || strings.Contains(err.Error(), "private-marker") {
		t.Fatalf("private builder failure leaked: %v", err)
	}
	buildDown = false
	// Revocation at worker execution does not call the builder or create a file.
	third := input
	third.IdemKey = "owner-job-03"
	j3, _, err := svc.RequestExport(ctx, third)
	if err != nil {
		t.Fatal(err)
	}
	active = false
	if err = run(j3, principal.TenantID); err != nil {
		t.Fatal(err)
	}
	active = true
	failed, err := svc.GetExport(ctx, j3.ID)
	if err != nil || failed.Status != domain.JobFailed || failed.ObjectKey != nil {
		t.Fatalf("revoked worker produced artifact: %v %+v", err, failed)
	}
	// Even an old schedule cannot start a custom job without a per-run freeze.
	sch := domain.Schedule{ID: ids.New(), TenantID: principal.TenantID, TemplateKey: "example.layout", Format: "xlsx",
		Params: domain.Params{Level: "province"}, Frequency: domain.FreqMonthly, DayOfMonth: 1, Hour: 10, Recipients: []uuid.UUID{principal.ActorRef},
		OwnerRef: principal.ActorRef, OwnerRoles: principal.Roles, ScopeAll: true, Active: true, CreatedAt: now, UpdatedAt: now, IdemKey: "owner-schedule-old"}
	if err = svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error { return store.InsertSchedule(ctx, tx, &sch) }); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.CreateSchedule(ctx, ScheduleInput{Schedule: sch, IdemKey: sch.IdemKey}); err == nil {
		t.Fatal("old custom schedule replay accepted")
	}
	if fired, e := svc.fire(ctx, sch.ID, principal.TenantID, now, "2026-10", now); e != nil || fired {
		t.Fatalf("custom schedule fired: %v %v", fired, e)
	}
	for _, op := range []port.SourceJobOperation{port.SourceJobRequest, port.SourceJobReplay, port.SourceJobList, port.SourceJobDetail, port.SourceJobDownload, port.SourceJobBuild} {
		if ops[op] == 0 {
			t.Fatalf("authorization operation missing: %s", op)
		}
	}
}

func TestSourceArgsNumericPostgresReplay(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	principal := sourceTestPrincipal()
	ctx := sourceTestContext(principal)
	builder := func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil }
	policy := port.SourceJobPolicy{Version: 1, ScopeMode: port.SourceScopeTenant,
		Freeze:       func(_ context.Context, q port.SourceFreezeRequest) (json.RawMessage, error) { return q.SourceArgs, nil },
		AuthorizeJob: func(context.Context, port.SourceJobAccess) error { return nil },
	}
	svc := Service{DB: env.Pool(t, migrations.PF, "reports_app"), Clock: clock.NewFixed(now), Catalog: NewCatalogRegistry(testCatalogRegistration()),
		Builders: NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: builder, JobPolicy: &policy})}
	in := ExportInput{TemplateKey: "example.layout", Format: "xlsx", IdemKey: "number-jsonb-01",
		SourceArgs: json.RawMessage(`{"small":1.230e-5,"zero":-0,"one":1.00,"large":1234567890123456789012345678901234567890}`)}
	first, replay, err := svc.RequestExport(ctx, in)
	if err != nil || replay {
		t.Fatalf("numeric request: %v", err)
	}
	// JSONB expands exponent/scale while preserving the numeric value exactly.
	svc.Clock = clock.NewFixed(now.Add(time.Hour))
	again, replay, err := svc.RequestExport(ctx, in)
	if err != nil || !replay || first.ID != again.ID || !bytes.Equal(first.SourceArgs.Args, again.SourceArgs.Args) {
		t.Fatalf("exact number changed through JSONB: %v", err)
	}
	in.SourceArgs = json.RawMessage(`{"small":0.00001230,"zero":0,"one":1,"large":123456789012345678901234567890123456789e1}`)
	if _, replay, err = svc.RequestExport(ctx, in); err != nil || !replay {
		t.Fatalf("equivalent decimal spelling changed snapshot: %v", err)
	}
}

type sourceDeliverySpyStore struct {
	objstore.Store
	calls int
}

func (s *sourceDeliverySpyStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	s.calls++
	return "https://files.test/should-not-send", nil
}

type sourceDeliverySpyMailer struct{ calls int }

func (m *sourceDeliverySpyMailer) Send(context.Context, port.Message) error { m.calls++; return nil }

type sourceDeliveryDirectory struct{}

func (sourceDeliveryDirectory) EmailOf(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "fixture@example.test", nil
}

func TestSourcePolicyPendingScheduledDeliveryDenied(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	for _, withPolicy := range []bool{true, false} {
		principal := sourceTestPrincipal()
		ctx := sourceTestContext(principal)
		svc := ownerPolicyService(now, func(context.Context, port.SourceJobAccess) error { return problem.Forbidden },
			func(context.Context, port.SourceBuildQuery) (port.SourceData, error) {
				t.Fatal("delivery called source builder")
				return port.SourceData{}, nil
			})
		if !withPolicy {
			svc.Builders = nil
		}
		svc.DB = env.Pool(t, migrations.PF, "reports_app")
		spy := &sourceDeliverySpyStore{}
		mailer := &sourceDeliverySpyMailer{}
		svc.Store, svc.Mailer, svc.Directory = spy, mailer, sourceDeliveryDirectory{}
		sid := ids.New()
		slot := "2026-10"
		sch := domain.Schedule{ID: sid, TenantID: principal.TenantID, TemplateKey: "example.layout", Format: "xlsx",
			Params: domain.Params{Level: "province"}, Frequency: domain.FreqMonthly, DayOfMonth: 1, Hour: 10, Recipients: []uuid.UUID{principal.ActorRef},
			OwnerRef: principal.ActorRef, OwnerRoles: principal.Roles, ScopeAll: true, Active: true, CreatedAt: now, UpdatedAt: now, IdemKey: "pending-schedule"}
		template, _ := svc.builtinTemplate("example.layout")
		j := domain.Job{ID: ids.New(), TenantID: principal.TenantID, TemplateKey: template.Key, TemplateVersion: template.Version, Def: template.Def, Format: "xlsx",
			Params: domain.Params{Level: "province"}, ScopeAll: true, RequestedBy: principal.ActorRef, RequestedAt: now,
			Trigger: domain.TriggerSchedule, ScheduleID: &sid, ScheduleSlot: &slot, IdemKey: "pending-schedule-job"}
		if !withPolicy {
			j.SourceArgs = &domain.FrozenSourceArgs{Source: j.Def.Source, Version: 1, JobID: j.ID, TenantID: j.TenantID, RequestedBy: j.RequestedBy, RequestedAt: j.RequestedAt,
				TemplateKey: j.TemplateKey, TemplateVersion: j.TemplateVersion, Format: j.Format, Args: json.RawMessage("{}")}
		}
		if err := svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error {
			if err := store.InsertSchedule(ctx, tx, &sch); err != nil {
				return err
			}
			if err := store.InsertJob(ctx, tx, &j); err != nil {
				return err
			}
			if _, err := store.ClaimRun(ctx, tx, principal.TenantID, store.Run{ScheduleID: sid, SlotKey: slot, SlotAt: now, Status: "queued", JobID: &j.ID, FiredAt: now}); err != nil {
				return err
			}
			return store.CompleteJob(ctx, tx, j.ID, domain.ObjectKey(j.TenantID, j.ID, j.Format), "checksum", 10, 1, false, now)
		}); err != nil {
			t.Fatal(err)
		}
		scheduleRef := sid.String()
		payload, _ := json.Marshal(port.ExportCompletedV1{JobID: j.ID.String(), ScheduleID: &scheduleRef})
		for repeat := 0; repeat < 2; repeat++ {
			if err := svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error {
				return svc.HandleExportCompleted(ctx, tx, outbox.Envelope{TenantID: principal.TenantID, Payload: payload})
			}); err != nil {
				t.Fatal(err)
			}
		}
		if spy.calls != 0 || mailer.calls != 0 {
			t.Fatalf("custom pending delivery escaped: presign=%d mail=%d", spy.calls, mailer.calls)
		}
		if err := svc.tx(ctx, principal.TenantID, func(tx pgx.Tx) error {
			run, err := store.RunByJob(ctx, tx, j.ID)
			if err != nil {
				return err
			}
			if run.DeliveryStatus == nil || *run.DeliveryStatus != DeliverySkipped || run.SentCount == nil || *run.SentCount != 0 {
				t.Fatal("delivery denial was not durably recorded")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
