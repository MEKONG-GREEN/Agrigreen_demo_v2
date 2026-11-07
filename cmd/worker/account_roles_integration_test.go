package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestWorkerAccountsUsesOwnerRoleOrder(t *testing.T) {
	// A direct native-only dependency must not replace the worker composition.
	m, err := workerAccounts(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppWorker), clock.System{}, accounts.Deps{Roles: accport.NativeRoleCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	partial := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: ids.New(),
		KN: &authz.KNContext{Role: authz.KNAdmin}})
	if _, err := m.KNPeople().PeopleInScope(partial); err == nil || problem.From(err).Code != problem.Forbidden.Code {
		t.Fatalf("worker bypassed the configured session owner: %v", err)
	}
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	m.RegisterAdmin(engine.Group("/v1", httpx.Authenticate(issuer.Verifier()), httpx.RequireMFA()))
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	token := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{accport.RoleAccountAdminProvince}, MFA: true})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/grants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Grantable []string `json:"grantable"`
		MFA       []string `json:"mfa_roles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || !slices.Equal(body.Grantable, []string{"kn_officer", "coop_manager"}) || !slices.Equal(body.MFA, accport.MFARoles) {
		t.Fatalf("worker catalog diverged from API: %d %+v", res.StatusCode, body)
	}
}
