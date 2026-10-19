package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Read the deployed representation: absent false values must not silently become
// enabled grants, and a browser override must not bypass the realm's D24 flow.
func TestKNWebClientCodePKCEAndClaims_KNSPLIT3(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm struct {
		Clients []struct {
			ClientID        string            `json:"clientId"`
			Protocol        string            `json:"protocol"`
			Enabled         bool              `json:"enabled"`
			PublicClient    bool              `json:"publicClient"`
			BearerOnly      bool              `json:"bearerOnly"`
			StandardFlow    bool              `json:"standardFlowEnabled"`
			ImplicitFlow    *bool             `json:"implicitFlowEnabled"`
			DirectGrants    *bool             `json:"directAccessGrantsEnabled"`
			ServiceAccounts *bool             `json:"serviceAccountsEnabled"`
			Secret          string            `json:"secret"`
			RedirectURIs    []string          `json:"redirectUris"`
			WebOrigins      []string          `json:"webOrigins"`
			DefaultScopes   []string          `json:"defaultClientScopes"`
			OptionalScopes  []string          `json:"optionalClientScopes"`
			Attributes      map[string]string `json:"attributes"`
			FlowOverrides   map[string]string `json:"authenticationFlowBindingOverrides"`
			ProtocolMappers []json.RawMessage `json:"protocolMappers"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range realm.Clients {
		if c.ClientID != "kn-web" {
			continue
		}
		count++
		if c.Protocol != "openid-connect" || !c.Enabled || !c.PublicClient || c.BearerOnly || !c.StandardFlow {
			t.Error("kn-web must be an enabled public OIDC authorization-code client")
		}
		for name, enabled := range map[string]*bool{"implicit": c.ImplicitFlow, "direct grant": c.DirectGrants, "service account": c.ServiceAccounts} {
			if enabled == nil || *enabled {
				t.Errorf("kn-web %s must be explicitly disabled", name)
			}
		}
		if c.Secret != "" || len(c.FlowOverrides) != 0 || len(c.ProtocolMappers) != 0 {
			t.Error("kn-web must inherit realm authentication and shared claim mappers without a secret or overrides")
		}
		if !slices.Equal(c.RedirectURIs, []string{"http://localhost:3000/*", "http://127.0.0.1:3000/*"}) ||
			!slices.Equal(c.WebOrigins, []string{"http://localhost:3000", "http://127.0.0.1:3000"}) {
			t.Errorf("kn-web development callbacks/origins broadened: %v / %v", c.RedirectURIs, c.WebOrigins)
		}
		for key, want := range map[string]string{
			"pkce.code.challenge.method":  "S256",
			"post.logout.redirect.uris":   "http://localhost:3000/*##http://127.0.0.1:3000/*",
			"client.session.idle.timeout": "14400",
			"client.session.max.lifespan": "36000",
		} {
			if c.Attributes[key] != want {
				t.Errorf("kn-web %s=%q, want %q", key, c.Attributes[key], want)
			}
		}
		wantScopes := []string{"basic", "roles", "web-origins", "agrigreen-claims"}
		if !slices.Equal(c.DefaultScopes, wantScopes) || len(c.OptionalScopes) != 0 {
			t.Errorf("kn-web scopes=%v optional=%v, need shared subject/roles/claims without offline access", c.DefaultScopes, c.OptionalScopes)
		}
		// Follow only scopes actually assigned to this client. Global mappers in an
		// unrelated scope do not prove that kn-web receives API identity claims.
		claims := map[string]string{}
		for _, s := range loadRealm(t, "agrigreen-realm.json").ClientScopes {
			if !slices.Contains(c.DefaultScopes, s.Name) {
				continue
			}
			for _, m := range s.ProtocolMappers {
				claim := m.Config["claim.name"]
				if m.ProtocolMapper == "oidc-audience-mapper" {
					claim = "aud:" + m.Config["included.client.audience"]
				}
				if claim == "" {
					claim = m.ProtocolMapper
				}
				if m.Config["access.token.claim"] != "true" {
					t.Errorf("assigned mapper %s does not emit an access-token claim", m.Name)
				}
				claims[claim] = m.ProtocolMapper
			}
		}
		for claim, want := range map[string]string{
			"tenant_id": "oidc-usermodel-attribute-mapper", "org_ref": "oidc-usermodel-attribute-mapper",
			"realm_access.roles": "oidc-usermodel-realm-role-mapper", "aud:agrigreen-api": "oidc-audience-mapper",
			"oidc-amr-mapper": "oidc-amr-mapper", "oidc-sub-mapper": "oidc-sub-mapper",
		} {
			if claims[claim] != want {
				t.Errorf("kn-web missing %s via assigned scopes (got %q)", claim, claims[claim])
			}
		}
	}
	if count != 1 {
		t.Fatalf("need exactly one kn-web client, found %d", count)
	}
}

func TestKNWebClientInheritsLoginTOTP_KNSPLIT3(t *testing.T) {
	realm := loadRealm(t, "agrigreen-realm.json")
	if realm.BrowserFlow != "agrigreen browser" {
		t.Fatalf("kn-web must inherit the deployed D24 browser flow, got %q", realm.BrowserFlow)
	}
	cfg := map[string]map[string]string{}
	for _, c := range realm.AuthenticatorConfig {
		cfg[c.Alias] = c.Config
	}
	byRole := false
	for _, gate := range otpGates(t, flowsOf(realm), realm.BrowserFlow, map[string]bool{}) {
		for _, e := range gate {
			if e.Authenticator == "conditional-user-role" && e.Requirement == "REQUIRED" &&
				cfg[e.AuthenticatorConfig]["condUserRole"] == "login_totp_required" && cfg[e.AuthenticatorConfig]["negate"] != "true" {
				byRole = true
			}
		}
	}
	if !byRole {
		t.Fatal("inherited browser flow has no required OTP gate for KN login roles")
	}
	for _, role := range []string{"kn_user", "kn_officer", "kn_province_admin", "kn_national_admin"} {
		found := false
		for _, r := range realm.Roles.Realm {
			if r.Name == role {
				found = r.Composite && slices.Contains(r.Composites.Realm, "login_totp_required")
			}
		}
		if !found {
			t.Errorf("KN login role %s must retain required TOTP", role)
		}
	}
	// Keep D9's shared SSO ceiling; adding a client must not stretch other clients.
	if realm.SSOSessionIdleTimeout != 14400 || realm.SSOSessionMaxLifespan != 86400 ||
		!strings.Contains(realm.PasswordPolicy, "length(12)") {
		t.Error("shared realm session/password policy changed with kn-web")
	}
}
