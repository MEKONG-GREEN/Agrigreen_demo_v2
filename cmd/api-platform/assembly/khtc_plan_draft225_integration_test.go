package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	calls atomic.Int64
}

func (p *planPublisher225) PublishBatch(ctx context.Context, in formsport.BatchInput, key string) ([]formsport.PublishedPeriod, error) {
	p.calls.Add(1)
	return p.Publisher.PublishBatch(ctx, in, key)
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
