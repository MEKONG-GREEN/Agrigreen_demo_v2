package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0033 §7.2 trên router THẬT: mọi route /v1/partner/* đã quyết định kế nhiệm (assembly.PartnerSuccessors) và
// phản hồi thật (kể cả 403 khi thiếu cấp quyền) mang Deprecation + Sunset + Link.
func TestPartnerLegacyRoutes_ADR0033(t *testing.T) {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mods, pools, err := buildModules(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := assembly.PartnerLegacyFromEnv(func(k string) string {
		return map[string]string{assembly.EnvPartnerSunset: "2027-03-31", assembly.EnvPartnerSuccessorBase: "https://partner.example.vn"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRouter(Deps{
		Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier(),
		Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods, Pools: pools, PartnerLegacy: legacy,
	})
	n := 0
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, "/v1/partner/") {
			continue
		}
		n++
		if _, ok := assembly.PartnerSuccessors[rt.Path]; !ok {
			t.Errorf("route %s %s chưa có trong assembly.PartnerSuccessors (quyết định feed/lô kế nhiệm hoặc \"\")", rt.Method, rt.Path)
		}
	}
	if n == 0 {
		t.Fatal("không thấy route /v1/partner/* nào — kiểm lại cách đăng ký")
	}

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/partner/cooperatives", nil)
	req.Header.Set("Authorization", "Bearer "+iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New()}))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Deprecation") == "" || resp.Header.Get("Sunset") != "Wed, 31 Mar 2027 00:00:00 GMT" ||
		resp.Header.Get("Link") != `<https://partner.example.vn/v1/feeds/cooperative_directory>; rel="successor-version"` {
		t.Fatalf("phản hồi %d thiếu header ngừng dùng: %v", resp.StatusCode, resp.Header)
	}
}
