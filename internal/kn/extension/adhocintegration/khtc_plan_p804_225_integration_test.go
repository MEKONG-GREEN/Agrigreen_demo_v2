package adhocintegration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	nativeforms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/knadhoc"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// Native fixture exposes only real supplier ports and its HTTP registrar.
type nativeHTTP225 struct{ *nativeforms.Fixture }

func (*nativeHTTP225) RegisterAdmin(*gin.RouterGroup) {}

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
		p = bindKNTestSession(s.t, p)
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

func TestInitialDraft225P804DirectAdhocRecoveryUsesStoredOwnerPlan(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	kn := extension.New(pool, clk, extension.Deps{})
	fm, err := nativeforms.New(pool, clk, nativeforms.Deps{Profiles: []formsport.Profile{fixtureKHTCProfile}, Validators: map[string]formsport.Validator{fixtureKHTCProfile.Name: kn.KHTCFormValidator()}})
	if err != nil {
		t.Fatal(err)
	}
	conditional, ok := fm.Control().(formsport.ConditionalPeriodControl)
	if !ok || conditional == nil {
		t.Fatal("real conditional period control capability missing")
	}
	probe := &planPublisher225{Publisher: fm.Publisher()}
	kn.UseKHTCPeriods(extension.KHTCDeps{Forms: probe, Control: fm.Control(), ConditionalControl: conditional, Entry: fm.Entry(), MaxSubmissions: fixtureKHTCProfile.MaxSubmissions})
	tid, actor, org := ids.New(), ids.New(), ids.New()
	owner := authz.KNContext{ID: ids.New(), Role: authz.KNAdmin, ExpiresAt: time.Now().Add(time.Hour), Scope: authz.Scope{All: true}}
	issuer := authztest.New(t)
	router := assembly.NewRouter(assembly.Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: pool, Modules: []httpx.Module{kn, &planSession225{t: t, contexts: map[uuid.UUID]authz.KNContext{actor: owner}}, &nativeHTTP225{Fixture: fm}}})
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
	principal := bindKNTestSession(t, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: org, Roles: []string{"kn_user"}, MFA: true, KN: &owner})
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
	snap, e := conditional.ControlSnapshot(ctx, fixtureKHTCProfile.Name, result.FormsPeriodID)
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
