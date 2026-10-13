package authz_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// SMS OTP chỉ được tính là yếu tố thứ hai cho vai thường; vai quản trị và vai kho định danh
// đọc/xóa dữ liệu cá nhân cần yếu tố mạnh (otp/hwk/swk/mfa).
func TestMFAPolicySMSExcludedForSensitiveRoles(t *testing.T) {
	iss := authztest.New(t)
	cases := []struct {
		name  string
		roles []string
		amr   []string
		want  bool
	}{
		{"vai thường + sms", []string{"kn_officer"}, []string{"pwd", "sms"}, true},
		{"vai thường chỉ mật khẩu", []string{"kn_officer"}, []string{"pwd"}, false},
		{"platform_admin + sms", []string{authz.RolePlatformAdmin}, []string{"pwd", "sms"}, false},
		{"platform_admin + otp", []string{authz.RolePlatformAdmin}, []string{"pwd", "otp"}, true},
		{"vault_registrar + sms", []string{"kn_officer", "vault_registrar"}, []string{"pwd", "SMS"}, false},
		{"vault_eraser + hwk", []string{"vault_eraser"}, []string{"pwd", "hwk"}, true},
	}
	v := iss.Verifier()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := v.Verify(context.Background(), iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: c.roles, AMR: c.amr}))
			if err != nil {
				t.Fatal(err)
			}
			if p.MFA != c.want {
				t.Fatalf("MFA = %v, mong %v", p.MFA, c.want)
			}
		})
	}
}

// AG_MFA_DISABLED chỉ dành cho demo: bật thì mọi phiên được tính là đã qua yếu tố thứ hai; production từ chối.
func TestMFAPolicyDisabledDemoOnly(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := authz.MFAPolicyFromEnv(env(map[string]string{authz.EnvMFADisabled: "1", "AG_ENV": "production"})); err == nil {
		t.Fatal("production không được bật AG_MFA_DISABLED")
	}
	p, err := authz.MFAPolicyFromEnv(env(map[string]string{authz.EnvMFADisabled: "1", "AG_ENV": "staging"}))
	if err != nil || !p.Disabled {
		t.Fatalf("staging bật được: %+v %v", p, err)
	}
	iss := authztest.New(t)
	pr, err := iss.Verifier().WithMFAPolicy(p).Verify(context.Background(),
		iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, AMR: []string{"pwd"}}))
	if err != nil || !pr.MFA {
		t.Fatalf("MFA = %v, %v; mong true khi tắt", pr.MFA, err)
	}
	if p, _ = authz.MFAPolicyFromEnv(env(nil)); p.Disabled {
		t.Fatal("mặc định phải bật MFA")
	}
}

func TestMFAPolicyFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	p, err := authz.MFAPolicyFromEnv(env(nil))
	if err != nil || len(p.StrongFactors) == 0 {
		t.Fatalf("mặc định: %+v %v", p, err)
	}
	if _, err := authz.MFAPolicyFromEnv(env(map[string]string{authz.EnvMFAStrongFactors: " , "})); err == nil {
		t.Fatal("danh sách đặt nhưng rỗng phải lỗi")
	}
	p, err = authz.MFAPolicyFromEnv(env(map[string]string{authz.EnvMFAStrongRoles: "kn_admin", authz.EnvMFAStrongFactors: "HWK"}))
	if err != nil {
		t.Fatal(err)
	}
	iss := authztest.New(t)
	v := iss.Verifier().WithMFAPolicy(p)
	tok := func(roles, amr []string) bool {
		pr, err := v.Verify(context.Background(), iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: roles, AMR: amr}))
		if err != nil {
			t.Fatal(err)
		}
		return pr.MFA
	}
	if tok([]string{"kn_admin"}, []string{"pwd", "otp"}) {
		t.Fatal("kn_admin chỉ nhận hwk theo cấu hình")
	}
	if !tok([]string{"kn_admin"}, []string{"pwd", "hwk"}) {
		t.Fatal("kn_admin + hwk phải đạt")
	}
	if !tok([]string{authz.RolePlatformAdmin}, []string{"pwd", "sms"}) {
		t.Fatal("platform_admin không còn trong vai nhạy cảm theo cấu hình → sms được tính")
	}
}
