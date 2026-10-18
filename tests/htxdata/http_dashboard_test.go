package htxdata_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
)

type dashboardScope struct{}

func (dashboardScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, _ := authz.FromContext(ctx)
	if p.Has(kn.RoleKNNationalAdmin) {
		return authz.Scope{All: true}, nil
	}
	units := map[uuid.UUID][]string{ref(901): {"89", "89001"}, ref(902): {"92", "92001"}, ref(903): {"89001"}}
	return authz.Scope{Units: units[p.OrgRef]}, nil
}

// The actual module/API uses the same explicit reader injection as production assembly.
// SQL fixture writes belong to this composition test, outside the KN source boundary.
func TestKNHTTPDashboardPublishedHTXData(t *testing.T) {
	f := newFixture(t)
	empty := ids.New()
	for _, tid := range []uuid.UUID{f.tid, empty} {
		for _, u := range []struct{ code, level, parent string }{{"89", "province", ""}, {"92", "province", ""}, {"89001", "commune", "89"}, {"92001", "commune", "92"}} {
			f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,name,level,parent_code,source,actor_ref)
    VALUES($1,$2,'2020-01-01',$2,$3,NULLIF($4,''),'published-dashboard-fixture',$5)`, tid, u.code, u.level, u.parent, ref(900))
		}
	}
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC))
	md := masterdata.New(pool, clk)
	reader := extension.NewPublishedHTXData(pool)
	mod := extension.New(pool, clk, extension.Deps{Scope: dashboardScope{}, Units: md.AdminUnits(), Plots: reader, Coops: reader})
	mod.UseHTXConfirmations(reader)
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterApp(engine.Group("/v1", httpx.Authenticate(issuer.Verifier())))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	request := func(tid, node uuid.UUID, role, path string) (int, map[string]any) {
		t.Helper()
		token := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, OrgRef: node, Roles: []string{role}, TTL: time.Hour})
		req, e := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var body map[string]any
		if e := json.NewDecoder(res.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, body
	}
	check := func(tid, node uuid.UUID, role, path string, coops, households, plots int, area string) {
		t.Helper()
		status, body := request(tid, node, role, path)
		if status != http.StatusOK {
			t.Fatalf("dashboard %d: %v", status, body)
		}
		b := body["total"].(map[string]any)
		if b["cooperatives"] != float64(coops) || b["households"] != float64(households) || b["digitized_plots"] != float64(plots) || b["digitized_area_ha"] != area {
			t.Fatalf("published dashboard total: %v", b)
		}
	}
	check(f.tid, uuid.Nil, kn.RoleKNNationalAdmin, "/v1/dashboard?period=2026-10", 3, 3, 4, "6.5680")
	check(f.tid, ref(901), kn.RoleKNProvinceAdmin, "/v1/dashboard?period=2026-10&unit=89", 2, 3, 3, "6.5679")
	check(f.tid, ref(902), kn.RoleKNProvinceAdmin, "/v1/dashboard?period=2026-10&unit=92", 1, 0, 1, "0.0001")
	check(f.tid, ref(903), kn.RoleKNOfficer, "/v1/dashboard?period=2026-10&unit=89001", 2, 3, 3, "6.5679")
	status, body := request(f.tid, ref(902), kn.RoleKNProvinceAdmin, "/v1/dashboard?unit=89")
	if status != http.StatusForbidden {
		t.Fatalf("outside province: %d %v", status, body)
	}
	check(empty, uuid.Nil, kn.RoleKNNationalAdmin, "/v1/dashboard?period=2026-10", 0, 0, 0, "0.0000")
}
