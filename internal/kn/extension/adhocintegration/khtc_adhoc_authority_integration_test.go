package adhocintegration_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	formapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/app"
	formdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func adhocOwnerContext(t *testing.T, f *reportFx, role, dept string, actor uuid.UUID, mfa, readonly bool, session uuid.UUID) context.Context {
	t.Helper()
	if session == uuid.Nil {
		session = uuid.NewSHA1(f.tid, []byte(actor.String()+":"+role+":"+dept))
	}
	k := authz.KNContext{ID: session, Role: role, Department: dept, OrgRef: f.node, ViewOnly: readonly, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		k.OrgRef = uuid.Nil
		k.Department = ""
		k.Scope = authz.Scope{All: true}
	}
	principal := bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: f.tid, KN: &k, MFA: mfa})
	return authz.WithPrincipal(tenant.WithID(context.Background(), f.tid), principal)
}
func assertAdhocStatus(t *testing.T, err error, status int) {
	t.Helper()
	var p *problem.Problem
	if !errors.As(err, &p) || p.Status != status {
		t.Fatalf("want status%d got %v", status, err)
	}
}
func adhocOpenInput() periodapp.OpenInput {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 12)
	due := to.AddDate(0, 0, 7)
	return periodapp.OpenInput{Request: pd.Request{Type: pd.TypeAdhoc, Year: 2026, Name: "Phát sinh biên nhận", From: &from, To: &to}, Overrides: pd.Overrides{DueAt: &due, ExportDueAt: &due}, Departments: []string{dTT}}
}

type lostAdhocPublisher struct {
	forms.Publisher
	after    atomic.Bool
	mu       sync.Mutex
	receipts []uuid.UUID
}

func (p *lostAdhocPublisher) PublishBatchOnce(ctx context.Context, in forms.BatchInput, key string) ([]forms.PublishedPeriod, error) {
	durable, ok := p.Publisher.(forms.DurablePublisher)
	if !ok {
		return nil, errors.New("durable port missing")
	}
	out, err := durable.PublishBatchOnce(ctx, in, key)
	if err == nil {
		p.mu.Lock()
		for _, r := range out {
			p.receipts = append(p.receipts, r.ID)
		}
		p.mu.Unlock()
		if p.after.Swap(false) {
			return nil, context.DeadlineExceeded
		}
	}
	return out, err
}

type failingOwnedInsert struct {
	tenant.Beginner
	fail atomic.Bool
}
type failingOwnedTx struct {
	pgx.Tx
	owner *failingOwnedInsert
}
type failedOwnedRow struct{}

func (failedOwnedRow) Scan(...any) error { return errors.New("injected own-period commit failure") }
func (d *failingOwnedInsert) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &failingOwnedTx{Tx: tx, owner: d}, nil
}
func (t *failingOwnedTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "INSERT INTO pf_extension.khtc_period (") && t.owner.fail.Swap(false) {
		return failedOwnedRow{}
	}
	return t.Tx.QueryRow(ctx, query, args...)
}
func TestW1168AdhocPublishLostResponseAndOwnCommitFailure(t *testing.T) {
	for _, mode := range []string{"native-response", "own-commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newReportFx(t)
			pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
			db := &failingOwnedInsert{Beginner: pool}
			publisher := &lostAdhocPublisher{Publisher: f.nativeForms}
			if mode == "native-response" {
				publisher.after.Store(true)
			} else {
				db.fail.Store(true)
			}
			service := periodapp.Service{DB: db, Clock: f.clk, Forms: publisher, Control: f.nativeControl, MaxSubmissions: 3}
			ctx := adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, uuid.Nil)
			input := adhocOpenInput()
			if _, err := service.Open(ctx, input, "lost-publish"); err == nil {
				t.Fatal("injected failure not reached")
			}
			recovered, err := service.Open(ctx, input, "lost-publish")
			if err != nil {
				t.Fatal(err)
			}
			again, err := service.Open(ctx, input, "lost-publish")
			if err != nil || again.Period.ID != recovered.Period.ID {
				t.Fatal("own receipt changed", err)
			}
			if len(publisher.receipts) != 2 || publisher.receipts[0] != publisher.receipts[1] || publisher.receipts[0] != recovered.Period.FormsPeriodRef {
				t.Fatalf("native duplicate receipts %v", publisher.receipts)
			}
			changed := input
			changed.Request.Name = "Payload khác"
			_, err = service.Open(ctx, changed, "lost-publish")
			assertAdhocStatus(t, err, 409)
		})
	}
}
func TestW1168AdhocPublishConcurrentSingleBatch(t *testing.T) {
	f := newReportFx(t)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	service := periodapp.Service{DB: pool, Clock: f.clk, Forms: f.nativeForms, Control: f.nativeControl, MaxSubmissions: 3}
	ctx := adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, uuid.Nil)
	idsOut := make(chan uuid.UUID, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := service.Open(ctx, adhocOpenInput(), "concurrent-publish")
			if err != nil {
				errs <- err
			} else {
				idsOut <- d.Period.FormsPeriodRef
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(idsOut)
	for err := range errs {
		t.Fatal(err)
	}
	var first uuid.UUID
	count := 0
	for id := range idsOut {
		if first == uuid.Nil {
			first = id
		}
		if id != first {
			t.Fatal("two native batches")
		}
		count++
	}
	if count != 2 {
		t.Fatal("missing receipt")
	}
}
func TestW1168AdminOperationRequiresRealMetadataAndExactBinding(t *testing.T) {
	f := newReportFx(t)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, uuid.Nil)
	service := periodapp.Service{DB: pool, Clock: f.clk, Forms: f.nativeForms, Control: f.nativeControl, MaxSubmissions: 3}
	d, err := service.Open(ctx, adhocOpenInput(), "proof-publish")
	if err != nil {
		t.Fatal(err)
	}
	principal, k, err := ext.RequireSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var intent *periodstore.AdhocOpenIntent
	if err = tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
		var e error
		intent, e = periodstore.AdhocOpenByKey(ctx, tx, "khtc:proof-publish")
		return e
	}); err != nil || intent == nil {
		t.Fatal("missing durable intent", err)
	}
	op := forms.Operation{Action: forms.OperationPublishBatch, Profile: pd.ProfileKHTC, PayloadHash: intent.BatchHash, IdemKey: intent.NativeKey}
	bind := func(ctx context.Context, proof ext.KHTCOperationProof) context.Context {
		return ext.WithKHTCOperation(ctx, periodapp.OperationAuthority{DB: pool}, proof)
	}
	proof := ext.KHTCOperationProof{IntentID: intent.ID, SessionID: k.ID, Expected: op}
	granted, err := ext.AuthorizeKHTCOperation(bind(ctx, proof), principal, op)
	if err != nil || !granted.Allowed || !strings.Contains(granted.Source, intent.ID.String()) {
		t.Fatal("valid metadata denied", granted, err)
	}
	for _, mode := range []string{"no-marker", "hash", "action", "profile", "intent", "session", "metadata-session", "actor", "readonly", "mfa", "tenant"} {
		t.Run(mode, func(t *testing.T) {
			candidate, request, scope := proof, op, ctx
			switch mode {
			case "hash":
				request.PayloadHash = "bad"
				candidate.Expected = request
			case "action":
				request.Action = "reopen"
				candidate.Expected = request
			case "profile":
				request.Profile = "kn_cnts"
				candidate.Expected = request
			case "intent":
				candidate.IntentID = ids.New()
			case "session":
				candidate.SessionID = ids.New()
			case "metadata-session":
				scope = adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, ids.New())
				_, current, _ := ext.RequireSession(scope)
				candidate.SessionID = current.ID
			case "actor":
				scope = adhocOwnerContext(t, f, authz.KNAdmin, "", ids.New(), true, false, k.ID)
			case "readonly":
				scope = adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, true, k.ID)
			case "mfa":
				scope = adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, false, false, k.ID)
			case "tenant":
				p := principal
				p.TenantID = ids.New()
				scope = authz.WithPrincipal(tenant.WithID(ctx, p.TenantID), p)
			}
			p, _, e := ext.RequireSession(scope)
			if e != nil {
				t.Fatal(e)
			}
			linked := scope
			if mode != "no-marker" {
				linked = bind(scope, candidate)
			}
			grant, e := ext.AuthorizeKHTCOperation(linked, p, request)
			if e == nil && grant.Allowed {
				t.Fatal("forged operation authorized")
			}
		})
	}
	// Real native calls retain the default guard when no trusted binding exists.
	_, err = f.nativeForms.PublishBatch(ctx, intent.Batch, "native-no-marker")
	assertAdhocStatus(t, err, 403)
	_, err = f.nativeEntry.Submit(bind(ctx, proof), pd.ProfileKHTC, d.Period.FormsPeriodRef, dTT, map[string]string{formdomain.FieldSheet: ids.New().String()}, 0, "admin-cannot-submit")
	assertAdhocStatus(t, err, 403)
	fixed := f.open(41, dTT)
	fixedID := uuid.MustParse(fixed.ID)
	nativeFixed := uuid.MustParse(fixed.FormsPeriodID)
	operation := forms.Operation{Action: forms.OperationClose, Profile: pd.ProfileKHTC, PeriodID: nativeFixed, IdemKey: "fixed-denied"}
	linked := periodapp.BindAdhocOperation(ctx, pool, k, fixedID, operation)
	err = f.nativeControl.ClosePeriod(linked, pd.ProfileKHTC, nativeFixed, "fixed-denied")
	assertAdhocStatus(t, err, 403)
	// Publisher metadata records the task level and the actual Admin actor/grant source.
	c, body := f.do(http.MethodGet, "/v1/forms/periods/"+d.Period.FormsPeriodRef.String()+"?profile=kn_khtc", authz.KNAdmin, "", f.khtcUser, "", nil)
	f.ok(c, body, nil)
	var native struct {
		Period struct {
			PublisherLevel string `json:"publisher_level"`
			PublishedBy    string `json:"published_by"`
		} `json:"period"`
		Events []struct {
			Reason *string `json:"reason"`
			Actor  string  `json:"actor_ref"`
		} `json:"events"`
	}
	if err = json.Unmarshal(body, &native); err != nil {
		t.Fatal(err)
	}
	if native.Period.PublisherLevel != "TINH" || native.Period.PublishedBy != f.khtcUser.String() {
		t.Fatalf("identity changed: %s", body)
	}
	audit := false
	for _, e := range native.Events {
		if e.Reason != nil && strings.Contains(*e.Reason, intent.ID.String()) && e.Actor == f.khtcUser.String() {
			audit = true
		}
	}
	if !audit {
		t.Fatalf("grant source audit missing: %s", body)
	}
}
func TestW1168CorePayloadReuseConflictsBeforeNativeIO(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Khóa payload bất biến")
	r := f.request(p.ID, "core-request")
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	service := formapp.Service{DB: pool, Clock: f.clk, Entry: f.nativeEntry}
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	due, _ := time.Parse(time.RFC3339, "2026-10-19T10:00:00+07:00")
	_, err := service.RequestSupplement(ctx, uuid.MustParse(p.ID), dTT, formapp.AdhocRequestInput{Instruction: "Payload khác", DueAt: due, Indicators: []pd.AdhocIndicator{{Code: "SO_LOP", Name: "Số lớp yêu cầu mới", Unit: "lớp"}}}, "core-request")
	assertAdhocStatus(t, err, 409)
	actor := ids.New()
	c, b := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "core-submit", map[string]any{"base_attempt": 0, "sheet": answerSheet(r.ID, "1")})
	f.ok(c, b, nil)
	ctx = adhocOwnerContext(t, f, authz.KNTinh, dTT, actor, false, false, uuid.Nil)
	_, err = service.Submit(ctx, uuid.MustParse(p.ID), dTT, formapp.SaveInput{BaseAttempt: 0, Sheet: formdomain.Sheet{AdhocRequestID: r.ID, AdhocValues: map[string]string{"SO_LOP": "2"}}}, "core-submit")
	assertAdhocStatus(t, err, 409)
}
