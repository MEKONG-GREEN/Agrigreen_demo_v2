package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type scopeCheckingDB struct {
	tenant.Beginner
	tenantID, node uuid.UUID
	checks         atomic.Int64
}

func (db *scopeCheckingDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := db.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &scopeCheckingTx{Tx: tx, db: db}, nil
}

type scopeCheckingTx struct {
	pgx.Tx
	db *scopeCheckingDB
}

func (tx *scopeCheckingTx) checkScope(ctx context.Context) error {
	var tid, node, xa, role string
	err := tx.Tx.QueryRow(ctx, `SELECT current_setting('app.tenant_id'), current_setting('app.node_id'),
		coalesce(current_setting('app.xa_ids', true), ''), current_user`).Scan(&tid, &node, &xa, &role)
	if err != nil {
		return err
	}
	if tid != tx.db.tenantID.String() || node != tx.db.node.String() || xa != "" || role != "r_cooperative" {
		return fmt.Errorf("native lookup changed tenant/node/commune/module write scope")
	}
	tx.db.checks.Add(1)
	return nil
}

func (tx *scopeCheckingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(sql, "INSERT INTO pf_coop.household_membership") || strings.HasPrefix(sql, "UPDATE pf_coop.household_membership") {
		if err := tx.checkScope(ctx); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx *scopeCheckingTx) Commit(ctx context.Context) error {
	if err := tx.checkScope(ctx); err != nil {
		return err
	}
	return tx.Tx.Commit(ctx)
}

func TestNativeManagerRejoinKeepsWriteScope(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300004"))
	if r.status != http.StatusCreated {
		t.Fatalf("seed household: %d %v", r.status, r.body)
	}
	ref := str(r.body, "household", "household_ref")
	checked := &scopeCheckingDB{Beginner: f.db, tenantID: f.tenant, node: tr.c1}
	f.svc.DB = checked
	for _, step := range []struct {
		coop   any
		change string
	}{{nil, "left"}, {tr.c1.String(), "joined"}} {
		r := mgr.appDo(http.MethodPost, "/v1/households/"+ref+"/membership", map[string]any{
			"cooperative_ref": step.coop, "reason": "Nông hộ thay đổi thành viên HTX",
		})
		if r.status != http.StatusOK || r.body["change"] != step.change || str(r.body, "household", "household_ref") != ref {
			t.Fatalf("%s with original write scope: %d %v", step.change, r.status, r.body)
		}
	}
	if checked.checks.Load() != 4 {
		t.Fatalf("expected two membership writes and two commits with unchanged scope, got %d", checked.checks.Load())
	}
	events := f.householdEvents(t, coopport.EventHouseholdMembershipChanged, ref)
	if len(events) != 2 || events[0]["change"] != "left" || events[1]["change"] != "joined" {
		t.Fatalf("membership/outbox atomic result: %v", events)
	}
}

// A realm manager's resolved commune must never replace a selected session's
// node or commune scope, even if the realm token itself belongs to another HTX.
func TestNativeManagerLookupPreservesSelectedScope(t *testing.T) {
	for _, mode := range []string{"selected_session", "kn_context"} {
		t.Run(mode, func(t *testing.T) {
			type projection struct {
				selected *authz.Session
				kn       *authz.KNContext
			}
			var active atomic.Pointer[projection]
			f := newFixtureWith(t, func(c *gin.Context) {
				if scope := active.Load(); scope != nil {
					p, _ := authz.FromContext(c.Request.Context())
					p.SelectedSession, p.KN = scope.selected, scope.kn
					c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
				}
				c.Next()
			})
			tr := f.seedTree(f.root())
			mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
			r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300001"))
			if r.status != http.StatusCreated {
				t.Fatalf("seed household: %d %v", r.status, r.body)
			}
			ref := str(r.body, "household", "household_ref")
			if mode == "selected_session" {
				selected, err := authz.NewSession("native_test", ids.New(), authz.SessionScope{
					NodeID: tr.c2, UnitCodes: []string{"31001"}, RequiresNode: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				active.Store(&projection{selected: &selected})
			} else {
				active.Store(&projection{kn: &authz.KNContext{
					ID: ids.New(), Role: authz.KNXa, OrgRef: tr.t30002,
					Communes: []string{"30002"}, Scope: authz.Scope{Units: []string{"30002"}},
				}})
			}
			assertManagerLookupDenied(t, mgr, ref, tr.c1)
			if events := f.householdEvents(t, coopport.EventHouseholdMembershipChanged, ref); len(events) != 0 {
				t.Fatalf("denied selected scope emitted membership events: %d", len(events))
			}
		})
	}
}

func TestNativeManagerLookupRequiresProjectedNode(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300002"))
	if r.status != http.StatusCreated {
		t.Fatalf("seed household: %d %v", r.status, r.body)
	}
	ref := str(r.body, "household", "household_ref")
	admin := f.env.AdminPool(t, migrations.PF)
	for _, stmt := range []string{
		"DELETE FROM k_ref.node_closure WHERE tenant_id = $1",
		"DELETE FROM k_ref.org WHERE tenant_id = $1",
	} {
		if _, err := admin.Exec(context.Background(), stmt, f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	assertManagerLookupDenied(t, mgr, ref, tr.c1)
	if events := f.householdEvents(t, coopport.EventHouseholdMembershipChanged, ref); len(events) != 0 {
		t.Fatalf("empty projection emitted membership events: %d", len(events))
	}
}

type lookupScope struct{ scope authz.Scope }

func (s lookupScope) Scope(context.Context) (authz.Scope, error) { return s.scope, nil }

func TestNativeManagerLookupRejectsUnboundedScope(t *testing.T) {
	for _, sc := range []authz.Scope{{All: true}, {}} {
		name := "empty"
		if sc.All {
			name = "unexpected_all"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tr := f.seedTree(f.root())
			mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
			r := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300003"))
			if r.status != http.StatusCreated {
				t.Fatalf("seed household: %d %v", r.status, r.body)
			}
			ref := str(r.body, "household", "household_ref")
			f.svc.Scope = lookupScope{scope: sc}
			r = mgr.appDo(http.MethodGet, "/v1/households/"+ref+"/contact", nil)
			if r.status != http.StatusForbidden && r.status != http.StatusNotFound {
				t.Fatalf("unbounded scope exposed contact: %d %v", r.status, r.body)
			}
			r = mgr.appDo(http.MethodPost, "/v1/households/"+ref+"/membership", map[string]any{
				"cooperative_ref": nil, "reason": "Nông hộ xin rời HTX hiện tại",
			})
			if r.status != http.StatusForbidden && r.status != http.StatusNotFound {
				t.Fatalf("unbounded scope changed membership: %d %v", r.status, r.body)
			}
			if events := f.householdEvents(t, coopport.EventHouseholdMembershipChanged, ref); len(events) != 0 {
				t.Fatalf("unbounded scope emitted membership events: %d", len(events))
			}
		})
	}
}

func assertManagerLookupDenied(t *testing.T, mgr caller, ref string, ownCoop uuid.UUID) {
	t.Helper()
	if r := mgr.appDo(http.MethodGet, "/v1/households/"+ref+"/contact", nil); r.status != http.StatusNotFound {
		t.Fatalf("denied contact: %d %v", r.status, r.body)
	}
	if r := mgr.appDo(http.MethodPost, "/v1/households/"+ref+"/membership", map[string]any{
		"cooperative_ref": ownCoop.String(), "reason": "Nông hộ xin vào lại HTX",
	}); r.status != http.StatusNotFound {
		t.Fatalf("denied membership: %d %v", r.status, r.body)
	}
	r := mgr.appDo(http.MethodPost, "/v1/households/copy", map[string]any{
		"items": []map[string]any{{"source_ref": ref, "full_name": "Bản sao bị từ chối", "phone": "0907300099"}},
	})
	if r.status != http.StatusOK || r.body["created"] != float64(0) || r.body["rejected"] != float64(1) {
		t.Fatalf("denied copy: %d %v", r.status, r.body)
	}
}
