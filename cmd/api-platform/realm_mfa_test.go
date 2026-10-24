package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func loadRealm(t *testing.T, file string) realmFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", file))
	if err != nil {
		t.Fatal(err)
	}
	var realm realmFile
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return realm
}

type flowExec = struct {
	Authenticator       string `json:"authenticator"`
	AuthenticatorConfig string `json:"authenticatorConfig"`
	FlowAlias           string `json:"flowAlias"`
	Requirement         string `json:"requirement"`
	Priority            int    `json:"priority"`
}

func flowsOf(realm realmFile) map[string][]flowExec {
	out := map[string][]flowExec{}
	for _, f := range realm.AuthenticationFlows {
		out[f.Alias] = f.Executions
	}
	return out
}

// otpGates trả các nhánh (subflow) trong flow có OTP Form REQUIRED, kèm điều kiện của nhánh — đi đệ quy từ flow gốc.
func otpGates(t *testing.T, flows map[string][]flowExec, alias string, seen map[string]bool) [][]flowExec {
	t.Helper()
	if seen[alias] {
		t.Fatalf("flow %q lặp vòng", alias)
	}
	seen[alias] = true
	execs, ok := flows[alias]
	if !ok {
		t.Fatalf("thiếu flow %q", alias)
	}
	var out [][]flowExec
	if slices.ContainsFunc(execs, func(e flowExec) bool { return e.Authenticator == "auth-otp-form" && e.Requirement == "REQUIRED" }) {
		out = append(out, execs)
	}
	for _, e := range execs {
		if e.FlowAlias != "" {
			out = append(out, otpGates(t, flows, e.FlowAlias, seen)...)
		}
	}
	return out
}

// D24 (CEO 13/10/2026, QĐ-4 c): TOTP bắt buộc khi đăng nhập cho cán bộ KN và vai [MFA] từ G1, đăng ký ở lần đăng
// nhập đầu. Realm: các vai trong accounts/port.LoginTOTPRoles — và CHỈ các vai đó — là vai tổng hợp chứa vai đánh
// dấu login_totp_required; browserFlow có nhánh CONDITIONAL "Condition - user role = login_totp_required" → OTP Form
// REQUIRED (Keycloak tự thêm CONFIGURE_TOTP khi chưa có). Vai nông hộ/HTX không bị bắt.
func TestRealmLoginTOTPByRole_D24(t *testing.T) {
	realm := loadRealm(t, "agrigreen-realm.json")
	var marked []string
	markerExists := false
	for _, r := range realm.Roles.Realm {
		if r.Name == accport.RoleLoginTOTP {
			markerExists = true
			if r.Composite {
				t.Errorf("vai đánh dấu %s không được là vai tổng hợp", r.Name)
			}
		}
		if slices.Contains(r.Composites.Realm, accport.RoleLoginTOTP) {
			if !r.Composite {
				t.Errorf("vai %s có composites nhưng composite=false", r.Name)
			}
			marked = append(marked, r.Name)
		}
	}
	if !markerExists {
		t.Fatalf("realm thiếu vai đánh dấu %s", accport.RoleLoginTOTP)
	}
	want := slices.Clone(accport.LoginTOTPRoles)
	slices.Sort(want)
	slices.Sort(marked)
	if !slices.Equal(marked, want) {
		t.Errorf("vai chứa %s trong realm %v ≠ accounts/port.LoginTOTPRoles %v", accport.RoleLoginTOTP, marked, want)
	}
	for _, r := range []string{"kn_user", "kn_officer", "kn_province_admin", "kn_national_admin"} {
		if !accport.NeedsLoginTOTP([]string{r}) {
			t.Errorf("D24: cán bộ KN %s phải dùng TOTP", r)
		}
	}
	for _, r := range []string{"farmer", "coop_manager", "mkg_sync", "map_consumer"} {
		if accport.NeedsLoginTOTP([]string{r}) {
			t.Errorf("vai %s không thuộc D24 (nông hộ, HTX, tài khoản dịch vụ)", r)
		}
	}

	if realm.BrowserFlow == "" {
		t.Fatal("D24: realm phải đặt browserFlow là luồng có TOTP theo vai")
	}
	cfg := map[string]map[string]string{}
	for _, c := range realm.AuthenticatorConfig {
		cfg[c.Alias] = c.Config
	}
	flows := flowsOf(realm)
	for _, f := range realm.AuthenticationFlows {
		if f.Alias == realm.BrowserFlow && !f.TopLevel {
			t.Errorf("browserFlow %q phải là flow gốc (topLevel)", f.Alias)
		}
	}
	gates := otpGates(t, flows, realm.BrowserFlow, map[string]bool{})
	byRole := 0
	for _, g := range gates {
		for _, e := range g {
			if e.Authenticator != "conditional-user-role" {
				continue
			}
			c := cfg[e.AuthenticatorConfig]
			if c == nil {
				t.Errorf("điều kiện vai thiếu authenticatorConfig %q", e.AuthenticatorConfig)
				continue
			}
			if c["condUserRole"] == accport.RoleLoginTOTP && c["negate"] != "true" {
				byRole++
			}
		}
	}
	if byRole != 1 {
		t.Errorf("D24: cần đúng một nhánh OTP REQUIRED với điều kiện vai %s (không phủ định), có %d", accport.RoleLoginTOTP, byRole)
	}
	// Nhánh theo vai phải được gọi CONDITIONAL từ bước form, sau mật khẩu.
	conditional := false
	for _, execs := range flows {
		for _, e := range execs {
			if e.FlowAlias != "" && e.Requirement == "CONDITIONAL" {
				for _, x := range flows[e.FlowAlias] {
					if x.Authenticator == "conditional-user-role" && cfg[x.AuthenticatorConfig]["condUserRole"] == accport.RoleLoginTOTP && cfg[x.AuthenticatorConfig]["negate"] != "true" {
						conditional = true
					}
				}
			}
		}
	}
	if !conditional {
		t.Error("D24: nhánh TOTP theo vai phải có requirement CONDITIONAL")
	}
	// Không đụng chính sách mật khẩu của realm cán bộ (D24: 14 ký tự chỉ ở realm quản trị riêng).
	if realm.PasswordPolicy != "length(12) and notUsername and passwordHistory(5)" {
		t.Errorf("D24: realm agrigreen giữ mật khẩu 12 ký tự/không trùng 5 (D9), có %q", realm.PasswordPolicy)
	}
}

// D24 + D25: realm quản trị RIÊNG của hệ KN — mật khẩu ≥ 14 ký tự có độ phức tạp, không trùng 10 lần gần nhất, đổi 60
// ngày; TOTP bắt buộc mọi tài khoản (OTP Form REQUIRED không điều kiện); chống dò như realm chính; phiên ngắn.
func TestAdminRealmPasswordPolicy_D24(t *testing.T) {
	realm := loadRealm(t, "agrigreen-kn-admin-realm.json")
	for _, p := range []string{"length(14)", "upperCase(1)", "lowerCase(1)", "digits(1)", "specialChars(1)", "notUsername",
		"passwordHistory(10)", "forceExpiredPasswordChange(60)"} {
		if !strings.Contains(realm.PasswordPolicy, p) {
			t.Errorf("realm quản trị: chính sách mật khẩu thiếu %s (%q)", p, realm.PasswordPolicy)
		}
	}
	if realm.RegistrationAllowed || !realm.BruteForceProtected || realm.FailureFactor != 5 || realm.PermanentLockout || realm.MaxTemporaryLockouts != 0 || !realm.ResetPasswordAllowed {
		t.Errorf("realm quản trị: chống dò/đăng ký: đăng ký=%v chống dò=%v sai=%d khóa hẳn=%v khóa tạm=%d",
			realm.RegistrationAllowed, realm.BruteForceProtected, realm.FailureFactor, realm.PermanentLockout, realm.MaxTemporaryLockouts)
	}
	if realm.SSOSessionIdleTimeout <= 0 || realm.SSOSessionIdleTimeout > 900 || realm.SSOSessionMaxLifespan > 28800 {
		t.Errorf("realm quản trị: phiên quản trị phải ngắn (≤ 15 phút không thao tác, ≤ 8 giờ): idle=%d max=%d",
			realm.SSOSessionIdleTimeout, realm.SSOSessionMaxLifespan)
	}
	flows := flowsOf(realm)
	gates := otpGates(t, flows, realm.BrowserFlow, map[string]bool{})
	unconditional := false
	for _, g := range gates {
		if !slices.ContainsFunc(g, func(e flowExec) bool { return strings.HasPrefix(e.Authenticator, "conditional-") }) {
			unconditional = true
		}
	}
	if !unconditional {
		t.Error("realm quản trị: OTP Form phải REQUIRED không điều kiện trong browserFlow")
	}
	// Không có client ROPC (direct access grants) trong realm quản trị.
	for _, c := range realm.Clients {
		if strings.Contains(c.ClientID, "seed") {
			t.Errorf("realm quản trị không được có client chỉ-dev %s", c.ClientID)
		}
	}
	main := loadRealm(t, "agrigreen-realm.json")
	if main.PasswordPolicy == realm.PasswordPolicy {
		t.Error("realm quản trị phải có chính sách mật khẩu riêng")
	}
}
