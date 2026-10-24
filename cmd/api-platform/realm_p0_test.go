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
