package main

import (
	"slices"
	"testing"
)

// Importing a realm replaces the built-in scopes. Web requests profile; MFA
// claim values must be derived from completed executions rather than a constant.
func TestRealmP0ScopesAndAuthenticatorReferences(t *testing.T) {
	for _, name := range []string{"agrigreen-realm.json", "agrigreen-kn-admin-realm.json"} {
		realm := loadRealm(t, name)
		scopes := map[string]bool{}
		for _, scope := range realm.ClientScopes {
			scopes[scope.Name] = true
		}
		if !scopes["profile"] {
			t.Errorf("%s: imported realm omits requested profile scope", name)
		}
		configs := map[string]map[string]string{}
		for _, cfg := range realm.AuthenticatorConfig {
			configs[cfg.Alias] = cfg.Config
		}
		passwords, otps := 0, 0
		for _, flow := range realm.AuthenticationFlows {
			for _, e := range flow.Executions {
				want := ""
				switch e.Authenticator {
				case "auth-username-password-form", "direct-grant-validate-password":
					want = "pwd"
					passwords++
				case "auth-otp-form", "direct-grant-validate-otp":
					want = "otp"
					otps++
				}
				if want != "" {
					cfg := configs[e.AuthenticatorConfig]
					if cfg["default.reference.value"] != want || cfg["default.reference.maxAge"] != "86400" {
						t.Errorf("%s %s: real execution lacks %s AMR reference", name, flow.Alias, want)
					}
				}
			}
		}
		if passwords == 0 || otps == 0 {
			t.Fatalf("%s: no password/OTP execution inspected", name)
		}
		for _, client := range realm.Clients {
			if slices.Contains([]string{"agrigreen-web-ops", "kn-web", "kn-admin-web"}, client.ClientID) && !slices.Contains(client.DefaultClientScopes, "profile") {
				t.Errorf("%s: %s cannot request profile", name, client.ClientID)
			}
		}
	}
}

// Recovery is a login path: email alone must not bypass the browser's MFA.
// Configured OTP is preserved and checked before resetting the password. Missing
// required OTP reaches CONFIGURE_TOTP, never the optional configured-only branch.
func TestRealmP0PasswordRecoveryKeepsOTP(t *testing.T) {
	for _, name := range []string{"agrigreen-realm.json", "agrigreen-kn-admin-realm.json"} {
		t.Run(name, func(t *testing.T) {
			realm := loadRealm(t, name)
			if realm.ResetCredentialsFlow != "agrigreen password recovery" {
				t.Fatal("unexpected recovery binding")
			}
			flows := flowsOf(realm)
			root := flows[realm.ResetCredentialsFlow]
			want := []string{"reset-credentials-choose-user", "reset-credential-email", "agrigreen recovery role otp", "agrigreen recovery configured otp", "reset-password"}
			if name == "agrigreen-kn-admin-realm.json" {
				want = []string{"reset-credentials-choose-user", "reset-credential-email", "auth-otp-form", "reset-password"}
			}
			if len(root) != len(want) {
				t.Fatalf("recovery steps: %+v", root)
			}
			previous := 0
			for i, e := range root {
				id := e.Authenticator
				if e.FlowAlias != "" {
					id = e.FlowAlias
				}
				if id != want[i] || e.Priority <= previous {
					t.Errorf("recovery order[%d]: %+v", i, e)
				}
				previous = e.Priority
				requirement := "REQUIRED"
				if e.FlowAlias != "" {
					requirement = "CONDITIONAL"
				}
				if e.Requirement != requirement {
					t.Errorf("unsafe recovery requirement: %+v", e)
				}
				if e.Authenticator == "auth-otp-form" && e.AuthenticatorConfig != "agrigreen-amr-otp" {
					t.Error("OTP lacks execution AMR")
				}
			}
			gates := otpGates(t, flows, realm.ResetCredentialsFlow, map[string]bool{})
			if len(gates) == 0 {
				t.Fatal("recovery has no required OTP gate")
			}
			for _, gate := range gates {
				for _, e := range gate {
					if e.Authenticator == "reset-otp" {
						t.Fatal("recovery deletes OTP")
					}
				}
			}
			if name == "agrigreen-realm.json" {
				role := flows["agrigreen recovery role otp"]
				optional := flows["agrigreen recovery configured otp"]
				if len(role) != 2 || role[0].Authenticator != "conditional-user-role" || role[0].AuthenticatorConfig != "agrigreen-login-totp-role" || role[1].Authenticator != "auth-otp-form" {
					t.Fatalf("missing required-role OTP gate: %+v", role)
				}
				if len(optional) != 3 || optional[0].AuthenticatorConfig != "agrigreen-not-login-totp-role" || optional[1].Authenticator != "conditional-user-configured" || optional[2].Authenticator != "auth-otp-form" {
					t.Fatalf("configured OTP gate must exclude required roles: %+v", optional)
				}
				for _, gate := range [][]flowExec{role, optional} {
					for _, e := range gate {
						if e.Requirement != "REQUIRED" {
							t.Errorf("optional MFA gate execution: %+v", e)
						}
					}
				}
			}
		})
	}
}
