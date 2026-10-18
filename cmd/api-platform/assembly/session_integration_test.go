package assembly

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Synthetic current-state fixtures test root wiring; the complete append-only
// assignment/context lifecycle remains covered by accounts/handler integration.
func TestBuildBindsOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	tid, actor, assignment, selected, tokenOrg := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	now := time.Now().UTC()
	admin := env.AdminPool(t, migrations.PF)
	_, err = admin.Exec(context.Background(), `INSERT INTO pf_accounts.role_assignment_current
  (tenant_id,assignment_id,user_ref,role,department_codes,commune_codes,valid_from,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
  VALUES($1,$2,$3,'admin',ARRAY[]::text[],ARRAY[]::text[],$4,true,$5,1,$3,$4,$3,$4)`, tid, assignment, actor, now.Add(-time.Hour), ids.New())
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(context.Background(), `INSERT INTO pf_accounts.session_context
  (tenant_id,context_id,user_ref,assignment_id,role,idem_key,created_at,expires_at)
  VALUES($1,$2,$3,$4,'admin','root-session-test',$5,$6)`, tid, selected, actor, assignment, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	rg := engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mods.Accounts.Middleware())
	rg.GET("/session-probe", func(c *gin.Context) {
		ctx := c.Request.Context()
		p, ok := authz.FromContext(ctx)
		if !ok {
			t.Error("missing authenticated identity")
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		var node, dbActor, dbTenant string
		err := db.WithContextTx(ctx, env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/content"), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT coalesce(current_setting('app.node_id',true),''),current_setting('app.actor_ref'),current_setting('app.tenant_id')`).Scan(&node, &dbActor, &dbTenant)
		})
		if err != nil {
			t.Error(err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		profile, sessionID := "", ""
		if p.SelectedSession != nil {
			profile, sessionID = p.SelectedSession.Profile(), p.SelectedSession.ID().String()
		}
		c.JSON(http.StatusOK, gin.H{"actor": p.ActorRef.String(), "tenant": p.TenantID.String(), "org": p.OrgRef.String(), "mfa": p.MFA, "kn_user": p.Has("kn_user"), "kn_officer": p.Has("kn_officer"), "profile": profile, "session": sessionID, "node": node, "db_actor": dbActor, "db_tenant": dbTenant})
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	request := func(subject, tenantID uuid.UUID, mfa, withSession bool) (int, map[string]any) {
		t.Helper()
		token := issuer.Sign(t, authztest.Token{Subject: subject, TenantID: tenantID, OrgRef: tokenOrg, Roles: []string{"kn_user"}, MFA: mfa})
		req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/session-probe", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if withSession {
			req.Header.Set(authz.ContextHeader, selected.String())
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body
	}
	status, body := request(actor, tid, true, true)
	if status != http.StatusOK || body["profile"] != "kn" || body["session"] != selected.String() || body["node"] != "" || body["actor"] != actor.String() || body["db_actor"] != actor.String() || body["tenant"] != tid.String() || body["db_tenant"] != tid.String() || body["org"] != tokenOrg.String() || body["mfa"] != true || body["kn_user"] != true || body["kn_officer"] != false {
		t.Fatalf("selected root binding: %d %v", status, body)
	}
	status, body = request(actor, tid, true, false)
	if status != http.StatusOK || body["profile"] != "" || body["session"] != "" || body["node"] != tokenOrg.String() {
		t.Fatalf("native unselected request: %d %v", status, body)
	}
	for _, c := range []struct {
		name          string
		actor, tenant uuid.UUID
		mfa           bool
	}{{"other actor", ids.New(), tid, true}, {"other tenant", actor, ids.New(), true}, {"admin missing MFA", actor, tid, false}} {
		status, body = request(c.actor, c.tenant, c.mfa, true)
		if status != http.StatusForbidden {
			t.Fatalf("%s: %d %v", c.name, status, body)
		}
	}
}
