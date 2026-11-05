package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	planfixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcplan225"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	ownedReplay225 := db.ForModule(pool, extension.DBModule)
	svc := planfixture.NewApp225(ownedReplay225, ownedReplay225, clk)
	own, wasReplay, replayErr := svc.Save(authz.WithPrincipal(context.Background(), principal), planfixture.Input225{Year: 2030, ExpectedRevision: 0, IdemKey: "draft225-create", Note: "initial", Entries: []planfixture.Entry225{{Ref: entryRef, Tab: "programs", Kind: "program", ProgramRef: &program.Ref, Name: "Tên dòng đã chốt225", Code: "P225", SortOrder: 1, Tracking: "tracking", Communes: []string{}}}})
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
	owned := db.ForModule(pool, extension.DBModule)
	intent, e := planfixture.NewLegacyIntent225(ctx, clk, fm.Publisher(), 2026)
	if e != nil {
		t.Fatal(e)
	}
	counts := func() (int, int, int) {
		t.Helper()
		i, l, p, e := planfixture.CalendarCounts225(ctx, owned)
		if e != nil {
			t.Fatal(e)
		}
		return i, l, p
	}
	// A fresh adhoc path is fenced before creating any intent or native dispatch.
	if status, raw := call("/v1/extension/khtc/periods", map[string]any{"report_type": "adhoc", "name": intent.Name, "from_date": "2026-10-01", "to_date": "2026-10-13", "departments": intent.Departments, "due_at": intent.DueAt, "export_due_at": intent.DueAt}); status != 409 || !bytes.Contains(raw, []byte(`"extension.khtc_plan_required"`)) {
		t.Fatalf("fresh adhoc plan guard status=%d", status)
	}
	if i, l, p := counts(); i != 0 || l != 0 || p != 0 || probe.durableCalls.Load() != 0 {
		t.Fatalf("fresh adhoc wrote state: intents=%d leases=%d periods=%d durable=%d", i, l, p, probe.durableCalls.Load())
	}
	if e = intent.Insert(ctx, owned); e != nil {
		t.Fatal(e)
	}
	readIntent := func() planfixture.IntentSnapshot225 {
		t.Helper()
		x, e := intent.Snapshot(ctx, owned)
		if e != nil {
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

// The fixture loses the ACK only after the real owned COMMIT succeeds.
func TestInitialDraft225LostCommitPoolOneStoredReplayAndHistoryPaging(t *testing.T) {
	env := pgtest.Get(t)
	base := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	cfg := base.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	tid, actor, org := ids.New(), ids.New(), ids.New()
	owner := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: "ke_hoach_tai_chinh", OrgRef: org, ExpiresAt: clk.Now().Add(time.Hour), Scope: authz.Scope{Units: []string{"X1"}}}
	p := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &owner})
	ctx = authz.WithPrincipal(ctx, p)
	owned := db.ForModule(pool, extension.DBModule)
	fault := planfixture.NewLostAckDB225(owned)
	svc := planfixture.NewApp225(fault, owned, clk)
	input := planfixture.Input225{Year: 2036, ExpectedRevision: 0, IdemKey: "draft225-lost-commit", Entries: []planfixture.Entry225{}, Note: "saved but ACK lost"}
	_, replay, err := svc.Save(ctx, input)
	if !errors.Is(err, context.DeadlineExceeded) || replay || !fault.Fired() {
		t.Fatalf("commit fault not exercised: replay=%t err=%T", replay, err)
	}
	first, replay, err := svc.Save(ctx, input)
	if err != nil || !replay || first.Event.Seq != 1 {
		t.Fatalf("exact commit recovery failed: replay=%t err=%T", replay, err)
	}
	for revision := int64(2); revision <= 101; revision++ {
		next, hit, err := svc.Save(ctx, planfixture.Input225{Year: input.Year, DraftID: first.Event.DraftID, ExpectedRevision: revision - 1, Entries: []planfixture.Entry225{}, IdemKey: fmt.Sprintf("draft225-page-%d", revision)})
		if err != nil || hit || next.Event.Seq != revision {
			t.Fatalf("history append revision=%d replay=%t err=%T", revision, hit, err)
		}
	}
	historical, hit, err := svc.Save(ctx, input)
	if err != nil || !hit || historical.Event.ID != first.Event.ID || historical.Event.Seq != 1 {
		t.Fatalf("old operation was promoted to latest: replay=%t err=%T", hit, err)
	}
	page, err := svc.History(ctx, input.Year, 0, 100)
	if err != nil || len(page) != 100 || page[0].Seq != 101 || page[99].Seq != 2 {
		t.Fatalf("complete first history page size=%d err=%T", len(page), err)
	}
	tail, err := svc.History(ctx, input.Year, page[99].Seq, 100)
	if err != nil || len(tail) != 1 || tail[0].ID != first.Event.ID {
		t.Fatalf("history tail size=%d err=%T", len(tail), err)
	}
	renewed := owner
	renewed.ID = ids.New()
	renewed.ExpiresAt = owner.ExpiresAt.Add(time.Hour)
	renewedP := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &renewed})
	again, hit, err := svc.Save(authz.WithPrincipal(ctx, renewedP), input)
	if err != nil || !hit || again.Event.ID != first.Event.ID {
		t.Fatalf("same-authority renewed replay failed: replay=%t err=%T", hit, err)
	}
	renewed.ViewOnly = true
	readonly := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: false, KN: &renewed})
	_, _, err = svc.Save(authz.WithPrincipal(ctx, readonly), input)
	var denied *problem.Problem
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatalf("current read-only authority was not enforced: err=%T", err)
	}
	count, _, err := planfixture.YearCounts225(ctx, owned, input.Year)
	if err != nil || count != 101 {
		t.Fatalf("receipt replays appended events: count=%d err=%T", count, err)
	}
}

type heldDraftYear225 struct {
	tenant.Beginner
	key     string
	hold    bool
	once    atomic.Bool
	before  chan uint32
	locked  chan struct{}
	release chan struct{}
}
type heldDraftYearTx225 struct {
	pgx.Tx
	owner *heldDraftYear225
}

func (b *heldDraftYear225) BeginTx(ctx context.Context, opt pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, opt)
	if e != nil {
		return nil, e
	}
	return &heldDraftYearTx225{Tx: tx, owner: b}, nil
}
func (tx *heldDraftYearTx225) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	target := strings.Contains(sql, "pg_advisory_xact_lock") && len(args) == 1 && args[0] == tx.owner.key
	first := target && tx.owner.once.CompareAndSwap(false, true)
	if first {
		tx.owner.before <- tx.Conn().PgConn().PID()
	}
	tag, e := tx.Tx.Exec(ctx, sql, args...)
	if e == nil && first && tx.owner.hold {
		close(tx.owner.locked)
		select {
		case <-tx.owner.release:
		case <-ctx.Done():
			return tag, ctx.Err()
		}
	}
	return tag, e
}
func TestInitialDraft225CreateAndLegacyPublishSerializeBothOrders(t *testing.T) {
	for _, draftFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("draft_first_%t", draftFirst), func(t *testing.T) {
			env := pgtest.Get(t)
			pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
			clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
			tid, actor, org := ids.New(), ids.New(), ids.New()
			year := 2037
			owner := authz.KNContext{ID: ids.New(), Role: authz.KNTinh, Department: "ke_hoach_tai_chinh", OrgRef: org, ExpiresAt: clk.Now().Add(time.Hour), Scope: authz.Scope{Units: []string{"X1"}}}
			p := fixture.Bind(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, KN: &owner})
			ctx, cancel := context.WithTimeout(authz.WithPrincipal(context.Background(), p), 15*time.Second)
			defer cancel()
			owned := db.ForModule(pool, extension.DBModule)
			firstDB := &heldDraftYear225{Beginner: owned, key: fmt.Sprintf("plan:%d", year), hold: true, before: make(chan uint32, 1), locked: make(chan struct{}), release: make(chan struct{})}
			secondDB := &heldDraftYear225{Beginner: owned, key: firstDB.key, before: make(chan uint32, 1)}
			var release sync.Once
			unlock := func() { release.Do(func() { close(firstDB.release) }) }
			defer unlock()
			first := planfixture.NewApp225(firstDB, owned, clk)
			second := planfixture.NewApp225(secondDB, owned, clk)
			run := func(svc *planfixture.App225, draft bool, key string) error {
				if draft {
					_, _, err := svc.Save(ctx, planfixture.Input225{Year: year, IdemKey: key, Entries: []planfixture.Entry225{}})
					return err
				}
				return svc.Publish(ctx, year, key)
			}
			firstDone := make(chan error, 1)
			secondDone := make(chan error, 1)
			firstPending, secondPending := true, false
			defer func() {
				unlock()
				cancel()
				if firstPending {
					select {
					case <-firstDone:
					case <-time.After(time.Second):
					}
				}
				if secondPending {
					select {
					case <-secondDone:
					case <-time.After(time.Second):
					}
				}
			}()
			go func() { firstDone <- run(first, draftFirst, "draft225-race-first") }()
			select {
			case <-firstDB.locked:
			case err := <-firstDone:
				firstPending = false
				t.Fatalf("first did not acquire year lock: err=%T", err)
			case <-ctx.Done():
				t.Fatal("year lock barrier timed out")
			}
			secondPending = true
			go func() { secondDone <- run(second, !draftFirst, "draft225-race-second") }()
			var pid uint32
			select {
			case pid = <-secondDB.before:
			case err := <-secondDone:
				secondPending = false
				t.Fatalf("second did not request year lock: err=%T", err)
			case <-ctx.Done():
				t.Fatal("second lock request timed out")
			}
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			waiting := false
			for !waiting {
				if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND NOT granted)`, pid).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if !waiting {
					select {
					case <-ticker.C:
					case <-ctx.Done():
						t.Fatal("real advisory waiter not observed")
					}
				}
			}
			unlock()
			select {
			case err := <-firstDone:
				firstPending = false
				if err != nil {
					t.Fatalf("first winner error=%T", err)
				}
			case <-ctx.Done():
				t.Fatal("first completion timed out")
			}
			select {
			case err := <-secondDone:
				secondPending = false
				var p *problem.Problem
				if !errors.As(err, &p) || p.Status != 409 {
					t.Fatalf("competing operation must conflict: err=%T", err)
				}
			case <-ctx.Done():
				t.Fatal("second completion timed out")
			}
			drafts, versions, err := planfixture.YearCounts225(ctx, owned, year)
			if err != nil || draftFirst && (drafts != 1 || versions != 0) || !draftFirst && (drafts != 0 || versions != 1) {
				t.Fatalf("year families both persisted: drafts=%d versions=%d err=%T", drafts, versions, err)
			}
		})
	}
}
