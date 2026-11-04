package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pa "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	rd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	rs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	fp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func submitter211(t *testing.T, f *deadlineFx209) context.Context {
	t.Helper()
	p := fixture.Bind(t, authz.Principal{ActorRef: uuid.New(), TenantID: f.tid, MFA: true, KN: &authz.KNContext{ID: uuid.New(), Role: authz.KNTinh, Department: "trong_trot", OrgRef: f.node, Scope: authz.Scope{All: true}}})
	return authz.WithPrincipal(context.Background(), p)
}
func openAdhoc211(t *testing.T, f *deadlineFx209, s *pa.Service, ctx context.Context) pa.Detail {
	t.Helper()
	_ = f.open209()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(12 * 24 * time.Hour)
	due := f.clk.Now().Add(24 * time.Hour)
	export := due.Add(24 * time.Hour)
	d, err := s.Open(ctx, pa.OpenInput{Request: pd.Request{Type: pd.TypeAdhoc, Year: 2026, Name: "Kỳ phát sinh Close211", From: &from, To: &to}, Overrides: pd.Overrides{DueAt: &due, ExportDueAt: &export}, Departments: []string{"trong_trot"}}, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func nativeHandler211(f *deadlineFx209) http.Handler {
	r := gin.New()
	f.fm.RegisterApp211(r.Group("/v1"))
	return r
}
func heldSubmission211(t *testing.T, f *deadlineFx209, ctx context.Context, period uuid.UUID, key string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"unit": "trong_trot", "values": map[string]string{"ket_qua": "Bản offline211"}, "captured_at": f.clk.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "base_version": 0})
	req := httptest.NewRequest(http.MethodPost, "/v1/forms/periods/"+period.String()+"/submissions?profile=kn_khtc", strings.NewReader(string(raw))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	nativeHandler211(f).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("offline held status=%d", rec.Code)
	}
}
func TestClose211AdhocAcceptedSubmissionAfterGetUsesOwnedRequestProjection(t *testing.T) {
	f := newDeadlineFx209(t)
	native := f.fm.ConditionalControl()
	probe := &casProbe209{ConditionalPeriodControl: native}
	s, ctx := closeConsumer211(t, f, probe)
	d := openAdhoc211(t, f, s, ctx)
	f.clk.Advance(d.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	requestDue := f.clk.Now().Add(2 * time.Hour)
	request := ps.AdhocRequest{ID: uuid.New(), PeriodID: d.Period.ID, ActorRef: f.actor, Department: "trong_trot", Instruction: "Chỉ dẫn bổ sung phải giữ211", Indicators: []pd.AdhocIndicator{}, DueAt: requestDue, At: f.clk.Now(), PayloadHash: strings.Repeat("a", 64), IdemKey: "request211"}
	if err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		if err := ps.InsertAdhocRequest(ctx, tx, f.tid, &request); err != nil {
			return err
		}
		return ps.InsertAdhocRequestEvent(ctx, tx, f.tid, ps.AdhocRequestEvent{ID: uuid.New(), RequestID: request.ID, PeriodID: d.Period.ID, ActorRef: f.actor, Action: "issued", At: f.clk.Now(), IdemKey: "issued211"})
	}); err != nil {
		t.Fatal(err)
	}
	sender := submitter211(t, f)
	var once sync.Once
	probe.snapshotHook = func(_ context.Context, snapshot fp.PeriodControlSnapshot) {
		if snapshot.Progress.Status != "open" {
			return
		}
		once.Do(func() {
			sub, err := f.fm.Entry211().Submit(sender, "kn_khtc", d.Period.FormsPeriodRef, "trong_trot", map[string]string{"ket_qua": "Mới giữa Get và Close211"}, 0, "new-submission211")
			if err != nil {
				t.Fatal(err)
			}
			if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
				return ps.InsertAdhocRequestEvent(ctx, tx, f.tid, ps.AdhocRequestEvent{ID: uuid.New(), RequestID: request.ID, PeriodID: d.Period.ID, ActorRef: f.actor, Action: "fulfilled", NativeSubmissionRef: &sub.ID, At: f.clk.Now(), IdemKey: "fulfilled211"})
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	out, err := s.Close(ctx, d.Period.ID, "Đóng kỳ ngoại lệ đủ lý do", "adhoc-own211")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Forms) != 1 {
		t.Fatal("missing owned row")
	}
	row := out.Forms[0]
	if row.ReviewNote != request.Instruction || row.RequestState != "fulfilled" || row.RequestDueAt == nil || !row.RequestDueAt.Equal(requestDue) || row.Late || row.NativeLate == nil || !*row.NativeLate || row.View.Attempt != 1 || row.View.State != pd.FormLocked {
		t.Fatalf("owned adhoc semantics note=%q state=%s late=%v nativeLate=%v attempt=%d", row.ReviewNote, row.RequestState, row.Late, row.NativeLate, row.View.Attempt)
	}
	again, err := s.Close(ctx, d.Period.ID, "Đóng kỳ ngoại lệ đủ lý do", "adhoc-own211")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("stored adhoc outcome changed")
	}
}
func seedExport211(t *testing.T, f *deadlineFx209, ctx context.Context, id uuid.UUID) uuid.UUID {
	t.Helper()
	content := rd.Draft{}
	hash := strings.Repeat("a", 64)
	body := []byte("immutable metadata fixture211")
	draft := rs.Draft{ID: uuid.New(), PeriodID: id, Action: "save", Content: content, InputHash: hash, ActorRef: f.actor, At: f.clk.Now()}
	export := rs.Export{ID: uuid.New(), PeriodID: id, DraftID: draft.ID, Content: content, InputHash: hash, FileName: "frozen211.xlsx", Body: body, BodyHash: fmt.Sprintf("%x", sha256.Sum256(body)), ActorRef: f.actor, At: f.clk.Now()}
	if err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		if err := rs.InsertDraft(ctx, tx, f.tid, &draft, "draft211"); err != nil {
			return err
		}
		if err := rs.InsertExport(ctx, tx, f.tid, &export, "export211"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return export.ID
}
func assertFinalCount211(t *testing.T, f *deadlineFx209, ctx context.Context, id uuid.UUID, want int) {
	t.Helper()
	var count int
	err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_report_final WHERE tenant_id=$1 AND period_id=$2`, f.tid, id).Scan(&count)
	})
	if err != nil || count != want {
		t.Fatalf("report Final count=%d want=%d error=%v", count, want, err)
	}
}
func seedFinal211(t *testing.T, f *deadlineFx209, ctx context.Context, id uuid.UUID) {
	t.Helper()
	exportID := seedExport211(t, f, ctx, id)
	assertFinalCount211(t, f, ctx, id, 0)
	err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		reserved, e := rs.CloseReserved211(ctx, tx, f.tid, id)
		if e != nil {
			return e
		}
		if reserved {
			return errors.New("positive first Final control unexpectedly has a Close reservation")
		}
		return rs.InsertFinal(ctx, tx, f.tid, rs.Final{ID: uuid.New(), PeriodID: id, ExportID: exportID, ActorRef: f.actor, At: f.clk.Now()}, "final211")
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFinalCount211(t, f, ctx, id, 1)
}
func TestClose211NormalCloseWithExistingFinal(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	s, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	sender := submitter211(t, f)
	if _, err := f.fm.Entry211().Submit(sender, "kn_khtc", uuid.MustParse(p.FormsPeriodID), "trong_trot", map[string]string{"ket_qua": "Dữ liệu đủ211"}, 0, "complete211"); err != nil {
		t.Fatal(err)
	}
	seedFinal211(t, f, ctx, uuid.MustParse(p.ID))
	out, err := s.Close(ctx, uuid.MustParse(p.ID), "", "close-final211")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != pd.PeriodClosed || out.Events[len(out.Events)-1].Forced {
		t.Fatal("Final incorrectly blocked normal close")
	}
}

// Module-role INSERT is fenced even without the application advisory hint.
func TestClose211ReservedBlocksNewFinalAndSnapshotSQL(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	exportID := seedExport211(t, f, ctx, id)
	assertFinalCount211(t, f, ctx, id, 0)
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	probe.before = func(_ context.Context, _ fp.PeriodOperationRequest) {
		for _, kind := range []string{"final", "snapshot"} {
			err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
				if kind == "final" {
					return rs.InsertFinal(ctx, tx, f.tid, rs.Final{ID: uuid.New(), PeriodID: id, ExportID: exportID, ActorRef: f.actor, At: f.clk.Now()}, "foreign-final211")
				}
				_, e := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_snapshot (id,tenant_id,stream_key,period_id,period_from,period_to,window_from,target_calendar_id,target_calendar_version,target_rule,crosswalk_version,closed,figures_hash,status_hash,row_count,value_count,change_count,event_time,actor_ref,idem_key,rule_version) VALUES($1,$2,'khtc_snapshot:'||$3::uuid::text,$3,'2026-10-01','2026-10-07','2026-01-01','test211',1,'test211','test211',true,$4,$4,0,0,0,$5,$6,'foreign-snapshot211','test211')`, uuid.New(), f.tid, id, strings.Repeat("a", 64), f.clk.Now(), f.actor)
				return e
			})
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "khtc_report_closing211" {
				t.Fatalf("%s pending Close fence: %v", kind, err)
			}
		}
	}
	svc.Operations = probe
	if _, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "reserved-sql211"); err != nil {
		t.Fatal(err)
	}
	assertFinalCount211(t, f, ctx, id, 0)
	if probe.calls != 1 {
		t.Fatal("native dispatch count changed")
	}
}
func TestClose211LocalCommitUnknownReconcilesOwnedOutcome(t *testing.T) {
	for _, mode := range []string{"commitLostACK", "rollbackUnknown"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			id := uuid.MustParse(p.ID)
			f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
			svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
			probe := &receiptProbe211{PeriodOperations: svc.Operations}
			svc.Operations = probe
			svc.CloseGuard = &reportapp.Service{DB: localACK209{Beginner: f.roleDB, mode: mode}, Clock: f.clk}
			if _, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "local-ack211"); err == nil {
				t.Fatal("unknown local ACK claimed success")
			}
			want := "hydrated"
			if mode == "commitLostACK" {
				want = "completed"
			}
			if phaseClose211(t, f) != want {
				t.Fatalf("wrong durable phase want %s", want)
			}
			svc.CloseGuard = &reportapp.Service{DB: f.roleDB, Clock: f.clk}
			out, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "local-ack211")
			if err != nil || out.State != pd.PeriodClosed || phaseClose211(t, f) != "completed" || probe.calls != 1 {
				t.Fatalf("own local reconciliation: state=%s calls=%d err=%v", out.State, probe.calls, err)
			}
			var ownClosed int
			if err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_period_event WHERE tenant_id=$1 AND period_id=$2 AND action='closed'`, f.tid, id).Scan(&ownClosed)
			}); err != nil || ownClosed != 1 {
				t.Fatalf("own event count=%d err=%v", ownClosed, err)
			}
		})
	}
}

func TestClose211NoMFADeniedBeforeIntentOrNativeIO(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, _ := closeConsumer211(t, f, f.fm.ConditionalControl())
	principal := fixture.Bind(t, authz.Principal{ActorRef: f.actor, TenantID: f.tid, MFA: false, KN: &authz.KNContext{ID: uuid.NewSHA1(f.tid, []byte("close211")), Role: authz.KNTinh, Department: khtcDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}})
	ctx := authz.WithPrincipal(context.Background(), principal)
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "no-mfa211")
	var problemError *problem.Problem
	if !errors.As(err, &problemError) || problemError.Status != http.StatusForbidden || problemError.Code != "mfa_required" {
		t.Fatalf("NoMFA must deny403: %v", err)
	}
	var intents int
	if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_close_intent211 WHERE tenant_id=$1`, f.tid).Scan(&intents)
	}); err != nil || intents != 0 || probe.calls != 0 || probe.reads != 0 {
		t.Fatalf("NoMFA wrote intent=%d calls=%d reads=%d error=%v", intents, probe.calls, probe.reads, err)
	}
}
func TestClose211OfflineHeldDoesNotInventAcceptedSubmission(t *testing.T) {
	for _, stage := range []string{"beforeHydration", "duringHydration"} {
		t.Run(stage, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			id := uuid.MustParse(p.ID)
			nativeID := uuid.MustParse(p.FormsPeriodID)
			f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
			conditional := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
			svc, ctx := closeConsumer211(t, f, conditional)
			probe := &receiptProbe211{PeriodOperations: svc.Operations}
			svc.Operations = probe
			held := func() {
				before, e := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", nativeID)
				if e != nil {
					t.Fatal(e)
				}
				f.clk.Advance(time.Second) // held must be AFTER Close, not an equal fixed-clock timestamp.
				heldSubmission211(t, f, submitter211(t, f), nativeID, "held-"+stage)
				after, e := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", nativeID)
				if e != nil {
					t.Fatal(e)
				}
				if before.Revision != after.Revision || len(after.Progress.Units) != 1 || after.Progress.Units[0].Status != "awaiting_reopen" || before.Progress.Units[0].Status == after.Progress.Units[0].Status {
					t.Fatal("held fixture did not change only raw progress status after owned Close")
				}
			}
			if stage == "beforeHydration" {
				probe.after = func(r fp.PeriodOperationReceipt, e error) (fp.PeriodOperationReceipt, error) {
					if e == nil {
						held()
					}
					return r, e
				}
			} else {
				var once sync.Once
				conditional.snapshotHook = func(_ context.Context, snap fp.PeriodControlSnapshot) {
					if snap.Progress.Status == "closed" {
						once.Do(held)
					}
				}
			}
			out, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "offline-"+stage)
			if stage == "duringHydration" {
				assertConflict209(t, err)
				if phaseClose211(t, f) != "unresolved" {
					t.Fatal("inconsistent hydration must remain unresolved")
				}
				out, err = svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "offline-"+stage)
			}
			if err != nil {
				t.Fatal(err)
			}
			if out.Counts.FormsSent != 0 || len(out.Forms) != 1 || out.Forms[0].View.Attempt != 0 || out.Forms[0].View.State != pd.FormNotSent || probe.calls != 1 {
				t.Fatalf("offline held changed accepted close projection: sent=%d calls=%d", out.Counts.FormsSent, probe.calls)
			}
		})
	}
}

func TestClose211SingleDispatchWithOneConnectionAndConcurrentReplay(t *testing.T) {
	n := fixture.NewSized(t, 1)
	f := &deadlineFx209{t: t, fm: n, roleDB: n.DB, clk: n.Clock, tid: uuid.New(), node: uuid.New(), actor: uuid.New()}
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	retryEntered := make(chan struct{})
	var releaseOnce, enterOnce, retryEnterOnce sync.Once
	var original fp.PeriodOperationRequest
	var originalMu sync.Mutex
	beforeCounts := nativeCountsFix211(t, f)
	beforeOutbox := f.outboxCount209(t)
	releaseNative := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseNative()
	probe := &receiptProbe211{PeriodOperations: svc.Operations, before: func(_ context.Context, in fp.PeriodOperationRequest) {
		originalMu.Lock()
		if original.OperationID == uuid.Nil {
			original = in
		} else {
			retryEnterOnce.Do(func() { close(retryEntered) })
		}
		if in != original {
			t.Error("concurrent recovery replaced original command/Expected")
		}
		originalMu.Unlock()
		enterOnce.Do(func() { close(entered) })
		<-release
	}}
	svc.Operations = probe
	done := make(chan error, 1)
	go func() {
		_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "concurrent211")
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("pool1 blocked before native dispatch")
	}
	retry := make(chan error, 1)
	go func() {
		_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "concurrent211")
		retry <- err
	}()
	select {
	case <-retryEntered:
	case <-ctx.Done():
		t.Fatal("competing exact retry did not reach supplier before release")
	}
	releaseNative()
	successes := 0
	for _, result := range []<-chan error{done, retry} {
		if err := <-result; err != nil {
			assertConflict209(t, err)
		} else {
			successes++
		}
	}
	if successes == 0 {
		t.Fatal("neither concurrent owner completed")
	}
	out, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "concurrent211")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "concurrent211")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("concurrent replay did not preserve exact stored outcome")
	}
	afterCounts := nativeCountsFix211(t, f)
	if phaseClose211(t, f) != "completed" || afterCounts != [3]int{beforeCounts[0] + 1, beforeCounts[1] + 1, beforeCounts[2] + 1} || f.outboxCount209(t) != beforeOutbox+1 {
		t.Fatal("concurrent exact-command retry repeated native event, snapshot, anchor or outbox")
	}
}

// Native effect may exist even when its ACK and receipt read are both unavailable.
// Keep its reservation and original operation, rather than guessing rollback.
type unknownRead211 struct{ fp.PeriodOperations }

func (p unknownRead211) ReadPeriodOperation(context.Context, fp.PeriodOperationRequest) (fp.PeriodOperationReceipt, error) {
	return fp.PeriodOperationReceipt{}, nil
}
func TestClose211UnknownReceiptKeepsFenceAndNeverExecutesAgain(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	control := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
	svc, ctx := closeConsumer211(t, f, control)
	probe := &receiptProbe211{PeriodOperations: svc.Operations, after: func(r fp.PeriodOperationReceipt, e error) (fp.PeriodOperationReceipt, error) {
		if e == nil {
			return fp.PeriodOperationReceipt{}, errors.New("synthetic lost Close ACK211")
		}
		return r, e
	}}
	svc.Operations = probe
	_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "missing-receipt211")
	assertConflict209(t, err)
	svc.Operations = unknownRead211{probe}
	for i := 0; i < 2; i++ {
		_, err = svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "missing-receipt211")
		assertConflict209(t, err)
	}
	_, err = svc.ChangeDeadline(ctx, id, pa.DeadlineInput{DueAt: f.clk.Now().Add(time.Hour), ExportDueAt: f.clk.Now().Add(2 * time.Hour)}, "deadline-during-unknown211")
	assertConflict209(t, err)
	var reserved, claimed bool
	err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		var e error
		reserved, e = rs.CloseReserved211(ctx, tx, f.tid, id)
		if e != nil {
			return e
		}
		claimed, e = rs.ClaimCancel(ctx, tx, f.tid, id, uuid.New(), time.Minute)
		return e
	})
	if err != nil || !reserved || claimed || probe.calls != 1 || control.count() != 0 || phaseClose211(t, f) != "unresolved" {
		t.Fatalf("unknown receipt lost fence reserved=%v cancelClaimed=%v calls=%d deadlineCalls=%d err=%v", reserved, claimed, probe.calls, control.count(), err)
	}
}
func TestClose211SameKeyNoMFAAndChangedActorCannotReplay(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	if _, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "protected-replay211"); err != nil {
		t.Fatal(err)
	}
	var before int
	count := func(ctx context.Context) int {
		var n int
		if err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_close_intent211 WHERE tenant_id=$1`, f.tid).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before = count(ctx)
	for _, mode := range []string{"noMFA", "changedActor"} {
		actor, mfa := f.actor, false
		if mode == "changedActor" {
			actor, mfa = uuid.New(), true
		}
		principal := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: f.tid, MFA: mfa, KN: &authz.KNContext{ID: uuid.NewSHA1(f.tid, []byte("close211")), Role: authz.KNTinh, Department: khtcDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}})
		denied := authz.WithPrincipal(context.Background(), principal)
		_, err := svc.Close(denied, id, "Đóng kỳ ngoại lệ đủ lý do", "protected-replay211")
		var pe *problem.Problem
		wantStatus := http.StatusForbidden
		if mode == "changedActor" {
			wantStatus = http.StatusConflict
		}
		if !errors.As(err, &pe) || pe.Status != wantStatus {
			t.Fatalf("%s replay must deny%d: %v", mode, wantStatus, err)
		}
		if count(ctx) != before || probe.calls != 1 || probe.reads != 0 {
			t.Fatal("revoked replay reached supplier or appended intent")
		}
	}
}

func TestClose211NativeSuccessorABANeverBecomesOwnOutcome(t *testing.T) {
	for _, stage := range []string{"beforeHydration", "duringHydration"} {
		t.Run(stage, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			id := uuid.MustParse(p.ID)
			nativeID := uuid.MustParse(p.FormsPeriodID)
			f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
			native := f.fm.ConditionalControl()
			conditional := &casProbe209{ConditionalPeriodControl: native}
			svc, ctx := closeConsumer211(t, f, conditional)
			probe := &receiptProbe211{PeriodOperations: svc.Operations}
			svc.Operations = probe
			var successor fp.PeriodRevision
			replace := func() {
				original, e := native.ControlSnapshot(ctx, "kn_khtc", nativeID)
				if e != nil {
					t.Fatal(e)
				}
				reopened, e := native.ReopenPeriodCAS(ctx, "kn_khtc", nativeID, original.Revision, "Mở lại theo quyết định kế tiếp211", "successor-reopen211")
				if e != nil {
					t.Fatal(e)
				}
				successor, e = native.ClosePeriodCAS(ctx, "kn_khtc", nativeID, reopened, "successor-close211")
				if e != nil {
					t.Fatal(e)
				}
				if successor == original.Revision {
					t.Fatal("native ABA did not advance heads")
				}
			}
			if stage == "beforeHydration" {
				probe.after = func(r fp.PeriodOperationReceipt, e error) (fp.PeriodOperationReceipt, error) {
					if e == nil {
						replace()
					}
					return r, e
				}
			} else {
				var once sync.Once
				conditional.snapshotHook = func(_ context.Context, snap fp.PeriodControlSnapshot) {
					if snap.Progress.Status == "closed" {
						once.Do(replace)
					}
				}
			}
			_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "successor-"+stage)
			assertConflict209(t, err)
			_, err = svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "successor-"+stage)
			assertConflict209(t, err)
			current, err := native.ControlSnapshot(ctx, "kn_khtc", nativeID)
			if err != nil || current.Revision != successor || current.Progress.Status != "closed" || probe.calls != 1 || phaseClose211(t, f) != "unresolved" {
				t.Fatalf("own outcome consumed successor: calls=%d err=%v", probe.calls, err)
			}
			var count int
			if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_period_event WHERE tenant_id=$1 AND period_id=$2 AND action='closed'`, f.tid, id).Scan(&count)
			}); err != nil || count != 0 {
				t.Fatal("successor generated a false local owned Close event")
			}
		})
	}
}
func TestClose211ClosedSubmitAndReviewCannotChangeAcceptedHead(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	nativeID := uuid.MustParse(p.FormsPeriodID)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	sender := submitter211(t, f)
	sub, err := f.fm.Entry211().Submit(sender, "kn_khtc", nativeID, "trong_trot", map[string]string{"ket_qua": "Accepted head211"}, 0, "accepted-head211")
	if err != nil {
		t.Fatal(err)
	}
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	assertClosed := func(err error) {
		var pe *problem.Problem
		if !errors.As(err, &pe) || pe.Code != "forms.period_closed" {
			t.Fatalf("closed native write was not rejected: %v", err)
		}
	}
	probe.after = func(r fp.PeriodOperationReceipt, e error) (fp.PeriodOperationReceipt, error) {
		if e == nil {
			_, submitErr := f.fm.Entry211().Submit(sender, "kn_khtc", nativeID, "trong_trot", map[string]string{"ket_qua": "Forbidden successor211"}, 1, "closed-submit211")
			assertClosed(submitErr)
			_, reviewErr := f.fm.Entry211().Review(ctx, "kn_khtc", sub.ID, "accepted", "Không được thay head sau Close211", "closed-review211")
			assertClosed(reviewErr)
		}
		return r, e
	}
	out, err := svc.Close(ctx, id, "", "accepted-close211")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Forms) != 1 || out.Forms[0].View.Attempt != 1 || out.Forms[0].ReviewedAt != nil || out.Forms[0].ReviewNote != "" {
		t.Fatal("closed write changed accepted head projection")
	}
}

type closeGuardHook211 struct {
	pa.CloseGuard211
	before func()
}

func (g closeGuardHook211) BeginClose211(ctx context.Context, id, op, event uuid.UUID) error {
	g.before()
	return g.CloseGuard211.BeginClose211(ctx, id, op, event)
}
func TestClose211CancelWinsBeforeReservationHasNoNativeEffect(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	svc.CloseGuard = closeGuardHook211{CloseGuard211: svc.CloseGuard, before: func() {
		err := tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
			if e := rs.Lock(ctx, tx, f.tid, id); e != nil {
				return e
			}
			won, e := rs.ClaimCancel(ctx, tx, f.tid, id, uuid.New(), time.Minute)
			if e == nil && !won {
				return errors.New("Cancel fixture did not win reservation gap")
			}
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}}
	_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "cancel-wins211")
	assertConflict209(t, err)
	if phaseClose211(t, f) != "rejected" || probe.calls != 0 || probe.reads != 0 {
		t.Fatal("Cancel won but Close dispatched a native command or retained uncertain phase")
	}
	// A valid original command is not authority after its owner has rejected it.
	var data struct {
		Native fp.PeriodOperationRequest `json:"native"`
	}
	var payload []byte
	err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM pf_extension.khtc_close_intent211 WHERE tenant_id=$1 AND period_id=$2 ORDER BY seq DESC LIMIT 1`, f.tid, id).Scan(&payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payload, &data); err != nil {
		t.Fatal(err)
	}
	before, err := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
	if err != nil || before.Revision != data.Native.Expected {
		t.Fatal("rejected command fixture is not otherwise valid at its original native revision")
	}
	beforeOutbox := f.outboxCount209(t)
	receipt, err := f.fm.PeriodOperations().ExecutePeriodOperation(ctx, data.Native)
	var denied *problem.Problem
	if !errors.As(err, &denied) || denied.Status != http.StatusForbidden {
		t.Fatalf("rejected owner intent must deny public Execute403: outcome=%s err=%v", receipt.Outcome, err)
	}
	after, err := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
	if err != nil || after.Revision != before.Revision || after.Progress.Status != "open" || f.outboxCount209(t) != beforeOutbox || phaseClose211(t, f) != "rejected" {
		t.Fatal("rejected owner policy allowed a native effect")
	}
	native, err := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
	if err != nil || native.Progress.Status != "open" {
		t.Fatal("Cancel gap closed native period")
	}
}
func TestClose211AdminAdhocOwnCompletedReplay(t *testing.T) {
	f := newDeadlineFx209(t)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	d := openAdhoc211(t, f, svc, ctx)
	f.clk.Advance(d.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	principal := fixture.Bind(t, authz.Principal{ActorRef: uuid.New(), TenantID: f.tid, MFA: true, KN: &authz.KNContext{ID: uuid.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}}})
	admin := authz.WithPrincipal(context.Background(), principal)
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	out, err := svc.Close(admin, d.Period.ID, "Đóng kỳ ngoại lệ đủ lý do", "admin-adhoc211")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Close(admin, d.Period.ID, "Đóng kỳ ngoại lệ đủ lý do", "admin-adhoc211")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) || probe.calls != 1 || probe.reads != 1 || out.State != pd.PeriodClosed {
		t.Fatal("own Admin receipt replay failed after terminal event")
	}
}

func TestClose211PendingDeadlineWinsResourceFence(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	control := &casProbe209{ConditionalPeriodControl: f.fm.ConditionalControl()}
	svc, ctx := closeConsumer211(t, f, control)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseNative := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseNative()
	control.changeHook = func(context.Context, int, fp.PeriodRevision) { close(entered); <-release }
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	done := make(chan error, 1)
	go func() {
		_, err := svc.ChangeDeadline(ctx, id, pa.DeadlineInput{DueAt: p.DueAt.Add(time.Hour), Reason: "Đổi hạn đã giữ tài nguyên211"}, "deadline-wins211")
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("Deadline did not acquire prepared resource fence")
	}
	_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "close-after-pending-deadline211")
	assertConflict209(t, err)
	releaseNative()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if probe.calls != 0 || probe.reads != 0 || control.count() != 1 {
		t.Fatal("pending Deadline allowed Close native I/O")
	}
	var intents int
	if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.khtc_close_intent211 WHERE tenant_id=$1`, f.tid).Scan(&intents)
	}); err != nil || intents != 0 {
		t.Fatal("Close recorded an intent across an owned pending Deadline")
	}
}
