package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// glueFixture dựng cả Application API và Admin API như main, có CORS.
func glueFixture(t *testing.T, origins ...string) (app, admin *httptest.Server, iss *authztest.FakeIssuer) {
	env := pgtest.Get(t)
	iss = authztest.New(t)
	mods, pools, err := buildModules(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{
		Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier(),
		Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods, Pools: pools, CORSOrigins: origins,
	}
	app, admin = httptest.NewServer(NewRouter(d)), httptest.NewServer(NewAdminRouter(d))
	t.Cleanup(app.Close)
	t.Cleanup(admin.Close)
	return app, admin, iss
}

func send(t *testing.T, method, url string, hdr map[string]string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

// Mục 11: web vận hành gọi chéo cả hai bề mặt; preflight không cần token.
func TestCORSOnBothSurfaces(t *testing.T) {
	app, admin, _ := glueFixture(t, "http://localhost:3000")
	// Route cần token trên từng bề mặt (Admin API không có /v1/me).
	protected := map[string]string{app.URL: "/v1/me", admin.URL: "/v1/orgs/" + ids.New().String() + "/events"}
	for _, base := range []string{app.URL, admin.URL} {
		resp, _ := send(t, http.MethodOptions, base+"/v1/consents", map[string]string{
			"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,idempotency-key",
		}, nil)
		if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Fatalf("%s preflight: %d %v", base, resp.StatusCode, resp.Header)
		}
		resp, _ = send(t, http.MethodOptions, base+"/v1/consents", map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "POST"}, nil)
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s origin lạ: %d %v", base, resp.StatusCode, resp.Header)
		}
		// Lỗi 401 vẫn mang header CORS để trình duyệt đọc được problem+json và X-Trace-Id.
		resp, _ = send(t, http.MethodGet, base+protected[base], map[string]string{"Origin": "http://localhost:3000"}, nil)
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" || resp.Header.Get("Access-Control-Expose-Headers") == "" {
			t.Fatalf("%s 401 có CORS: %d %v", base, resp.StatusCode, resp.Header)
		}
	}
}

// Nối đầu-cuối: GET /v1/me trên Application API; entity consent đi qua pf/sync tới Sink của pf/party.
func TestMeAndConsentSyncWired(t *testing.T) {
	app, _, iss := glueFixture(t)
	tid, actor := ids.New(), ids.New()
	tok := iss.Sign(t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{port.RoleConsentWriter, port.RoleConsentReader}})
	auth := map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"}

	resp, me := send(t, http.MethodGet, app.URL+"/v1/me", auth, nil)
	if resp.StatusCode != http.StatusOK || me["actor_ref"] != actor.String() || me["org"] != nil {
		t.Fatalf("/v1/me: %d %v", resp.StatusCode, me)
	}
	if sc, _ := me["scope"].(map[string]any); sc["level"] != "none" {
		t.Fatalf("/v1/me scope: %v", me)
	}

	device := ids.New()
	with := func(extra map[string]string) map[string]string {
		h := map[string]string{"X-Device-Id": device.String()}
		for k, v := range auth {
			h[k] = v
		}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	if resp, b := send(t, http.MethodPost, app.URL+"/v1/devices", with(map[string]string{"Idempotency-Key": "devreg:" + device.String()}),
		map[string]any{"device_id": device.String(), "platform": "android", "app_version": "1.0.0"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("đăng ký thiết bị: %d %v", resp.StatusCode, b)
	}
	recID := ids.New()
	record := map[string]any{
		"id": recID.String(), "idem_key": "dev:" + device.String() + ":1", "entity": "consent", "op": "create",
		"event_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"payload":    map[string]any{"subject_ref": ids.New().String(), "purpose": "mrv.evidence", "text_version": "2026-11", "status": "granted"},
	}
	statusOf := func(batch string) string {
		t.Helper()
		resp, b := send(t, http.MethodPost, app.URL+"/v1/sync/push", with(map[string]string{"Idempotency-Key": "batch:" + batch}),
			map[string]any{"device_id": device.String(), "records": []any{record}})
		res, _ := b["results"].([]any)
		if resp.StatusCode != http.StatusOK || len(res) != 1 {
			t.Fatalf("push: %d %v", resp.StatusCode, b)
		}
		r := res[0].(map[string]any)
		if r["status"] == "rejected" {
			t.Fatalf("consent bị từ chối: %v", r)
		}
		return r["status"].(string)
	}
	if s := statusOf(ids.New().String()); s != "accepted" {
		t.Fatalf("lô đầu phải accepted, nhận %s", s)
	}
	if s := statusOf(ids.New().String()); s != "duplicate" {
		t.Fatalf("lô khác cùng idem_key phải duplicate, nhận %s", s)
	}
	if resp, b := send(t, http.MethodGet, app.URL+"/v1/consents/"+recID.String(), auth, nil); resp.StatusCode != http.StatusOK || b["device_id"] != device.String() {
		t.Fatalf("đọc lại đồng ý đã đồng bộ: %d %v", resp.StatusCode, b)
	}
}

// W1.4 (R-MOD-08): gốc lắp ráp gắn cổng bật module vào route của module per_tenant — tắt pf/sync cho
// tenant thì route của pf/sync trả 403 module_not_enabled, route lõi (/v1/me) vẫn chạy, tenant khác không ảnh hưởng.
func TestModuleGateWired(t *testing.T) {
	app, admin, iss := glueFixture(t)
	tid, other := ids.New(), ids.New()
	adm := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{"platform_admin"}, MFA: true})
	hdr := func(tok, key string) map[string]string {
		h := map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"}
		if key != "" {
			h["Idempotency-Key"] = key
		}
		return h
	}
	device := func(tok string) int {
		d := ids.New().String()
		h := hdr(tok, "devreg:"+d)
		h["X-Device-Id"] = d
		resp, _ := send(t, http.MethodPost, app.URL+"/v1/devices", h, map[string]any{"device_id": d, "platform": "android", "app_version": "1.0.0"})
		return resp.StatusCode
	}
	if s := device(adm); s != http.StatusCreated {
		t.Fatalf("pf/sync bật mặc định: %d", s)
	}
	if resp, b := send(t, http.MethodPost, admin.URL+"/v1/modules/pf/sync/enablement", hdr(adm, "mod-"+ids.New().String()),
		map[string]any{"enabled": false, "reason": "Đơn vị chưa dùng đồng bộ"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("tắt pf/sync: %d %v", resp.StatusCode, b)
	}
	d := ids.New().String()
	h := hdr(adm, "devreg:"+d)
	h["X-Device-Id"] = d
	if resp, b := send(t, http.MethodPost, app.URL+"/v1/devices", h, map[string]any{"device_id": d, "platform": "android", "app_version": "1.0.0"}); resp.StatusCode != http.StatusForbidden || b["code"] != "module_not_enabled" || b["module"] != "pf/sync" {
		t.Fatalf("route pf/sync đã tắt: %d %v", resp.StatusCode, b)
	}
	if resp, b := send(t, http.MethodGet, app.URL+"/v1/me", hdr(adm, ""), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("route lõi không bị chặn: %d %v", resp.StatusCode, b)
	}
	if s := device(iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: other, Roles: []string{"platform_admin"}, MFA: true})); s != http.StatusCreated {
		t.Fatalf("tenant khác: %d", s)
	}
	if resp, b := send(t, http.MethodPost, admin.URL+"/v1/modules/pf/sync/enablement", hdr(adm, "mod-"+ids.New().String()),
		map[string]any{"enabled": true}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("bật lại pf/sync: %d %v", resp.StatusCode, b)
	}
	if s := device(adm); s != http.StatusCreated {
		t.Fatalf("sau khi bật lại: %d", s)
	}
}
