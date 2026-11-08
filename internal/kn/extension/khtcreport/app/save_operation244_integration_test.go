package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type saveFixture244 struct {
	ctx            context.Context
	cancel         context.CancelFunc
	svc            *Service
	pool           *pgxpool.Pool
	admin          *pgxpool.Pool
	tid, actor, id uuid.UUID
	principal      authz.Principal
	owner          port.SessionContext
	base           int64
}
type poolProbe244 struct {
	forms.Entry
	pool  *pgxpool.Pool
	calls int
}

func (p *poolProbe244) Submissions(ctx context.Context, _ string, _ uuid.UUID, _ string) ([]forms.SubmissionRef, error) {
	p.calls++
	// With pool1 this times out if a report transaction still holds the connection.
	if err := p.pool.Ping(ctx); err != nil {
		return nil, err
	}
	return []forms.SubmissionRef{}, nil
}
func newSaveFixture244(t *testing.T) *saveFixture244 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	env := pgtest.Get(t)
	cfg := env.Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tid, actor := ids.New(), ids.New()
	owner := port.SessionContext{ID: ids.New(), AssignmentID: ids.New(), Role: port.SessionRoleProvince, Department: "ke_hoach_tai_chinh", OrgRef: ids.New(), Scope: authz.Scope{All: true}}
	session, err := port.NewSessionContext(owner)
	if err != nil {
		t.Fatal(err)
	}
	principal := authz.Principal{TenantID: tid, ActorRef: actor, SelectedSession: &session}
	ctx = tenant.WithID(authz.WithPrincipal(ctx, principal), tid)
	database := db.ForModule(pool, db.Module("pf/extension"))
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	no, key := 41, "2026-W41"
	period := ps.Period{ID: ids.New(), Type: pd.TypeWeek, Year: 2026, No: &no, Key: &key, Name: "Kỳ kiểm receipt lưu nháp", From: at, To: at.AddDate(0, 0, 6), Cutoff: at.AddDate(0, 0, 6), DueAt: at.AddDate(0, 0, 7), ExportDueAt: at.AddDate(0, 0, 8), MaxSubmissions: 3, Departments: []string{"trong_trot"}, FormsPeriodRef: ids.New(), TemplateCode: pd.TemplateCode, Rule: pd.RuleVersion, ActorRef: actor, EventTime: at}
	draft := store.Draft{ID: ids.New(), PeriodID: period.ID, Action: "refresh", ActorRef: actor, At: at, InputHash: strings.Repeat("1", 64), Content: domain.Draft{Data: domain.Data{Template: "mau_1", Period: domain.Period{ID: period.ID, Type: pd.TypeWeek, Year: 2026, From: at, To: period.To, Cutoff: period.Cutoff, Departments: period.Departments}, InputHash: strings.Repeat("1", 64)}, Admin: map[string]domain.AdminField{"signer": {System: "Người ký hệ thống"}}, Texts: map[string]string{"B": "Nội dung ban đầu"}}}
	err = tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error {
		if e := ps.InsertPeriod(ctx, tx, tid, &period, ids.New().String()); e != nil {
			return e
		}
		return store.InsertDraft(ctx, tx, tid, &draft, ids.New().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: database, Clock: clock.NewFixed(at), Forms: &poolProbe244{pool: pool}}
	return &saveFixture244{ctx: ctx, cancel: cancel, svc: svc, pool: pool, admin: env.AdminPool(t, migrations.PF), tid: tid, actor: actor, id: period.ID, principal: principal, owner: owner, base: draft.Version}
}
func (f *saveFixture244) counts(t *testing.T) (int, int, int) {
	t.Helper()
	var drafts, receipts, outbox int
	if err := f.admin.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM pf_extension.khtc_report_draft WHERE tenant_id=$1),(SELECT count(*) FROM pf_extension.khtc_report_save_operation WHERE tenant_id=$1),(SELECT count(*) FROM shared.outbox WHERE tenant_id=$1)`, f.tid).Scan(&drafts, &receipts, &outbox); err != nil {
		t.Fatal(err)
	}
	return drafts, receipts, outbox
}
func expectCode244(t *testing.T, err error, code string) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("want %s got %v", code, err)
	}
}

func TestReportSave244_AtomicReceiptReplayLostACKSuccessorAndABA(t *testing.T) {
	f := newSaveFixture244(t)
	op, key := ids.New(), ids.New().String()
	signer := "Người ký đã sửa"
	in := SaveInput{OperationID: op, BaseVersion: f.base, Admin: map[string]*string{"signer": &signer}, Texts: map[string]string{"B": "Nội dung một"}}
	saved, err := f.svc.Save(f.ctx, f.id, in, key)
	if err != nil {
		t.Fatal(err)
	}
	receipt := saved.SaveReceipt
	if receipt == nil || receipt.Outcome != "applied" || receipt.Applied.ActorRef != f.actor || len(receipt.Applied.Changes) != 2 || receipt.Applied.BaseDraftID == nil {
		t.Fatalf("missing atomic audit: %+v", receipt)
	}
	firstVersion := receipt.Applied.Version
	if !receipt.Applied.At.Equal(f.svc.Clock.Now()) {
		t.Fatal("audit time not actual mutation time")
	}
	next, err := f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: saved.Version, Texts: map[string]string{"B": "Nội dung hai"}}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	next, err = f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: next.Version, Texts: map[string]string{"B": "Nội dung một"}}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.svc.Save(f.ctx, f.id, in, key)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Version != next.Version || replay.SaveReceipt.Applied.Version != firstVersion || replay.SaveReceipt.Applied.DraftID != receipt.Applied.DraftID {
		t.Fatal("replay falsely acknowledged successor")
	}
	read, err := f.svc.ReadSaveOperation(f.ctx, f.id, op)
	if err != nil || read.Applied.Version != firstVersion {
		t.Fatal("lost ACK read", read, err)
	}
	changed := in
	changed.Texts = map[string]string{"B": "Nội dung khác"}
	_, err = f.svc.Save(f.ctx, f.id, changed, key)
	expectCode244(t, err, problem.Conflict.Code)
	changed = in
	changed.OperationID = ids.New()
	_, err = f.svc.Save(f.ctx, f.id, changed, key)
	expectCode244(t, err, problem.Conflict.Code)
	changed = in
	changed.BaseVersion = next.Version
	changed.Texts = map[string]string{"B": "Nội dung khác"}
	_, err = f.svc.Save(f.ctx, f.id, changed, ids.New().String())
	expectCode244(t, err, problem.Conflict.Code)
	unknown, err := f.svc.ReadSaveOperation(f.ctx, f.id, ids.New())
	if err != nil || unknown.Outcome != "unknown" || unknown.Applied != nil {
		t.Fatal("absence was promoted to rollback/applied", unknown, err)
	}
	audit, err := f.svc.SaveAudit(f.ctx, f.id)
	if err != nil || len(audit) != 3 || audit[2].Applied.Version != firstVersion {
		t.Fatal("actual save history", audit, err)
	}
	d, r, o := f.counts(t)
	if d != 4 || r != 3 || o != 0 {
		t.Fatal("duplicate mutation/outbox", d, r, o)
	}
	if f.svc.Forms.(*poolProbe244).calls == 0 {
		t.Fatal("no real pool1 port probe")
	}
}

func TestReportSave244_AuditInsertAndDeferredCommitRollbackThenRecovery(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprint(deferred), func(t *testing.T) {
			f := newSaveFixture244(t)
			first, err := f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: f.base}, ids.New().String())
			if err != nil {
				t.Fatal(err)
			}
			var schema, partition string
			if err = f.admin.QueryRow(f.ctx, `SELECT n.nspname,c.relname FROM pf_extension.khtc_report_save_operation a JOIN pg_class c ON c.oid=a.tableoid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE a.tenant_id=$1 LIMIT 1`, f.tid).Scan(&schema, &partition); err != nil {
				t.Fatal(err)
			}
			name := "test_save244_" + strings.ReplaceAll(ids.New().String(), "-", "")
			fn := pgx.Identifier{"pf_extension", name}.Sanitize()
			trigger := pgx.Identifier{name}.Sanitize()
			table := pgx.Identifier{"pf_extension", "khtc_report_save_operation"}.Sanitize()
			clause := "CREATE TRIGGER " + trigger + " BEFORE INSERT ON " + table + " FOR EACH ROW "
			if deferred {
				table = pgx.Identifier{schema, partition}.Sanitize()
				clause = "CREATE CONSTRAINT TRIGGER " + trigger + " AFTER INSERT ON " + table + " DEFERRABLE INITIALLY DEFERRED FOR EACH ROW "
			}
			function := `CREATE FUNCTION ` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='` + f.tid.String() + `'::uuid THEN RAISE EXCEPTION 'controlled report save audit failure'; END IF; RETURN NEW; END $$; `
			if _, err = f.admin.Exec(f.ctx, function+clause+"EXECUTE FUNCTION "+fn+"()"); err != nil {
				t.Fatal(err)
			}
			cleanup := func() {
				if _, e := f.admin.Exec(f.ctx, "DROP TRIGGER IF EXISTS "+trigger+" ON "+table+"; DROP FUNCTION IF EXISTS "+fn+"()"); e != nil {
					t.Error(e)
				}
			}
			t.Cleanup(cleanup)
			op, key := ids.New(), ids.New().String()
			in := SaveInput{OperationID: op, BaseVersion: first.Version, Texts: map[string]string{"B": "Không được commit một phần"}}
			if _, err = f.svc.Save(f.ctx, f.id, in, key); err == nil {
				t.Fatal("audit failure claimed success")
			}
			d, r, o := f.counts(t)
			if d != 2 || r != 1 || o != 0 {
				t.Fatal("draft/audit rollback not atomic", d, r, o)
			}
			unknown, err := f.svc.ReadSaveOperation(f.ctx, f.id, op)
			if err != nil || unknown.Outcome != "unknown" {
				t.Fatal("rolled-back operation has receipt", unknown, err)
			}
			cleanup()
			recovered, err := f.svc.Save(f.ctx, f.id, in, key)
			if err != nil || recovered.SaveReceipt == nil || recovered.SaveReceipt.Outcome != "applied" {
				t.Fatal("recovery", recovered.SaveReceipt, err)
			}
			d, r, o = f.counts(t)
			if d != 3 || r != 2 || o != 0 {
				t.Fatal("recovery duplicate", d, r, o)
			}
		})
	}
}

type lostCommitDB244 struct {
	tenant.Beginner
	fired bool
}
type lostCommitTx244 struct {
	pgx.Tx
	owner   *lostCommitDB244
	written bool
}

func (d *lostCommitDB244) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := d.Beginner.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return &lostCommitTx244{Tx: tx, owner: d}, nil
}
func (t *lostCommitTx244) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, e := t.Tx.Exec(ctx, sql, args...)
	if e == nil && strings.Contains(sql, "INSERT INTO pf_extension.khtc_report_save_operation") {
		t.written = true
	}
	return tag, e
}
func (t *lostCommitTx244) Commit(ctx context.Context) error {
	e := t.Tx.Commit(ctx)
	if e == nil && t.written && !t.owner.fired {
		t.owner.fired = true
		return errors.New("controlled lost COMMIT ACK after native commit")
	}
	return e
}
func TestReportSave244_RealCommitWithLostACKReadsExactReceipt(t *testing.T) {
	f := newSaveFixture244(t)
	fault := &lostCommitDB244{Beginner: f.svc.DB}
	f.svc.DB = fault
	op, key := ids.New(), ids.New().String()
	in := SaveInput{OperationID: op, BaseVersion: f.base, Texts: map[string]string{"B": "Commit đã xảy ra"}}
	if _, err := f.svc.Save(f.ctx, f.id, in, key); err == nil || !fault.fired {
		t.Fatal("lost COMMIT ACK not exercised", err)
	}
	receipt, err := f.svc.ReadSaveOperation(f.ctx, f.id, op)
	if err != nil || receipt.Outcome != "applied" || receipt.Applied.ActorRef != f.actor {
		t.Fatal("durable receipt after lost ACK", receipt, err)
	}
	if _, err = f.svc.Save(f.ctx, f.id, in, key); err != nil {
		t.Fatal(err)
	}
	d, r, o := f.counts(t)
	if d != 2 || r != 1 || o != 0 {
		t.Fatal("lost ACK replay duplicated", d, r, o)
	}
}

type countedSaveDB244 struct {
	tenant.Beginner
	calls int
}

func (d *countedSaveDB244) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	d.calls++
	return d.Beginner.BeginTx(ctx, o)
}
func TestReportSave244_CurrentAuthorityBeforeDBAndReceipt(t *testing.T) {
	f := newSaveFixture244(t)
	counted := &countedSaveDB244{Beginner: f.svc.DB}
	f.svc.DB = counted
	cases := []struct {
		name      string
		principal *authz.Principal
	}{
		{name: "missing identity"}, {name: "legacy only", principal: &authz.Principal{TenantID: f.tid, ActorRef: f.actor, KN: &authz.KNContext{Role: authz.KNAdmin}}},
	}
	for _, change := range []struct {
		name string
		fn   func(*port.SessionContext, *authz.Principal)
	}{
		{"readonly", func(k *port.SessionContext, _ *authz.Principal) { k.ViewOnly = true }},
		{"national", func(k *port.SessionContext, _ *authz.Principal) { k.Role = port.SessionRoleNational }},
		{"wrong department", func(k *port.SessionContext, _ *authz.Principal) { k.Department = "trong_trot" }},
		{"admin missing MFA", func(k *port.SessionContext, p *authz.Principal) {
			k.Role = port.SessionRoleAdmin
			k.Department = ""
			p.MFA = false
		}},
	} {
		k, p := f.owner, f.principal
		change.fn(&k, &p)
		session, e := port.NewSessionContext(k)
		if e != nil {
			t.Fatal(e)
		}
		p.SelectedSession = &session
		cases = append(cases, struct {
			name      string
			principal *authz.Principal
		}{change.name, &p})
	}
	other, e := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if e != nil {
		t.Fatal(e)
	}
	p := f.principal
	p.SelectedSession = &other
	cases = append(cases, struct {
		name      string
		principal *authz.Principal
	}{"different profile", &p})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tenant.WithID(context.Background(), f.tid)
			if tc.principal != nil {
				ctx = authz.WithPrincipal(ctx, *tc.principal)
			}
			counted.calls = 0
			_, a := f.svc.Save(ctx, f.id, SaveInput{BaseVersion: f.base}, ids.New().String())
			_, b := f.svc.ReadSaveOperation(ctx, f.id, ids.New())
			_, c := f.svc.SaveAudit(ctx, f.id)
			if a == nil || b == nil || c == nil || counted.calls != 0 {
				t.Fatal("authority reached DB", a, b, c, counted.calls)
			}
		})
	}
	saved, err := f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: f.base}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"session", "assignment", "mfa", "actor", "tenant", "scope"} {
		t.Run(change, func(t *testing.T) {
			p, k := f.principal, f.owner
			switch change {
			case "session":
				k.ID = ids.New()
			case "assignment":
				k.AssignmentID = ids.New()
			case "mfa":
				p.MFA = !p.MFA
			case "actor":
				p.ActorRef = ids.New()
			case "tenant":
				p.TenantID = ids.New()
			case "scope":
				k.Scope = authz.Scope{Units: []string{"X1"}}
			}
			session, e := port.NewSessionContext(k)
			if e != nil {
				t.Fatal(e)
			}
			p.SelectedSession = &session
			ctx := tenant.WithID(authz.WithPrincipal(context.Background(), p), p.TenantID)
			if receipt, e := f.svc.ReadSaveOperation(ctx, f.id, saved.SaveReceipt.OperationID); e == nil {
				t.Fatal("changed authority returned receipt", receipt)
			}
		})
	}
}

func TestReportSave244_FrozenReportKeepsReceiptAndRejectsNewSave(t *testing.T) {
	f := newSaveFixture244(t)
	saved, err := f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: f.base}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	exported, err := f.svc.Export(f.ctx, f.id, ExportInput{BaseVersion: saved.Version, AcknowledgeProvisional: true}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	// Establish the already-frozen storage boundary using native ledger guards;
	// Finalize business/source validation remains covered by existing consumer tests.
	err = f.svc.tx(f.ctx, f.tid, func(tx pgx.Tx) error {
		return store.InsertFinal(f.ctx, tx, f.tid, store.Final{ID: ids.New(), PeriodID: f.id, ExportID: exported.ID, ActorRef: f.actor, At: f.svc.now()}, ids.New().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := f.svc.ReadSaveOperation(f.ctx, f.id, saved.SaveReceipt.OperationID)
	if err != nil || read.Applied.DraftID != saved.SaveReceipt.Applied.DraftID || !read.Applied.At.Equal(saved.SaveReceipt.Applied.At) {
		t.Fatal("frozen receipt changed", read, err)
	}
	_, err = f.svc.Save(f.ctx, f.id, SaveInput{BaseVersion: saved.Version, Texts: map[string]string{"B": "Không được đổi bản cuối"}}, ids.New().String())
	expectCode244(t, err, ErrFinal.Code)
	replay, err := f.svc.Save(f.ctx, f.id, SaveInput{OperationID: saved.SaveReceipt.OperationID, BaseVersion: f.base}, "different-key")
	if err == nil || replay.SaveReceipt != nil {
		t.Fatal("different original command cannot borrow a frozen receipt", err)
	}
	d, r, _ := f.counts(t)
	if d != 2 || r != 1 {
		t.Fatal("frozen boundary wrote draft/receipt", d, r)
	}
}
