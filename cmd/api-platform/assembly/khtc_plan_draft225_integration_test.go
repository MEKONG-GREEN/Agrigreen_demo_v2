package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	khtcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	khtcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The identity projection is a trusted fixture, not an accounts lifecycle test.
// JWT/MFA, production NewRouter and wireForms, real plan provider and native
// publisher/control are exercised. The separate draft-cache test uses Build/List.
type planSession225 struct {
	t        *testing.T
	contexts map[uuid.UUID]authz.KNContext
}

func (*planSession225) RegisterApp(*gin.RouterGroup)   {}
func (*planSession225) RegisterAdmin(*gin.RouterGroup) {}
func (s *planSession225) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		k, ok := s.contexts[p.ActorRef]
		if !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		p.KN = &k
		p = fixture.Bind(s.t, p)
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type planPublisher225 struct {
	formsport.Publisher
	calls        atomic.Int64
	durableCalls atomic.Int64
}

func (p *planPublisher225) PublishBatch(ctx context.Context, in formsport.BatchInput, key string) ([]formsport.PublishedPeriod, error) {
	p.calls.Add(1)
	return p.Publisher.PublishBatch(ctx, in, key)
}

var _ formsport.DurablePublisher = (*planPublisher225)(nil)

func (p *planPublisher225) PublishBatchOnce(ctx context.Context, in formsport.BatchInput, key string) ([]formsport.PublishedPeriod, error) {
	p.durableCalls.Add(1)
	durable, ok := p.Publisher.(formsport.DurablePublisher)
	if !ok {
		return nil, fmt.Errorf("native durable publisher unavailable")
	}
	return durable.PublishBatchOnce(ctx, in, key)
}

func TestInitialDraft225P804PlanlessOpenHasNoNativeEffect(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := wireForms(pool, clk, workflow.New(pool, clk), kn, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := &planPublisher225{Publisher: fm.Publisher()}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: probe, Control: fm.Control(), ConditionalControl: fm.ConditionalControl(), Entry: fm.Entry(), MaxSubmissions: KNKHTCProfile.MaxSubmissions})
	tid, org, admin, writer := ids.New(), ids.New(), ids.New(), ids.New()
	identities := &planSession225{t: t, contexts: map[uuid.UUID]authz.KNContext{
		admin:  {ID: ids.New(), Role: authz.KNAdmin, ExpiresAt: time.Now().Add(time.Hour), Scope: authz.Scope{All: true}},
		writer: {ID: ids.New(), Role: authz.KNTinh, ExpiresAt: time.Now().Add(time.Hour), Department: "ke_hoach_tai_chinh", OrgRef: org, Scope: authz.Scope{Units: []string{"X1"}}},
	}}
	issuer := authztest.New(t)
	router := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: pool, Modules: []httpx.Module{kn, identities, fm}})
	call := func(actor uuid.UUID, method, path string, body any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true}))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "plan225:"+ids.New().String())
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result.Code, result.Body.Bytes()
	}
	code, _ := call(admin, http.MethodPost, "/v1/extension/khtc/form-template/install", nil)
	if code != http.StatusOK {
		t.Fatalf("template setup status=%d", code)
	}
	code, _ = call(writer, http.MethodGet, "/v1/kn/khtc/plans/2026", nil)
	if code != http.StatusNotFound {
		t.Fatalf("precondition: unexpected published plan status=%d", code)
	}
	code, raw := call(writer, http.MethodPost, "/v1/extension/khtc/periods", map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{"trong_trot"}})
	var result struct {
		Code          string    `json:"code"`
		FormsPeriodID uuid.UUID `json:"forms_period_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal("open response was not JSON")
	}
	nativeApplied := false
	if result.FormsPeriodID != uuid.Nil {
		p := fixture.Bind(t, authz.Principal{ActorRef: writer, TenantID: tid, OrgRef: org, MFA: true, KN: func() *authz.KNContext { k := identities.contexts[writer]; return &k }()})
		snap, err := fm.ConditionalControl().ControlSnapshot(authz.WithPrincipal(context.Background(), p), KNKHTCProfile.Name, result.FormsPeriodID)
		if err != nil {
			t.Fatalf("native read control failed: %T", err)
		}
		nativeApplied = snap.Revision.PeriodID == result.FormsPeriodID && snap.Revision.EventSeq > 0
	}
	listCode, listRaw := call(writer, http.MethodGet, "/v1/extension/khtc/periods?year=2026", nil)
	if listCode != http.StatusOK {
		t.Fatalf("period metadata read status=%d", listCode)
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(listRaw, &list); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict || result.Code != "extension.khtc_plan_required" || probe.calls.Load() != 0 || nativeApplied || len(list.Items) != 0 {
		t.Fatalf("P804 missing published owner plan must prevent dispatch: http=%d code=%s native_publish_calls=%d native_applied=%t owned_periods=%d", code, result.Code, probe.calls.Load(), nativeApplied, len(list.Items))
	} // A real published empty plan is present; an empty department projection must
	// not be confused with missing plan. Explicit departments remain supported.
	code, _ = call(writer, http.MethodPost, "/v1/kn/khtc/plans/2026/versions", map[string]any{"base_version": 0, "entries": []any{}})
	if code != http.StatusCreated {
		t.Fatalf("published positive control setup status=%d", code)
	}
	code, raw = call(writer, http.MethodPost, "/v1/extension/khtc/periods", map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{"trong_trot"}})
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal("positive open response was not JSON")
	}
	if code != http.StatusCreated || probe.calls.Load() != 1 || result.FormsPeriodID == uuid.Nil {
		t.Fatalf("applied-plan positive control: http=%d native_publish_calls=%d native_id_present=%t", code, probe.calls.Load(), result.FormsPeriodID != uuid.Nil)
	}
	k := identities.contexts[writer]
	principal := fixture.Bind(t, authz.Principal{ActorRef: writer, TenantID: tid, OrgRef: org, MFA: true, KN: &k})
	if snap, err := fm.ConditionalControl().ControlSnapshot(authz.WithPrincipal(context.Background(), principal), KNKHTCProfile.Name, result.FormsPeriodID); err != nil || snap.Revision.EventSeq < 1 {
		t.Fatal("published-plan control did not create a native period")
	}
}

func TestInitialDraft225NativeCRUDGenerationAndPublishFence(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := wireForms(pool, clk, workflow.New(pool, clk), kn, nil)
	if err != nil {
		t.Fatal(err)
	}
	tid, org, a, b, other := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	contexts := map[uuid.UUID]authz.KNContext{}
	for _, actor := range []uuid.UUID{a, b} {
		contexts[actor] = authz.KNContext{ID: ids.New(), Role: authz.KNTinh, ExpiresAt: clk.Now().Add(time.Hour), Department: "ke_hoach_tai_chinh", OrgRef: org, Scope: authz.Scope{Units: []string{"X1"}}}
	}
	contexts[other] = authz.KNContext{ID: ids.New(), Role: authz.KNTinh, ExpiresAt: clk.Now().Add(time.Hour), Department: "trong_trot", OrgRef: org, Scope: authz.Scope{Units: []string{"X1"}}}
	issuer := authztest.New(t)
	router := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: pool, Modules: []httpx.Module{kn, &planSession225{t: t, contexts: contexts}, fm}})
	call := func(actor uuid.UUID, method, path, key string, body any) (int, []byte) {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true}))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	type receipt struct {
		DraftID  string `json:"draft_id"`
		EventID  string `json:"event_id"`
		Revision int64  `json:"revision"`
		Actor    string `json:"actor_ref"`
		Action   string `json:"action"`
	}
	decode := func(raw []byte) receipt {
		t.Helper()
		var r receipt
		if e := json.Unmarshal(raw, &r); e != nil {
			t.Fatal("receipt JSON invalid")
		}
		return r
	}
	path := "/v1/kn/khtc/plans/2030/draft"
	badStatus, _ := call(a, http.MethodPost, path, "draft225-nul-note", map[string]any{"expected_revision": 0, "entries": []any{}, "note": "bad\u0000note"})
	if badStatus != 422 {
		t.Fatalf("new draft NUL note must be validation failure without append: status=%d", badStatus)
	}
	var program struct {
		Ref uuid.UUID `json:"ref"`
	}
	{
		code, raw := call(a, http.MethodPost, "/v1/kn/khtc/programs", "draft225-real-program", map[string]any{"code": "P225", "name": "Tên CT gốc225", "kind": "program"})
		if code != 201 || json.Unmarshal(raw, &program) != nil || program.Ref == uuid.Nil {
			t.Fatalf("real program setup status=%d", code)
		}
	}
	// Raw numeric occurrences are checked before decimal construction or receipt hashing.
	for _, field := range []string{"planned_qty", "budget", "sort_order", "points_count"} {
		bad := []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), strings.Repeat("9", 80), "0.00001", "-1"}
		for i, value := range bad {
			t.Run(fmt.Sprintf("raw_%s_%d", field, i), func(t *testing.T) {
				row := fmt.Sprintf(`{"ref":%q,"tab":"programs","kind":"program","program_ref":%q,"name":"Hostile numeric225","%s":%s}`, ids.New().String(), program.Ref.String(), field, value)
				rawBody := json.RawMessage(`{"expected_revision":0,"entries":[` + row + `]}`)
				status, _ := call(a, http.MethodPost, path, "draft225:raw:"+ids.New().String(), rawBody)
				expected := http.StatusUnprocessableEntity
				if i < 4 || i == 4 && (field == "sort_order" || field == "points_count") {
					expected = http.StatusBadRequest
				}
				if status != expected {
					t.Fatalf("hostile %s case%d status=%d want=%d", field, i, status, expected)
				}
			})
		}
		t.Run("duplicate_casefold_"+field, func(t *testing.T) {
			row := fmt.Sprintf(`{"ref":%q,"tab":"programs","kind":"program","program_ref":%q,"name":"Duplicate numeric225","%s":1e100000,"%s":2}`, ids.New().String(), program.Ref.String(), field, strings.ToUpper(field))
			status, _ := call(a, http.MethodPost, path, "draft225:duplicate:"+ids.New().String(), json.RawMessage(`{"expected_revision":0,"entries":[`+row+`]}`))
			if status != http.StatusBadRequest {
				t.Fatalf("overwritten hostile numeric occurrence status=%d", status)
			}
		})
	}
	tooLarge := json.RawMessage(`{"expected_revision":0,"entries":[],"note":"` + strings.Repeat("x", (1<<20)+1) + `"}`)
	if status, _ := call(a, http.MethodPost, path, "draft225:oversize", tooLarge); status != 413 {
		t.Fatalf("pre-cache body limit status=%d", status)
	}
	{
		status, raw := call(a, http.MethodGet, path, "", nil)
		var current struct {
			Revision int64 `json:"revision"`
		}
		if status != 200 || json.Unmarshal(raw, &current) != nil || current.Revision != 0 {
			t.Fatalf("hostile requests appended a draft: status=%d revision=%d", status, current.Revision)
		}
	}
	entryRef := ids.New()
	entriesBody := []map[string]any{{"ref": entryRef.String(), "tab": "programs", "kind": "program", "program_ref": program.Ref.String(), "name": "Tên dòng đã chốt225", "code": "P225", "sort_order": 1, "tracking": "tracking", "communes": []string{}}}
	body := map[string]any{"expected_revision": 0, "entries": entriesBody, "note": "initial"}
	code, raw := call(a, http.MethodPost, path, "draft225-create", body)
	if code != 201 {
		t.Fatalf("create status=%d", code)
	}
	first := decode(raw)
	if first.DraftID == "" || first.Revision != 1 || first.Actor != a.String() {
		t.Fatal("create receipt binding incorrect")
	}
	// Rename the live master after the acknowledged draft. Direct public app
	// receipt replay intentionally bypasses the HTTP cache and must retain stored data.
	renameCode, _ := call(a, http.MethodPost, "/v1/kn/khtc/programs/"+program.Ref.String()+"/revise", "draft225-rename-master", map[string]any{"code": "P225", "name": "Tên danh mục mới225", "kind": "program", "active": true})
	if renameCode != 201 {
		t.Fatalf("real master rename status=%d", renameCode)
	}
	k := contexts[a]
	principal := fixture.Bind(t, authz.Principal{ActorRef: a, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true, KN: &k})
	svc := &khtcapp.Service{DB: db.ForModule(pool, db.Module("pf/extension")), Clock: clk}
	own, wasReplay, replayErr := svc.SaveInitialDraft225(authz.WithPrincipal(context.Background(), principal), khtcapp.InitialDraftInput225{Year: 2030, ExpectedRevision: 0, IdemKey: "draft225-create", Note: "initial", Entries: []khtcdomain.Entry{{Ref: entryRef, Tab: khtcdomain.TabPrograms, Kind: khtcdomain.EntryProgram, ProgramRef: &program.Ref, Name: "Tên dòng đã chốt225", Code: "P225", SortOrder: 1, Tracking: "tracking", Communes: []string{}}}})
	if replayErr != nil || !wasReplay || own.Event.ID.String() != first.EventID || len(own.Entries) != 1 || own.Entries[0].Name != "Tên dòng đã chốt225" || own.Entries[0].Code != "P225" || own.Entries[0].ProgramName != "" {
		t.Fatalf("immutable nonempty app receipt replay failed: replay=%t error=%T", wasReplay, replayErr)
	}
	code, replayed := call(a, http.MethodPost, path, "draft225-create", body)
	if code != 201 || !bytes.Equal(raw, replayed) {
		t.Fatalf("same-operation original ACK status=%d identical=%t", code, bytes.Equal(raw, replayed))
	}
	code, _ = call(a, http.MethodPost, "/v1/kn/khtc/plans/2030/versions", "draft225-publish", map[string]any{"base_version": 0, "entries": []any{}})
	if code != 409 {
		t.Fatalf("active draft publish fence status=%d", code)
	}
	code, _ = call(other, http.MethodGet, path, "", nil)
	if code != 403 {
		t.Fatalf("unpublished draft foreign department status=%d", code)
	}
	body = map[string]any{"draft_id": first.DraftID, "expected_revision": first.Revision, "entries": entriesBody, "note": "shared actor edit"}
	code, raw = call(b, http.MethodPost, path, "draft225-save", body)
	if code != 201 {
		t.Fatalf("shared actor save status=%d", code)
	}
	second := decode(raw)
	if second.Revision != 2 || second.DraftID != first.DraftID || second.Actor != b.String() {
		t.Fatal("shared generation receipt incorrect")
	}
	code, _ = call(a, http.MethodPost, path, "draft225-stale", body)
	if code != 409 {
		t.Fatalf("stale revision status=%d", code)
	}
	discard := map[string]any{"draft_id": first.DraftID, "expected_revision": second.Revision, "note": "discard exact generation"}
	code, discardRaw := call(a, http.MethodPost, path+"/discard", "draft225-discard", discard)
	if code != 201 {
		t.Fatalf("discard status=%d", code)
	}
	gone := decode(discardRaw)
	if gone.Revision != 3 || gone.Action != "discard" {
		t.Fatal("discard is not durable revision")
	}
	code, newRaw := call(b, http.MethodPost, path, "draft225-recreate", map[string]any{"expected_revision": 3, "entries": []any{}})
	if code != 201 {
		t.Fatalf("recreate status=%d", code)
	}
	next := decode(newRaw)
	if next.Revision != 4 || next.DraftID == first.DraftID {
		t.Fatal("recreated draft reused generation")
	}
	code, replayed = call(a, http.MethodPost, path+"/discard", "draft225-discard", discard)
	if code != 201 || !bytes.Equal(discardRaw, replayed) {
		t.Fatalf("old discard own receipt status=%d identical=%t", code, bytes.Equal(discardRaw, replayed))
	}
	code, _ = call(a, http.MethodPost, path+"/discard", "draft225-stale-discard", discard)
	if code != 409 {
		t.Fatalf("old generation fresh discard status=%d", code)
	}
	code, currentRaw := call(b, http.MethodGet, path, "", nil)
	if code != 200 {
		t.Fatalf("reload status=%d", code)
	}
	var state struct {
		Revision int64    `json:"revision"`
		Draft    *receipt `json:"draft"`
	}
	if e := json.Unmarshal(currentRaw, &state); e != nil {
		t.Fatal("state JSON invalid")
	}
	if state.Revision != 4 || state.Draft == nil || state.Draft.DraftID != next.DraftID {
		t.Fatal("late old discard changed successor")
	}
	code, historyRaw := call(a, http.MethodGet, path+"/history?limit=2", "", nil)
	if code != 200 {
		t.Fatalf("history status=%d", code)
	}
	var history struct {
		Items []receipt `json:"items"`
	}
	if e := json.Unmarshal(historyRaw, &history); e != nil {
		t.Fatal("history JSON invalid")
	}
	if len(history.Items) != 2 || history.Items[0].Revision != 4 || history.Items[1].Revision != 3 {
		t.Fatal("history not immutable sequence ordered")
	}
	code, _ = call(a, http.MethodGet, "/v1/kn/khtc/plans/2030", "", nil)
	if code != 404 {
		t.Fatalf("server draft leaked into published plan status=%d", code)
	}
}

// Bootstrap only the Admin assignment current-state fixture, as in the existing
// root session test. Both selected sessions below are produced by public Accounts
// routes; actual Build/List/Gated/ResolveContext/cache policy remain in the path.
func TestInitialDraft225BuildListPublicRenewalAndCurrentPolicy(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Now().UTC()
	clk := clock.NewFixed(now)
	mods, pools, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clk, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	tid, actor, assignment, org := ids.New(), ids.New(), ids.New(), ids.New()
	_, err = env.AdminPool(t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_accounts.role_assignment_current
 (tenant_id,assignment_id,user_ref,role,department_codes,commune_codes,valid_from,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
 VALUES($1,$2,$3,'admin',ARRAY[]::text[],ARRAY[]::text[],$4,true,$5,1,$3,$4,$3,$4)`, tid, assignment, actor, now.Add(-time.Hour), ids.New())
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	router := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Pools: pools, Modules: mods.List()})
	call := func(method, path, key, selected string, mfa bool, body any) (int, []byte) {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: mfa}))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if selected != "" {
			req.Header.Set(authz.ContextHeader, selected)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	selectCtx := func(key string) string {
		t.Helper()
		code, raw := call(http.MethodPost, "/v1/me/context", key, "", true, map[string]any{"assignment_id": assignment.String(), "department_code": nil})
		if code != 201 {
			t.Fatalf("public selection status=%d", code)
		}
		var x struct {
			ID string `json:"context_id"`
		}
		if json.Unmarshal(raw, &x) != nil || x.ID == "" {
			t.Fatal("public context ID missing")
		}
		return x.ID
	}
	a := selectCtx("draft225-real-select-a")
	body := map[string]any{"expected_revision": 0, "entries": []any{}}
	path := "/v1/kn/khtc/plans/2031/draft"
	code, original := call(http.MethodPost, path, "draft225-real-create", a, true, body)
	if code != 201 {
		t.Fatalf("real root draft create status=%d", code)
	}
	clk.Advance(time.Second)
	b := selectCtx("draft225-real-select-b")
	if a == b {
		t.Fatal("public renewal reused context identity")
	}
	code, replayed := call(http.MethodPost, path, "draft225-real-create", b, true, body)
	if code != 201 || !bytes.Equal(original, replayed) {
		t.Fatalf("real same-grant renewal status=%d original_ack=%t", code, bytes.Equal(original, replayed))
	}
	for _, target := range []string{path, "/v1/kn/khtc/plans/02031/draft", "/v1/kn/khtc/plans/not-a-year/draft"} {
		code, _ = call(http.MethodPost, target, "draft225-real-create", b, false, body)
		if code != 403 {
			t.Fatalf("current Admin MFA policy must precede cache/year parsing status=%d", code)
		}
	}
	code, raw := call(http.MethodGet, path+"/history", "", b, true, nil)
	if code != 200 {
		t.Fatalf("real history status=%d", code)
	}
	var history struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &history) != nil || len(history.Items) != 1 {
		t.Fatal("renewal or denied replay appended an event")
	}
	// Only synthetic bootstrap current-state revocation is arranged here. The
	// real Accounts resolver must revalidate it; this does not claim a grant/Vault lifecycle.
	_, err = env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_accounts.role_assignment_current SET active=false WHERE tenant_id=$1 AND assignment_id=$2`, tid, assignment)
	if err != nil {
		t.Fatal(err)
	}
	code, _ = call(http.MethodPost, path, "draft225-real-create", b, true, body)
	if code != 403 {
		t.Fatalf("revoked current grant recovered cached ACK status=%d", code)
	}
}

func TestInitialDraft225P804DirectAdhocRecoveryUsesStoredOwnerPlan(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := wireForms(pool, clk, workflow.New(pool, clk), kn, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := &planPublisher225{Publisher: fm.Publisher()}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: probe, Control: fm.Control(), ConditionalControl: fm.ConditionalControl(), Entry: fm.Entry(), MaxSubmissions: KNKHTCProfile.MaxSubmissions})
	tid, actor, org := ids.New(), ids.New(), ids.New()
	owner := authz.KNContext{ID: ids.New(), Role: authz.KNAdmin, ExpiresAt: time.Now().Add(time.Hour), Scope: authz.Scope{All: true}}
	issuer := authztest.New(t)
	router := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: pool, Modules: []httpx.Module{kn, &planSession225{t: t, contexts: map[uuid.UUID]authz.KNContext{actor: owner}}, fm}})
	call := func(path string, body any) (int, []byte) {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "draft225:adhoc:"+ids.New().String())
		req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	if status, _ := call("/v1/extension/khtc/form-template/install", nil); status != 200 {
		t.Fatalf("template status=%d", status)
	}
	principal := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true, KN: &owner})
	ctx := authz.WithPrincipal(context.Background(), principal)
	_, canonical, e := extport.RequireSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if canonical.Support != nil || !canonical.Scope.All {
		t.Fatal("simple admin fixture required")
	}
	canonical.ID = uuid.Nil
	canonical.ExpiresAt = time.Time{}
	canonical.DefaultRoute = ""
	canonical.Scope.Units = []string{}
	canonical.Communes = []string{}
	canonical.Powers = []string{}
	canonical.Duties = []string{}
	canonical.Facilities = []uuid.UUID{}
	canonical.Households = []uuid.UUID{}
	contextHash, e := formsport.OperationHash(canonical)
	if e != nil {
		t.Fatal(e)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 12)
	due := to.AddDate(0, 0, 7)
	input := periodapp.OpenInput{Request: perioddomain.Request{Type: perioddomain.TypeAdhoc, Year: 2026, Name: "Intent trước kế hoạch225", From: &from, To: &to}, Overrides: perioddomain.Overrides{DueAt: &due, ExportDueAt: &due}, Departments: []string{"trong_trot"}}
	plan, e := perioddomain.PlanOf(rc.DefaultRule, input.Request, clock.Zone())
	if e != nil {
		t.Fatal(e)
	}
	plan, e = plan.Apply(input.Overrides, clock.Zone())
	if e != nil {
		t.Fatal(e)
	}
	template, e := fm.Publisher().LatestTemplate(ctx, perioddomain.ProfileKHTC, perioddomain.TemplateCode)
	if e != nil {
		t.Fatal(e)
	}
	max := 3
	batch := formsport.BatchInput{Profile: perioddomain.ProfileKHTC, TemplateID: template.ID, UnitKind: formsport.UnitDepartment, Units: input.Departments, MaxSubmissions: &max, Periods: []formsport.PeriodDef{{Label: plan.Name, DueAt: plan.DueAt, Kind: plan.FormsKind, Key: plan.FormsKey, From: plan.From, To: plan.To, Rule: plan.Rule}}}
	rawInput, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	requestHash, e := formsport.OperationHash(input)
	if e != nil {
		t.Fatal(e)
	}
	batchHash, e := formsport.OperationHash(batch)
	if e != nil {
		t.Fatal(e)
	}
	owned := db.ForModule(pool, extension.DBModule)
	counts := func() (int, int, int) {
		t.Helper()
		var intents, leases, periods int
		e := tenant.WithTenantTx(ctx, owned, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_extension.khtc_adhoc_open_intent),(SELECT count(*) FROM pf_extension.khtc_adhoc_open_lease),(SELECT count(*) FROM pf_extension.khtc_period)`).Scan(&intents, &leases, &periods)
		})
		if e != nil {
			t.Fatal(e)
		}
		return intents, leases, periods
	}
	// A fresh adhoc path is fenced before creating any intent or native dispatch.
	if status, raw := call("/v1/extension/khtc/periods", map[string]any{"report_type": "adhoc", "name": input.Request.Name, "from_date": "2026-10-01", "to_date": "2026-10-13", "departments": input.Departments, "due_at": due, "export_due_at": due}); status != 409 || !bytes.Contains(raw, []byte(`"extension.khtc_plan_required"`)) {
		t.Fatalf("fresh adhoc plan guard status=%d", status)
	}
	if i, l, p := counts(); i != 0 || l != 0 || p != 0 || probe.durableCalls.Load() != 0 {
		t.Fatalf("fresh adhoc wrote state: intents=%d leases=%d periods=%d durable=%d", i, l, p, probe.durableCalls.Load())
	}
	intent := periodstore.AdhocOpenIntent{ID: ids.New(), PlannedPeriodID: ids.New(), ActorRef: actor, SessionID: owner.ID, EffectiveSessionID: owner.ID, ContextHash: contextHash, SessionExpiresAt: owner.ExpiresAt, EffectiveExpiresAt: owner.ExpiresAt, NativeKey: "khtc:" + ids.New().String(), RequestHash: requestHash, BatchHash: batchHash, Input: rawInput, Plan: plan, Batch: batch, Departments: input.Departments, Sources: []extport.KHTCSourceRef{}, At: clk.Now()}
	e = tenant.WithTenantTx(ctx, owned, tid, func(tx pgx.Tx) error { return periodstore.InsertAdhocOpen(ctx, tx, tid, intent) })
	if e != nil {
		t.Fatal(e)
	}
	readIntent := func() *periodstore.AdhocOpenIntent {
		t.Helper()
		var x *periodstore.AdhocOpenIntent
		e := tenant.WithTenantTx(ctx, owned, tid, func(tx pgx.Tx) error { var e error; x, e = periodstore.AdhocOpenByID(ctx, tx, intent.ID); return e })
		if e != nil || x == nil {
			t.Fatalf("intent read error=%T", e)
		}
		return x
	}
	before := readIntent()
	path := "/v1/extension/khtc/adhoc-open-intents/" + intent.ID.String() + "/reconcile"
	if status, raw := call(path, map[string]any{"expected_generation": 0}); status != 409 || !bytes.Contains(raw, []byte(`"extension.khtc_plan_required"`)) {
		t.Fatalf("stored intent plan guard status=%d", status)
	}
	if i, l, p := counts(); i != 1 || l != 0 || p != 0 || probe.durableCalls.Load() != 0 || !reflect.DeepEqual(before, readIntent()) {
		t.Fatalf("deny mutated stored intent: intents=%d leases=%d periods=%d durable=%d", i, l, p, probe.durableCalls.Load())
	}
	if status, _ := call("/v1/kn/khtc/plans/2026/versions", map[string]any{"base_version": 0, "entries": []any{}, "effective_from": "2026-01-01"}); status != 201 {
		t.Fatalf("real empty plan status=%d", status)
	}
	var result struct {
		ID            uuid.UUID `json:"id"`
		FormsPeriodID uuid.UUID `json:"forms_period_id"`
	}
	status, raw := call(path, map[string]any{"expected_generation": 0})
	if status != 200 || json.Unmarshal(raw, &result) != nil || result.ID != intent.PlannedPeriodID || result.FormsPeriodID == uuid.Nil || probe.durableCalls.Load() != 1 {
		t.Fatalf("real durable recovery status=%d durable=%d native_id=%t", status, probe.durableCalls.Load(), result.FormsPeriodID != uuid.Nil)
	}
	snap, e := fm.ConditionalControl().ControlSnapshot(ctx, KNKHTCProfile.Name, result.FormsPeriodID)
	if e != nil || snap.Revision.PeriodID != result.FormsPeriodID || snap.Revision.EventSeq < 1 {
		t.Fatalf("native snapshot proof error=%T", e)
	}
	replayStatus, replayRaw := call(path, map[string]any{"expected_generation": 0})
	var replay struct {
		ID            uuid.UUID `json:"id"`
		FormsPeriodID uuid.UUID `json:"forms_period_id"`
	}
	if replayStatus != 200 || json.Unmarshal(replayRaw, &replay) != nil || replay != result || probe.durableCalls.Load() != 1 {
		t.Fatalf("completed recovery replay status=%d durable=%d", replayStatus, probe.durableCalls.Load())
	}
	if i, l, p := counts(); i != 1 || l != 0 || p != 1 {
		t.Fatalf("recovery cardinality intents=%d leases=%d periods=%d", i, l, p)
	}
}
