package kn77_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type countedSessionDB struct {
	tenant.Beginner
	calls int
}

func (d *countedSessionDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	d.calls++
	return d.Beginner.BeginTx(ctx, opts)
}

// The real migrated reader must check the bound owner before opening a database
// transaction. A valid owner reaches PostgreSQL even after legacy KN is removed.
func TestKHTCSourceRequiresOwnedSessionThroughHTTP(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	counted := &countedSessionDB{Beginner: pool}
	module := extension.New(counted, clock.System{}, extension.Deps{})
	module.UseKHTCSourceReader(&goldenReader{})
	actor, tid := ids.New(), ids.New()
	makeSession := func(role string) authz.Session {
		s, err := extport.NewSessionContext(extport.SessionContext{ID: ids.New(), Role: role, Department: partyport.DeptKeHoachTaiChinh, OrgRef: ids.New()})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	valid, commune := makeSession(extport.SessionRoleProvince), makeSession(extport.SessionRoleCommune)
	other, err := authz.NewSession("other", ids.New(), authz.SessionScope{})
	if err != nil {
		t.Fatal(err)
	}
	empty := authz.Session{}
	missingPayload := valid.WithPayload(nil)
	principal := func(s *authz.Session, legacyRole string) *authz.Principal {
		return &authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{"platform_admin", "kn_officer"}, MFA: true, KN: &authz.KNContext{Role: legacyRole, Department: partyport.DeptKeHoachTaiChinh}, SelectedSession: s}
	}
	withoutLegacy := principal(&valid, authz.KNXa)
	withoutLegacy.KN = nil
	cases := []struct {
		name                 string
		p                    *authz.Principal
		status, transactions int
	}{
		{"no identity", nil, 401, 0},
		{"legacy admin only", principal(nil, authz.KNAdmin), 428, 0},
		{"zero session", principal(&empty, authz.KNAdmin), 403, 0},
		{"different owner", principal(&other, authz.KNAdmin), 403, 0},
		{"missing owner payload", principal(&missingPayload, authz.KNAdmin), 403, 0},
		{"owner commune overrides legacy admin", principal(&commune, authz.KNAdmin), 403, 0},
		{"owner province overrides legacy commune", principal(&valid, authz.KNXa), 404, 1},
		{"owner province without legacy context", withoutLegacy, 404, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counted.calls = 0
			engine := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
			group := engine.Group("/v1", func(c *gin.Context) {
				ctx := tenant.WithID(c.Request.Context(), tid)
				if tc.p != nil {
					ctx = authz.WithPrincipal(ctx, *tc.p)
				}
				c.Request = c.Request.WithContext(ctx)
			})
			module.RegisterApp(group)
			request := httptest.NewRequest(http.MethodGet, "/v1/extension/khtc/periods/"+ids.New().String()+"/sources", nil)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.status || counted.calls != tc.transactions {
				t.Fatalf("status=%d transactions=%d want %d/%d: %s", response.Code, counted.calls, tc.status, tc.transactions, response.Body.String())
			}
		})
	}
}
