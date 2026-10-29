package contextdata_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// #60 parity: với mọi ngữ cảnh mà middleware pf/accounts thật sự dựng (Principal.KN + phiên chủ chiếu qua sổ đăng ký
// KN), đơn vị mặc định của GET /v1/internal-units qua adapter chủ KN trùng cầu nối cũ (SessionPolicies rỗng): cùng
// mã trạng thái, cùng đơn vị, cùng danh sách dưới RLS. Khác biệt duy nhất được chấp nhận là câu chữ của 422 khi
// Admin không có đơn vị mặc định (cùng status, cùng field org_ref).
func TestAccountDefaultUnitParityWithLegacyBridge(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	tid, actor, homeOrg, lostOrg, unitA, unitB := ids.New(), ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	for _, row := range []struct {
		unit  uuid.UUID
		title string
	}{{unitA, "Home unit"}, {unitB, "Assigned unit"}} {
		if _, err := admin.Exec(context.Background(), `INSERT INTO pf_accounts.internal_unit (tenant_id,id,org_ref,code,kind,title,last_event_id,last_seq,updated_by) VALUES($1,$2,$3,'station','tram',$4,$5,1,$6)`,
			tid, ids.New(), row.unit, row.title, ids.New(), actor); err != nil {
			t.Fatal(err)
		}
	}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	deps := &accountPolicyDepartments{units: map[uuid.UUID]uuid.UUID{homeOrg: unitA}}
	legacy := accounts.New(pool, clock.System{}, accounts.Deps{Departments: deps})
	owner := accounts.New(pool, clock.System{}, accounts.Deps{Departments: deps, SessionPolicies: extension.AccountSessionPolicies(deps)})
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	contexts := []struct {
		name string
		org  uuid.UUID // Nút đã xác thực của token
		kc   *authz.KNContext
	}{
		{"native", homeOrg, nil},
		{"native without node", uuid.Nil, nil},
		{"province", homeOrg, &authz.KNContext{ID: ids.New(), AssignmentID: ids.New(), Role: authz.KNTinh, Department: "trong_trot", OrgRef: unitB, UnitRef: unitB, Scope: authz.Scope{All: true}, ExpiresAt: exp}},
		{"commune", homeOrg, &authz.KNContext{ID: ids.New(), AssignmentID: ids.New(), Role: authz.KNXa, Department: "trong_trot", OrgRef: unitB, UnitRef: unitB, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}, ExpiresAt: exp}},
		{"support", homeOrg, &authz.KNContext{ID: ids.New(), Role: authz.KNXa, Department: "trong_trot", OrgRef: unitB, UnitRef: unitB, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}, ExpiresAt: exp,
			Support: &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: ids.New()}}},
		{"admin at home node", homeOrg, &authz.KNContext{ID: ids.New(), AssignmentID: ids.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}, ExpiresAt: exp}},
		{"admin node outside units", lostOrg, &authz.KNContext{ID: ids.New(), AssignmentID: ids.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}, ExpiresAt: exp}},
		{"admin without node", uuid.Nil, &authz.KNContext{ID: ids.New(), AssignmentID: ids.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}, ExpiresAt: exp}},
	}
	call := func(mod *accounts.Module, p *authz.Principal, query string) (int, string) {
		engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
		group := engine.Group("/v1", func(c *gin.Context) {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), *p))
		})
		mod.RegisterApp(group)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal-units"+query, nil))
		return response.Code, response.Body.String()
	}
	for _, c := range contexts {
		for _, query := range []string{"", "?org_ref=" + unitB.String(), "?org_ref=" + unitA.String()} {
			t.Run(c.name+query, func(t *testing.T) {
				p := &authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: c.org, Roles: []string{authz.RolePlatformAdmin, "kn_officer"}, MFA: true}
				if c.kc != nil { // đúng như middleware: KN cũ rồi phiên chủ chiếu từ chính ngữ cảnh đó
					p.KN = c.kc
					sel, err := registry.ResolveSelected(context.Background(), *p)
					if err != nil {
						t.Fatal(err)
					}
					p.SelectedSession = &sel
				}
				ls, lb := call(legacy, p, query)
				os, ob := call(owner, p, query)
				if ls != os {
					t.Fatalf("status legacy=%d owner=%d\nlegacy=%s\nowner=%s", ls, os, lb, ob)
				}
				if ls == http.StatusOK {
					if lb != ob {
						t.Fatalf("body differs\nlegacy=%s\nowner=%s", lb, ob)
					}
					return
				}
				var lp, op struct {
					Status int    `json:"status"`
					Code   string `json:"code"`
					Errors []struct {
						Field string `json:"field"`
					} `json:"errors"`
				}
				_ = json.Unmarshal([]byte(lb), &lp)
				_ = json.Unmarshal([]byte(ob), &op)
				if lp.Status != op.Status || lp.Code != op.Code || len(lp.Errors) != len(op.Errors) || (len(lp.Errors) > 0 && lp.Errors[0].Field != op.Errors[0].Field) {
					t.Fatalf("problem differs\nlegacy=%s\nowner=%s", lb, ob)
				}
				t.Logf("same %d: legacy=%s owner=%s", ls, strings.TrimSpace(lb), strings.TrimSpace(ob))
			})
		}
	}
}

// #60 parity: thao tác ghi trong chế độ hỗ trợ đi qua xác thực JWT + middleware pf/accounts thật, ngữ cảnh hỗ trợ giải
// từ PostgreSQL. Nhật ký hỗ trợ ghi qua adapter chủ KN trùng từng cột với cầu nối cũ (trừ request_id/thời điểm).
func TestAccountSupportAuditParityWithLegacyBridge(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	tid, actor, onBehalf, unit, selected, support := ids.New(), ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	now := time.Now().UTC()
	if _, err := admin.Exec(context.Background(), `INSERT INTO pf_accounts.support_session (tenant_id,support_session_id,admin_ref,acted_role,department_code,org_ref,on_behalf_of,context_id,started_at,expires_at,last_event_id,last_seq) VALUES($1,$2,$3,'kn_xa','trong_trot',$4,$5,$6,$7,$8,$9,1)`,
		tid, support, actor, unit, onBehalf, selected, now, now.Add(time.Hour), ids.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), `INSERT INTO pf_accounts.session_context (tenant_id,context_id,user_ref,role,department_code,org_ref,unit_ref,support_session_id,on_behalf_of,idem_key,created_at,expires_at) VALUES($1,$2,$3,'kn_xa','trong_trot',$4,$4,$5,$6,'account-owner-parity',$7,$8)`,
		tid, selected, actor, unit, support, onBehalf, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	issuer := authztest.New(t)
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	reason := "Hỗ trợ nhập liệu theo cuộc gọi được xác nhận"
	type auditRow struct {
		Actor, OnBehalf, Context uuid.UUID
		Role, Dept, Why, Phase   string
		Status                   int
		Function, Content        string
	}
	run := func(policies accountsport.SessionPolicies, withReason bool) (int, []auditRow) {
		mod := accounts.New(pool, clock.System{}, accounts.Deps{Scope: accountPolicyScope{}, Departments: &accountPolicyDepartments{}, Sessions: registry, SessionPolicies: policies})
		engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
		group := engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), mod.Middleware())
		group.POST("/audit-probe/:id", func(c *gin.Context) { c.JSON(201, gin.H{"ok": true}) })
		req := httptest.NewRequest(http.MethodPost, "/v1/audit-probe/resource", strings.NewReader(`{"secret":"body-must-not-be-logged"}`))
		req.Header.Set("Authorization", "Bearer "+issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, OrgRef: ids.New(), Roles: []string{"platform_admin"}, MFA: true}))
		req.Header.Set(authz.ContextHeader, selected.String())
		if withReason {
			req.Header.Set(authz.ReasonHeader, url.PathEscape(reason))
		}
		var before int64
		if err := admin.QueryRow(context.Background(), `SELECT coalesce(max(seq),0) FROM pf_accounts.support_action_log WHERE tenant_id=$1`, tid).Scan(&before); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		rows, err := admin.Query(context.Background(), `SELECT actor_ref,on_behalf_of,context_id,acted_role,department_code,reason,phase,coalesce(status_code,0),function,content::text FROM pf_accounts.support_action_log WHERE tenant_id=$1 AND support_session_id=$2 AND seq>$3 ORDER BY seq`, tid, support, before)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []auditRow
		for rows.Next() {
			var r auditRow
			if err := rows.Scan(&r.Actor, &r.OnBehalf, &r.Context, &r.Role, &r.Dept, &r.Why, &r.Phase, &r.Status, &r.Function, &r.Content); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return response.Code, out
	}
	for _, withReason := range []bool{true, false} {
		ls, lrows := run(accountsport.SessionPolicies{}, withReason)
		os, orows := run(extension.AccountSessionPolicies(&accountPolicyDepartments{}), withReason)
		if ls != os || len(lrows) != len(orows) {
			t.Fatalf("reason=%v legacy=%d %+v owner=%d %+v", withReason, ls, lrows, os, orows)
		}
		for i := range lrows {
			if lrows[i] != orows[i] {
				t.Fatalf("row %d differs\nlegacy=%+v\nowner=%+v", i, lrows[i], orows[i])
			}
		}
		if withReason {
			if os != 201 || len(orows) != 2 || orows[0].Actor != actor || orows[0].OnBehalf != onBehalf || orows[0].Context != selected ||
				orows[0].Role != "kn_xa" || orows[0].Why != reason || orows[1].Phase != "completed" || orows[1].Status != 201 || strings.Contains(orows[0].Content, "body-must-not-be-logged") {
				t.Fatalf("unexpected audit rows: %d %+v", os, orows)
			}
		} else if os != 422 || len(orows) != 0 {
			t.Fatalf("missing reason must be rejected before logging: %d %+v", os, orows)
		}
	}
}
