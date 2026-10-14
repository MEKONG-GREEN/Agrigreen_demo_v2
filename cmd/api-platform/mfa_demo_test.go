package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// AG_MFA_DISABLED bật ngoài máy demo → run() trả lỗi TRƯỚC khi mở DB (tiến trình từ chối khởi động). Máy demo
// được nhận diện bằng AG_DEPLOY_ENV=demo; AG_ENV=production luôn bị từ chối.
func TestStartupRefusesMFADisabledOutsideDemo(t *testing.T) {
	cases := []struct{ name, env, deploy string }{
		{"production, deploy=demo", "production", "demo"},
		{"staging, không đánh dấu demo", "staging", ""},
		{"staging, deploy=staging", "staging", "staging"},
		{"local, không đánh dấu demo", "local", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AG_ENV", c.env)
			t.Setenv(authz.EnvDeployEnv, c.deploy)
			t.Setenv(authz.EnvMFADisabled, "1")
			// Cố ý để trống cấu hình DB/OIDC: nếu guard không chặn, run() sẽ lỗi ở bước sau với thông điệp khác.
			t.Setenv("AG_PF_DB_URL", "")
			t.Setenv("AG_OIDC_ISSUER", "")
			err := run(telemetry.NewLogger("test", io.Discard))
			if c.env == "production" && err != nil && !errors.Is(err, authz.ErrMFABypassNotDemo) {
				// residency.CheckEnv (RB-7) chạy trước và từ chối production thiếu allowlist — vẫn là từ chối khởi động;
				// guard MFA được kiểm trực tiếp để không phụ thuộc thứ tự đó.
				if _, perr := mfaPolicyFromEnv(func(k string) string {
					return map[string]string{"AG_ENV": c.env, authz.EnvDeployEnv: c.deploy, authz.EnvMFADisabled: "1"}[k]
				}, telemetry.NewLogger("test", io.Discard)); !errors.Is(perr, authz.ErrMFABypassNotDemo) {
					t.Fatalf("mfaPolicyFromEnv(production) = %v, mong ErrMFABypassNotDemo", perr)
				}
				return
			}
			if !errors.Is(err, authz.ErrMFABypassNotDemo) {
				t.Fatalf("run() = %v, mong ErrMFABypassNotDemo", err)
			}
		})
	}
	// Máy demo hợp lệ: guard cho qua, run() dừng ở bước kế tiếp (thiếu cấu hình), không phải vì cờ MFA.
	t.Setenv("AG_ENV", "staging")
	t.Setenv(authz.EnvDeployEnv, "demo")
	t.Setenv(authz.EnvMFADisabled, "1")
	t.Setenv("AG_PF_DB_URL", "")
	if err := run(telemetry.NewLogger("test", io.Discard)); err == nil || errors.Is(err, authz.ErrMFABypassNotDemo) {
		t.Fatalf("demo: run() = %v, mong lỗi thiếu cấu hình, không phải lỗi cờ MFA", err)
	}
}

// Admin API thật (NewAdminRouter + module thật): cờ TẮT (mặc định, mọi môi trường không phải demo) → không token
// 401, platform_admin chỉ mật khẩu 403 mfa_required, có otp thì qua cổng MFA. Cờ BẬT trên máy demo → chỉ mật khẩu
// cũng qua cổng MFA. Đây là hành vi D24 phải giữ trên dev.
func TestAdminAPIMFAGate_SyncMain(t *testing.T) {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	mods, pools, err := buildModules(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	admin := func(t *testing.T, vars map[string]string) *httptest.Server {
		policy, err := mfaPolicyFromEnv(func(k string) string { return vars[k] }, telemetry.NewLogger("test", io.Discard))
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(NewAdminRouter(Deps{
			Log: telemetry.NewLogger("test", io.Discard), Verifier: iss.Verifier().WithMFAPolicy(policy),
			Ops: env.Pool(t, migrations.PF, dbadmin.AppPlatform), Modules: mods, Pools: pools,
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	tid := ids.New()
	pwdOnly := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{authz.RolePlatformAdmin}, AMR: []string{"pwd"}})
	withOTP := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{authz.RolePlatformAdmin}, AMR: []string{"pwd", "otp"}})
	get := func(t *testing.T, srv *httptest.Server, token string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/users", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body.Code
	}
	mfaGate := func(status int, code string) bool {
		return status == http.StatusUnauthorized || (status == http.StatusForbidden && strings.Contains(code, "mfa_required"))
	}

	t.Run("cờ tắt", func(t *testing.T) {
		for _, vars := range []map[string]string{
			nil,
			{"AG_ENV": "production"},
			{"AG_ENV": "staging", authz.EnvDeployEnv: "demo", authz.EnvMFADisabled: "0"},
		} {
			srv := admin(t, vars)
			if st, _ := get(t, srv, ""); st != http.StatusUnauthorized {
				t.Fatalf("%v: không token → %d, mong 401", vars, st)
			}
			if st, code := get(t, srv, pwdOnly); st != http.StatusForbidden || !strings.Contains(code, "mfa_required") {
				t.Fatalf("%v: platform_admin chỉ mật khẩu → %d %q, mong 403 mfa_required", vars, st, code)
			}
			if st, code := get(t, srv, withOTP); mfaGate(st, code) {
				t.Fatalf("%v: có otp vẫn bị cổng MFA chặn: %d %q", vars, st, code)
			}
		}
	})
	t.Run("máy demo bật cờ", func(t *testing.T) {
		srv := admin(t, map[string]string{"AG_ENV": "staging", authz.EnvDeployEnv: "demo", authz.EnvMFADisabled: "1"})
		if st, _ := get(t, srv, ""); st != http.StatusUnauthorized {
			t.Fatalf("không token vẫn phải 401, được %d", st)
		}
		if st, code := get(t, srv, pwdOnly); mfaGate(st, code) {
			t.Fatalf("demo: chỉ mật khẩu bị cổng MFA chặn: %d %q", st, code)
		}
	})
}
