package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func adhocLeaseContext(t *testing.T, ctx context.Context, session uuid.UUID, expiry time.Time, change func(*ext.SessionContext, *authz.Principal)) context.Context {
	t.Helper()
	p, k, e := ext.RequireSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	k.ID = session
	k.ExpiresAt = expiry
	if change != nil {
		change(&k, &p)
	}
	selected, e := ext.NewSessionContext(k)
	if e != nil {
		t.Fatal(e)
	}
	p.SelectedSession = &selected
	return authz.WithPrincipal(tenant.WithID(context.Background(), p.TenantID), p)
}
func recoveryHTTP(t *testing.T, f *reportFx, ctx context.Context, method, path string, body any) (int, []byte) {
	t.Helper()
	p, k, e := ext.RequireSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	encoded, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(encoded))
	req.Header.Set("X-Test-Actor", p.ActorRef.String())
	if !p.MFA {
		req.Header.Set("X-Test-No-MFA", "true")
	}
	if method == "POST" {
		req.Header.Set("Idempotency-Key", ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}
func TestW1168RecoveryReloadExpiryAdoptionAndScope(t *testing.T) {
	f := newReportFx(t)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	broken := &lostAdhocPublisher{Publisher: f.nativeForms}
	broken.after.Store(true)
	s := periodapp.Service{DB: pool, Clock: f.clk, Forms: broken, Control: f.nativeControl, MaxSubmissions: 3}
	base := adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, uuid.Nil)
	old := adhocLeaseContext(t, base, ids.New(), f.clk.Now().Add(time.Minute), nil)
	if _, e := s.Open(old, adhocOpenInput(), "lost-browser-key"); e == nil {
		t.Fatal("loss not injected")
	}
	var list struct {
		Items []periodapp.AdhocOpenRecovery `json:"items"`
	}
	code, b := recoveryHTTP(t, f, old, "GET", "/v1/extension/khtc/adhoc-open-intents", nil)
	f.ok(code, b, &list)
	if len(list.Items) != 1 {
		t.Fatalf("reload lost intent: %s", b)
	}
	if strings.Contains(string(b), "native_idem") || strings.Contains(string(b), "lost-browser-key") || strings.Contains(string(b), "batch_hash") {
		t.Fatal("private receipt exposed")
	}
	id := list.Items[0].ID
	fresh := adhocLeaseContext(t, base, ids.New(), f.clk.Now().Add(time.Hour), nil)
	path := "/v1/extension/khtc/adhoc-open-intents/" + id.String() + "/reconcile"
	code, b = recoveryHTTP(t, f, fresh, "POST", path, periodapp.AdhocOpenReconcileInput{Reason: "Tiếp tục thao tác sau hết phiên"})
	if code != 409 {
		t.Fatalf("unexpired adoption %d %s", code, b)
	}
	f.clk.Advance(2 * time.Minute)
	for _, mode := range []string{"actor", "scope", "role", "readonly", "mfa"} {
		t.Run(mode, func(t *testing.T) {
			denied := adhocLeaseContext(t, fresh, ids.New(), f.clk.Now().Add(time.Hour), func(k *ext.SessionContext, p *authz.Principal) {
				switch mode {
				case "actor":
					p.ActorRef = ids.New()
				case "scope":
					k.Scope = authz.Scope{Units: []string{"X2"}}
				case "role":
					k.Role = ext.SessionRoleProvince
					k.Department = khtcDept
				case "readonly":
					k.ViewOnly = true
				case "mfa":
					p.MFA = false
				}
			})
			_, e := s.ReconcileAdhocOpen(denied, id, periodapp.AdhocOpenReconcileInput{Reason: "Tiếp nhận từ phiên đã hết hạn"})
			if e == nil {
				t.Fatal("adopted foreign or weaker context")
			}
		})
	}
	code, b = recoveryHTTP(t, f, fresh, "POST", path, periodapp.AdhocOpenReconcileInput{Reason: "Tiếp nhận sau mất kết nối và hết phiên"})
	var result khtcPeriod
	f.ok(code, b, &result)
	if result.FormsPeriodID != broken.receipts[0].String() {
		t.Fatal("native identity changed")
	}
	_, e := s.Open(old, adhocOpenInput(), "lost-browser-key")
	if e == nil {
		t.Fatal("old session resumed after adoption")
	}
	// Old marker fails even though its native batch and immutable identity still exist.
	var intent *ps.AdhocOpenIntent
	if e = tenant.WithTenantTx(fresh, pool, f.tid, func(tx pgx.Tx) error { var e error; intent, e = ps.AdhocOpenByID(fresh, tx, id); return e }); e != nil {
		t.Fatal(e)
	}
	p, k, _ := ext.RequireSession(old)
	op := forms.Operation{Action: forms.OperationPublishBatch, Profile: "kn_khtc", PayloadHash: intent.BatchHash, IdemKey: intent.NativeKey}
	linked := ext.WithKHTCOperation(old, periodapp.OperationAuthority{DB: pool}, ext.KHTCOperationProof{IntentID: id, SessionID: k.ID, Expected: op})
	grant, e := ext.AuthorizeKHTCOperation(linked, p, op)
	if e == nil && grant.Allowed {
		t.Fatal("old marker survived lease change")
	}
	code, b = recoveryHTTP(t, f, fresh, "POST", path, periodapp.AdhocOpenReconcileInput{ExpectedGeneration: 1})
	var replay khtcPeriod
	f.ok(code, b, &replay)
	if replay.ID != result.ID {
		t.Fatal("reconcile created second KN period")
	}
	code, b = recoveryHTTP(t, f, fresh, "GET", "/v1/extension/khtc/adhoc-open-intents", nil)
	f.ok(code, b, &list)
	if len(list.Items) != 0 {
		t.Fatal("complete intent remains incomplete")
	}
}

type blockedAdhocPublisher struct {
	forms.Publisher
	block            atomic.Bool
	entered, release chan struct{}
}

func (p *blockedAdhocPublisher) PublishBatchOnce(ctx context.Context, in forms.BatchInput, key string) ([]forms.PublishedPeriod, error) {
	out, e := p.Publisher.(forms.DurablePublisher).PublishBatchOnce(ctx, in, key)
	if e == nil && p.block.Swap(false) {
		close(p.entered)
		<-p.release
	}
	return out, e
}
func TestW1168RecoveryInflightOldSessionAndConcurrentAdoption(t *testing.T) {
	f := newReportFx(t)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	publisher := &blockedAdhocPublisher{Publisher: f.nativeForms, entered: make(chan struct{}), release: make(chan struct{})}
	publisher.block.Store(true)
	s := periodapp.Service{DB: pool, Clock: f.clk, Forms: publisher, Control: f.nativeControl, MaxSubmissions: 3}
	base := adhocOwnerContext(t, f, authz.KNAdmin, "", f.khtcUser, true, false, uuid.Nil)
	old := adhocLeaseContext(t, base, ids.New(), f.clk.Now().Add(time.Minute), nil)
	oldResult := make(chan error, 1)
	go func() { _, e := s.Open(old, adhocOpenInput(), "inflight-old-lease"); oldResult <- e }()
	select {
	case <-publisher.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("native publish did not finish")
	}
	defer close(publisher.release)
	f.clk.Advance(2 * time.Minute)
	contexts := []context.Context{adhocLeaseContext(t, base, ids.New(), f.clk.Now().Add(time.Hour), nil), adhocLeaseContext(t, base, ids.New(), f.clk.Now().Add(time.Hour), nil)}
	xs, e := s.AdhocOpenRecoveries(contexts[0])
	if e != nil || len(xs) != 1 {
		t.Fatal("missing open", e)
	}
	id := xs[0].ID
	var wg sync.WaitGroup
	success := make(chan periodapp.Detail, 2)
	failed := make(chan error, 2)
	for _, ctx := range contexts {
		wg.Add(1)
		go func(ctx context.Context) {
			defer wg.Done()
			d, e := s.ReconcileAdhocOpen(ctx, id, periodapp.AdhocOpenReconcileInput{Reason: "Tiếp nhận an toàn sau phiên hết hạn"})
			if e != nil {
				failed <- e
			} else {
				success <- d
			}
		}(ctx)
	}
	wg.Wait()
	if len(success) != 1 || len(failed) != 1 {
		t.Fatalf("adoption CAS success=%d fail=%d", len(success), len(failed))
	}
	winner := <-success
	assertAdhocStatus(t, <-failed, 409)
	publisher.release <- struct{}{}
	select {
	case e := <-oldResult:
		assertAdhocStatus(t, e, 409)
	case <-time.After(20 * time.Second):
		t.Fatal("old writer held cross-module TX")
	}
	if winner.Period.ID == uuid.Nil || winner.Period.FormsPeriodRef == uuid.Nil {
		t.Fatal("missing stable identity")
	}
}
func TestW1168SupplementEffectiveDeadlinePreservesNativeFlag(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Hạn bổ sung riêng")
	f.clk.Advance(7 * 24 * time.Hour)
	body := adhocRequestBody()
	body["due_at"] = f.clk.Now().Add(24 * time.Hour).Format(time.RFC3339)
	var request adhocTestRequest
	c, b := f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "late-period-request", body)
	f.ok(c, b, &request)
	c, b = f.send(p.ID, dTT, "submit", 0, answerSheet(request.ID, "3"))
	f.ok(c, b, nil)
	var sheet struct {
		Period struct {
			DueAt time.Time `json:"due_at"`
		} `json:"period"`
		Overdue     bool `json:"overdue"`
		Submissions []struct {
			Late       bool  `json:"late"`
			NativeLate *bool `json:"native_late"`
		} `json:"submissions"`
	}
	c, b = f.do("GET", formPath(p.ID, dTT), authz.KNTinh, dTT, ids.New(), "", nil)
	f.ok(c, b, &sheet)
	if sheet.Overdue || len(sheet.Submissions) != 1 || sheet.Submissions[0].Late || sheet.Submissions[0].NativeLate == nil || !*sheet.Submissions[0].NativeLate {
		t.Fatalf("deadline projection: %s", b)
	}
	var inbox struct {
		Rows []struct {
			PeriodID string    `json:"period_id"`
			DueAt    time.Time `json:"due_at"`
		} `json:"items"`
	}
	c, b = f.do("GET", "/v1/extension/khtc/department-forms", authz.KNTinh, dTT, ids.New(), "", nil)
	f.ok(c, b, &inbox)
	if !strings.Contains(string(b), sheet.Period.DueAt.Format(time.RFC3339)) {
		t.Fatalf("inbox deadline differs: %s", b)
	}
}
