package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// KN-STANDALONE-10B-PROFILE-CI (PLAN-KN-STANDALONE Q8): realm riêng `kn` của profile Compose chỉ-KN
// (deploy/compose/keycloak-kn/kn-realm.json). Client đăng nhập chỉ có kn-web và kn-field; không vai, client hay tài
// khoản dịch vụ của HTX/MKG/CGH/MRV/Public API; trang "Quên mật khẩu" gốc của Keycloak tắt (ADR-0057 mới có giới hạn
// tần suất); luồng đăng nhập D24, phiên và claim giữ đúng như realm agrigreen.
func TestKNStandaloneRealm(t *testing.T) {
	read := func(parts ...string) map[string]any {
		raw, err := os.ReadFile(filepath.Join(append([]string{repoRoot, "deploy", "compose"}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	kn, main := read("keycloak-kn", "kn-realm.json"), read("keycloak", "agrigreen-realm.json")
	named := func(list any, key string) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, v := range list.([]any) {
			m := v.(map[string]any)
			out[m[key].(string)] = m
		}
		return out
	}

	if kn["realm"] != "kn" || kn["resetPasswordAllowed"] != false || kn["registrationAllowed"] != false {
		t.Errorf("realm=%v resetPasswordAllowed=%v registrationAllowed=%v: cần realm kn, tắt quên mật khẩu gốc và tự đăng ký",
			kn["realm"], kn["resetPasswordAllowed"], kn["registrationAllowed"])
	}
	if _, ok := kn["resetCredentialsFlow"]; ok {
		t.Error("realm kn không khai luồng reset credentials riêng (trang gốc đã tắt)")
	}

	// Client: đúng bốn; chỉ kn-web và kn-field đăng nhập người dùng (authorization code + PKCE).
	clients := named(kn["clients"], "clientId")
	var ids, login []string
	for id, c := range clients {
		ids = append(ids, id)
		if c["standardFlowEnabled"] == true {
			login = append(login, id)
			if c["publicClient"] != true || c["directAccessGrantsEnabled"] != false || c["attributes"].(map[string]any)["pkce.code.challenge.method"] != "S256" {
				t.Errorf("client %s phải là public, PKCE S256, không direct grant", id)
			}
		}
	}
	slices.Sort(ids)
	slices.Sort(login)
	if !slices.Equal(ids, []string{"kn-accounts", "kn-field", "kn-seed", "kn-web"}) || !slices.Equal(login, []string{"kn-field", "kn-web"}) {
		t.Errorf("client=%v, client đăng nhập=%v", ids, login)
	}
	if acc := clients["kn-accounts"]; acc["secret"] != "${AG_KC_CLIENT_SECRET}" || acc["serviceAccountsEnabled"] != true {
		t.Error("kn-accounts: bí mật phải là placeholder ${AG_KC_CLIENT_SECRET}, không ghi giá trị thật")
	}
	mainClients := named(main["clients"], "clientId")
	for knID, mainID := range map[string]string{"kn-web": "kn-web", "kn-field": "agrigreen-field-app"} {
		a, b := clients[knID], mainClients[mainID]
		for _, k := range []string{"redirectUris", "attributes", "defaultClientScopes", "optionalClientScopes"} {
			if !reflect.DeepEqual(a[k], b[k]) {
				t.Errorf("%s.%s lệch %s của realm agrigreen", knID, k, mainID)
			}
		}
	}
	users := kn["users"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["serviceAccountClientId"] != "kn-accounts" {
		t.Errorf("realm kn chỉ được có tài khoản dịch vụ của kn-accounts, không người dùng/mật khẩu: %d", len(users))
	}

	// Vai: tập con của realm agrigreen, cùng định nghĩa; không vai của hệ ngoài KN.
	mainRoles := named(main["roles"].(map[string]any)["realm"], "name")
	for name, r := range named(kn["roles"].(map[string]any)["realm"], "name") {
		if name == "default-roles-kn" {
			continue
		}
		if !reflect.DeepEqual(r, mainRoles[name]) {
			t.Errorf("vai %s lệch realm agrigreen", name)
		}
		for _, p := range []string{"coop_", "cgh_", "mrv_", "hub_", "plot_", "content_", "api_client_", "billing_", "waterway_", "sales_", "field_"} {
			if strings.HasPrefix(name, p) {
				t.Errorf("vai %s thuộc hệ ngoài KN", name)
			}
		}
		for _, x := range []string{"farmer", "mkg_sync", "map_consumer", "simulation_analyst", "metering_viewer", "driver", "dispatcher", "sop_admin"} {
			if name == x {
				t.Errorf("vai %s thuộc hệ ngoài KN", name)
			}
		}
	}
	for _, need := range []string{"platform_admin", "account_admin", "kn_user", "kn_province_admin", "vault_registrar", "login_totp_required"} {
		if _, ok := named(kn["roles"].(map[string]any)["realm"], "name")[need]; !ok {
			t.Errorf("thiếu vai %s", need)
		}
	}

	// Đăng nhập, khóa tạm, phiên, mật khẩu, hồ sơ người dùng: y như realm agrigreen.
	for _, k := range []string{"browserFlow", "directGrantFlow", "authenticatorConfig", "passwordPolicy", "bruteForceProtected", "failureFactor",
		"waitIncrementSeconds", "maxFailureWaitSeconds", "permanentLockout", "accessTokenLifespan", "ssoSessionIdleTimeout", "ssoSessionMaxLifespan",
		"offlineSessionIdleTimeout", "offlineSessionMaxLifespan", "otpPolicyType", "otpPolicyDigits", "otpPolicyPeriod", "components", "loginTheme"} {
		if !reflect.DeepEqual(kn[k], main[k]) {
			t.Errorf("%s lệch realm agrigreen", k)
		}
	}
	mainFlows := named(main["authenticationFlows"], "alias")
	for alias, f := range named(kn["authenticationFlows"], "alias") {
		if !reflect.DeepEqual(f, mainFlows[alias]) {
			t.Errorf("flow %q lệch realm agrigreen", alias)
		}
		if strings.Contains(alias, "recovery") {
			t.Errorf("flow %q: realm kn không có luồng quên mật khẩu của Keycloak", alias)
		}
	}

	// Claim: như realm agrigreen, riêng aud là chuỗi tùy biến (realm kn không có client agrigreen-api).
	mainScopes := named(main["clientScopes"], "name")
	for name, s := range named(kn["clientScopes"], "name") {
		if name != "agrigreen-claims" {
			if !reflect.DeepEqual(s, mainScopes[name]) {
				t.Errorf("client scope %s lệch realm agrigreen", name)
			}
			continue
		}
		got := map[string]string{}
		for _, m := range s["protocolMappers"].([]any) {
			mm := m.(map[string]any)
			cfg := mm["config"].(map[string]any)
			if mm["protocolMapper"] == "oidc-audience-mapper" {
				got["aud"], _ = cfg["included.custom.audience"].(string)
				continue
			}
			got[mm["name"].(string)], _ = cfg["access.token.claim"].(string)
		}
		if want := map[string]string{"aud": "agrigreen-api", "tenant_id": "true", "org_ref": "true", "amr": "true"}; !reflect.DeepEqual(got, want) {
			t.Errorf("agrigreen-claims = %v, cần %v", got, want)
		}
	}
}
