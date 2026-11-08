package assembly

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Production Authenticate -> accounts ResolveContext -> registered KN owner
// projection supplies the policy's identity. This does not install a production
// consumer policy or invent a unique OIDC login/session identifier (#259).
func TestEntryReceipt288_ActualHTTPCurrentOwnerAuthority(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	tid, actor, assignment, selected, otherSelected := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	now := time.Now().UTC()
	admin := env.AdminPool(t, migrations.PF)
	_, err = admin.Exec(context.Background(), `INSERT INTO pf_accounts.role_assignment_current
(tenant_id,assignment_id,user_ref,role,department_codes,commune_codes,valid_from,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
VALUES($1,$2,$3,'admin',ARRAY[]::text[],ARRAY[]::text[],$4,true,$5,1,$3,$4,$3,$4)`, tid, assignment, actor, now.Add(-time.Hour), ids.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{selected, otherSelected} {
		_, err = admin.Exec(context.Background(), `INSERT INTO pf_accounts.session_context
(tenant_id,context_id,user_ref,assignment_id,role,idem_key,created_at,expires_at)
VALUES($1,$2,$3,$4,'admin',$2::uuid::text,$5,$6)`, tid, id, actor, assignment, now, now.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	var stamps [][]byte
	err = mods.Forms.UseReviewOperationPolicy("kn_khtc", func(ctx context.Context, p authz.Principal, req formsport.ReviewOperationRequest) (formsport.ReviewOperationGrant, error) {
		actual, k, e := extport.RequireSession(ctx)
		if e != nil {
			return formsport.ReviewOperationGrant{}, e
		}
		if p.ActorRef != actor || p.TenantID != tid || actual.ActorRef != p.ActorRef || !p.MFA || k.AssignmentID != assignment || k.ReadOnly() || k.Role != extport.SessionRoleAdmin || req.Profile != "kn_khtc" || p.SelectedSession.ID() != k.ID {
			t.Error("untrusted caller facts reached Forms owner policy")
		}
		calls++
		stamp, e := p.SelectedSession.AuthorizationStamp()
		if e != nil {
			t.Fatal(e)
		}
		stamps = append(stamps, stamp)
		return formsport.ReviewOperationGrant{Allowed: true, Binding: "actual-current-owner288"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	rg := engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mods.Accounts.Middleware())
	req := formsport.ReviewOperationRequest{OperationID: ids.New(), Profile: "kn_khtc", PeriodID: ids.New(), SubmissionID: ids.New(), Unit: "trong_trot", ExpectedVersion: 1, Decision: "accepted"}
	rg.GET("/receipt-authority288", func(c *gin.Context) {
		r, e := mods.Forms.EntryOperations().ReadReviewOperation(c.Request.Context(), req)
		if r.Outcome != formsport.ReviewOperationUnknown {
			t.Error("authority probe manufactured outcome")
		}
		// Admin has no native delegated review proof here. Owner receipt policy
		// must never bypass existing native authorization or read a fake target.
		if e == nil || problem.From(e).Code != problem.Forbidden.Code {
			t.Errorf("native authorization bypass: %v", e)
		}
		httpx.Fail(c, e)
	})
	call := func(who, tenantID, contextID uuid.UUID, mfa bool) {
		t.Helper()
		token := issuer.Sign(t, authztest.Token{Subject: who, TenantID: tenantID, Roles: []string{"kn_user"}, MFA: mfa})
		request := httptest.NewRequest(http.MethodGet, "/v1/receipt-authority288", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set(authz.ContextHeader, contextID.String())
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unexpected authority response: %d %s", response.Code, response.Body.String())
		}
	}
	call(actor, tid, selected, true)
	call(actor, tid, selected, true)
	call(actor, tid, otherSelected, true)
	if calls != 3 || !bytes.Equal(stamps[0], stamps[1]) || bytes.Equal(stamps[0], stamps[2]) {
		t.Fatal("selected context authority stamp not bound to actual owner")
	}
	call(ids.New(), tid, selected, true)
	call(actor, ids.New(), selected, true)
	call(actor, tid, selected, false)
	if calls != 3 {
		t.Fatal("invalid actor/tenant/MFA reached supplier policy")
	}
	_, err = admin.Exec(context.Background(), `UPDATE pf_accounts.role_assignment_current SET active=false WHERE tenant_id=$1 AND assignment_id=$2`, tid, assignment)
	if err != nil {
		t.Fatal(err)
	}
	call(actor, tid, selected, true)
	if calls != 3 {
		t.Fatal("revoked assignment reached cached supplier policy")
	}
}
