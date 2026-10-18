package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const repoRoot = "../.."

// roleConstants quét mã nguồn (không import) để lấy mọi hằng Role* có giá trị chuỗi
// trong port/ của các module, vai của kho định danh và vai dùng chung ở authz.
func roleConstants(t *testing.T) map[string]string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(p)
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.Contains(slash, "/port/") || strings.HasSuffix(slash, "vault/identity/client/roles.go") || strings.HasSuffix(slash, "shared/authz/authz.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Role") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue // bí danh của vai khai ở nơi khác
					}
					v, _ := strconv.Unquote(lit.Value)
					out[v] = filepath.ToSlash(f) + ":" + name.Name
				}
			}
		}
	}
	return out
}

type realmFile struct {
	RegistrationAllowed bool `json:"registrationAllowed"`
	BruteForceProtected bool `json:"bruteForceProtected"`
	FailureFactor       int  `json:"failureFactor"`
	MaxDeltaTimeSeconds int  `json:"maxDeltaTimeSeconds"`
	// A3 / Q-21: MULTIPLE = khóa ở mỗi bội số failureFactor (5, 10, 15…), không khóa lại ở từng lần sai lẻ.
	BruteForceStrategy string `json:"bruteForceStrategy"`
	// KN v7.7 N-10 (thay GIS US-LOG-01 AC-3): khóa TẠM 15 phút, tự mở sau thời gian chờ.
	PermanentLockout      bool   `json:"permanentLockout"`
	MaxTemporaryLockouts  int    `json:"maxTemporaryLockouts"`
	WaitIncrementSeconds  int    `json:"waitIncrementSeconds"`
	MaxFailureWaitSeconds int    `json:"maxFailureWaitSeconds"`
	LoginWithEmail        bool   `json:"loginWithEmailAllowed"`
	LoginTheme            string `json:"loginTheme"`
	// KN v7.7 N-09: phiên Web 4 giờ không thao tác, Mobile 24 giờ + ngoại tuyến 72 giờ.
	SSOSessionIdleTimeout     int                          `json:"ssoSessionIdleTimeout"`
	SSOSessionMaxLifespan     int                          `json:"ssoSessionMaxLifespan"`
	OfflineSessionMaxLifespan int                          `json:"offlineSessionMaxLifespan"`
	OfflineMaxLifespanEnabled bool                         `json:"offlineSessionMaxLifespanEnabled"`
	LocalizationTexts         map[string]map[string]string `json:"localizationTexts"`
	PasswordPolicy            string                       `json:"passwordPolicy"`
	Roles                     struct {
		Realm []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Composite   bool   `json:"composite"`
			Composites  struct {
				Realm []string `json:"realm"`
			} `json:"composites"`
		} `json:"realm"`
	} `json:"roles"`
	// D24: luồng đăng nhập có TOTP theo vai.
	BrowserFlow         string `json:"browserFlow"`
	AuthenticatorConfig []struct {
		Alias  string            `json:"alias"`
		Config map[string]string `json:"config"`
	} `json:"authenticatorConfig"`
	AuthenticationFlows []struct {
		Alias      string `json:"alias"`
		TopLevel   bool   `json:"topLevel"`
		Executions []struct {
			Authenticator       string `json:"authenticator"`
			AuthenticatorConfig string `json:"authenticatorConfig"`
			FlowAlias           string `json:"flowAlias"`
			Requirement         string `json:"requirement"`
			Priority            int    `json:"priority"`
		} `json:"authenticationExecutions"`
	} `json:"authenticationFlows"`
	ClientScopes []struct {
		Name            string `json:"name"`
		ProtocolMappers []struct {
			Name           string            `json:"name"`
			ProtocolMapper string            `json:"protocolMapper"`
			Config         map[string]string `json:"config"`
		} `json:"protocolMappers"`
	} `json:"clientScopes"`
	Clients []struct {
		ClientID            string            `json:"clientId"`
		WebOrigins          []string          `json:"webOrigins"`
		DefaultClientScopes []string          `json:"defaultClientScopes"`
		Attributes          map[string]string `json:"attributes"`
	} `json:"clients"`
}

// Realm Keycloak của Local/Dev phải có đủ vai mà backend kiểm, và các claim
// backend đọc (tenant_id, org_ref, amr, realm_access.roles, aud = agrigreen-api).
func TestKeycloakRealmCoversRolesAndClaims(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm realmFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&realm); err != nil {
		t.Fatalf("realm JSON: %v", err)
	}
	// CGH FN-01 BR-01/luồng ngoại lệ: không đăng ký công khai; sai 5 lần → khóa tạm thời; mật khẩu có tiêu chí độ
	// mạnh (BR-02). Cửa sổ đếm lần sai 24 giờ (A3 03/10/2026, thay 15 phút — cần để đếm được 3 lần khóa tạm/24 h).
	if realm.RegistrationAllowed || !realm.BruteForceProtected || realm.FailureFactor != 5 || realm.MaxDeltaTimeSeconds != 86400 ||
		!strings.Contains(realm.PasswordPolicy, "length(") {
		t.Errorf("chính sách đăng nhập: đăng ký=%v chống dò=%v sai tối đa=%d cửa sổ=%ds mật khẩu=%q",
			realm.RegistrationAllowed, realm.BruteForceProtected, realm.FailureFactor, realm.MaxDeltaTimeSeconds, realm.PasswordPolicy)
	}
	// KN v7.7 FN-CH-00.01 / N-10 (thay GIS US-LOG-01 AC-3 — BRD v7.7 mới hơn, gap §4.6) + A3/Q-21 (chốt
	// 03/10/2026): mỗi 5 lần sai → khóa TẠM 15 phút rồi tự mở (chờ 900 s); sau 3 lần khóa tạm trong cửa sổ 24 giờ
	// → khóa HẲN (permanentLockout = true, maxTemporaryLockouts = 3; Keycloak 26 đặt enabled = false), Admin mở
	// qua pf/accounts unlock. Tài khoản bị khóa giữ thông báo "liên hệ Admin".
	const lockedMsg = "Tài khoản đã bị khóa, vui lòng liên hệ Admin để mở khóa."
	if !realm.PermanentLockout || realm.MaxTemporaryLockouts != 3 || realm.BruteForceStrategy != "MULTIPLE" ||
		realm.WaitIncrementSeconds != 900 || realm.MaxFailureWaitSeconds != 900 ||
		realm.LocalizationTexts["vi"]["accountPermanentlyDisabledMessage"] != lockedMsg ||
		!strings.Contains(realm.LocalizationTexts["vi"]["invalidUserMessage"], "3 lần trong 24 giờ") ||
		!strings.Contains(realm.LocalizationTexts["vi"]["accountTemporarilyDisabledMessage"], "15 phút") ||
		realm.LocalizationTexts["vi"]["accountDisabledMessage"] != lockedMsg {
		t.Errorf("KN v7.7 N-10 / A3: permanentLockout=%v maxTemporaryLockouts=%d strategy=%q waitIncrement=%d maxFailureWait=%d thông báo khóa tạm=%q",
			realm.PermanentLockout, realm.MaxTemporaryLockouts, realm.BruteForceStrategy, realm.WaitIncrementSeconds, realm.MaxFailureWaitSeconds,
			realm.LocalizationTexts["vi"]["accountTemporarilyDisabledMessage"])
	}
	// KN v7.7 FN-CH-00.01: đăng nhập bằng email (loginWithEmailAllowed) hoặc SĐT (username = SĐT chuẩn hóa).
	if !realm.LoginWithEmail {
		t.Error("KN v7.7 FN-CH-00.01: phải đăng nhập được bằng email (loginWithEmailAllowed)")
	}
	// KN v7.7 FN-CH-00.01 / X-08 (Hình 3a): theme đăng nhập agrigreen — realm đặt loginTheme và compose gắn
	// thư mục theme vào /opt/keycloak/themes/agrigreen (thiếu theme thì Keycloak rơi về theme mặc định).
	if realm.LoginTheme != "agrigreen" {
		t.Errorf("KN v7.7 X-08: loginTheme=%q, cần \"agrigreen\"", realm.LoginTheme)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "deploy", "compose", "keycloak-themes", "agrigreen", "login", "theme.properties")); err != nil {
		t.Errorf("KN v7.7 X-08: thiếu theme agrigreen: %v", err)
	}
	if compose, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "docker-compose.yml")); err != nil ||
		!strings.Contains(string(compose), "./keycloak-themes/agrigreen:/opt/keycloak/themes/agrigreen:ro") {
		t.Errorf("KN v7.7 X-08: docker-compose.yml chưa gắn theme agrigreen vào Keycloak (%v)", err)
	}
	// KN v7.7 N-09 / D9, GIỮ NGUYÊN theo D24 (CEO 13/10/2026) kèm đánh giá rủi ro
	// docs/security/danh-gia-rui-ro-phien-4h-24h.md: realm 4 giờ không thao tác, tối đa 24 giờ; ngoại tuyến 72 giờ.
	// Giá trị KHÓA CỨNG — đổi phải qua quyết định mới và cập nhật bản đánh giá rủi ro.
	if realm.SSOSessionIdleTimeout != 14400 || realm.SSOSessionMaxLifespan != 86400 || !realm.OfflineMaxLifespanEnabled || realm.OfflineSessionMaxLifespan != 259200 {
		t.Errorf("KN v7.7 N-09: ssoIdle=%d ssoMax=%d offlineMax=%d (bật=%v)", realm.SSOSessionIdleTimeout, realm.SSOSessionMaxLifespan,
			realm.OfflineSessionMaxLifespan, realm.OfflineMaxLifespanEnabled)
	}
	wantClient := map[string]map[string]string{
		"agrigreen-web-ops":   {"client.session.idle.timeout": "14400", "client.session.max.lifespan": "36000"},
		"agrigreen-field-app": {"client.session.max.lifespan": "86400", "client.offline.session.max.lifespan": "259200"},
	}
	for _, c := range realm.Clients {
		for k, v := range wantClient[c.ClientID] {
			if c.Attributes[k] != v {
				t.Errorf("KN v7.7 N-09: client %s %s=%q, cần %q", c.ClientID, k, c.Attributes[k], v)
			}
		}
	}
	have := map[string]string{}
	for _, r := range realm.Roles.Realm {
		if _, dup := have[r.Name]; dup {
			t.Fatalf("vai %s khai hai lần", r.Name)
		}
		have[r.Name] = r.Description
	}
	roles := roleConstants(t)
	if len(roles) < 20 {
		t.Fatalf("quét được quá ít hằng vai (%d) — đường quét sai?", len(roles))
	}
	for v, where := range roles {
		if _, ok := have[v]; !ok {
			t.Errorf("realm thiếu vai %q (%s)", v, where)
		}
	}
	for _, admin := range []string{"platform_admin", "masterdata_admin", "registry_admin", "geo_admin", "device_admin", "vault_eraser", "account_admin", "account_admin_province"} {
		if !strings.Contains(have[admin], "MFA") {
			t.Errorf("vai quản trị %s phải ghi chú bắt buộc MFA", admin)
		}
	}

	claims := map[string]string{} // claim → loại mapper
	scopes := map[string]bool{}
	for _, s := range realm.ClientScopes {
		scopes[s.Name] = true
		for _, m := range s.ProtocolMappers {
			switch {
			case m.Config["claim.name"] != "":
				claims[m.Config["claim.name"]] = m.ProtocolMapper
			case m.ProtocolMapper == "oidc-audience-mapper":
				claims["aud:"+m.Config["included.client.audience"]] = m.ProtocolMapper
			default:
				claims[m.ProtocolMapper] = m.ProtocolMapper
			}
		}
	}
	for claim, mapper := range map[string]string{
		"tenant_id": "oidc-usermodel-attribute-mapper", "org_ref": "oidc-usermodel-attribute-mapper",
		"realm_access.roles": "oidc-usermodel-realm-role-mapper", "aud:agrigreen-api": "oidc-audience-mapper",
		"oidc-amr-mapper": "oidc-amr-mapper", "oidc-sub-mapper": "oidc-sub-mapper",
	} {
		if claims[claim] != mapper {
			t.Errorf("thiếu mapper %s cho %s (có %v)", mapper, claim, claims)
		}
	}
	webInternal := false
	for _, c := range realm.Clients {
		for _, s := range c.DefaultClientScopes {
			if !scopes[s] {
				t.Errorf("client %s dùng scope %s không khai trong realm", c.ClientID, s)
			}
		}
		if c.ClientID == "agrigreen-web-ops" && (!slices.Contains(c.WebOrigins, "http://localhost:3000") || slices.Contains(c.WebOrigins, "*") || slices.Contains(c.WebOrigins, "+")) {
			t.Errorf("webOrigins của web-ops: %v", c.WebOrigins)
		}
		// G3-2: web nội bộ vùng MKG (apps/web-internal) — client công khai riêng, cổng 3001.
		if c.ClientID == "agrigreen-web-internal" {
			webInternal = true
			if !slices.Contains(c.WebOrigins, "http://localhost:3001") || slices.Contains(c.WebOrigins, "*") || slices.Contains(c.WebOrigins, "+") {
				t.Errorf("webOrigins của web-internal: %v", c.WebOrigins)
			}
		}
	}
	if !webInternal {
		t.Error("realm thiếu client agrigreen-web-internal (apps/web-internal)")
	}
}

// Keycloak lưu description của vai/client trong cột VARCHAR(255): mô tả dài hơn làm --import-realm hỏng và
// Keycloak không khởi động (Compose: "Value too long for column DESCRIPTION"). Đếm theo ký tự, không theo byte.
func TestKeycloakRealmDescriptionsFitColumn(t *testing.T) {
	for _, name := range []string{"agrigreen-realm.json", "agrigreen-kn-admin-realm.json"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", name))
		if err != nil {
			t.Fatal(err)
		}
		var realm struct {
			Roles struct {
				Realm []struct{ Name, Description string } `json:"realm"`
			} `json:"roles"`
			Clients []struct {
				ClientID    string `json:"clientId"`
				Description string `json:"description"`
			} `json:"clients"`
		}
		if err := json.Unmarshal(raw, &realm); err != nil {
			t.Fatal(err)
		}
		for _, r := range realm.Roles.Realm {
			if n := len([]rune(r.Description)); n > 255 {
				t.Errorf("%s: vai %s có description %d ký tự (tối đa 255)", name, r.Name, n)
			}
		}
		for _, c := range realm.Clients {
			if n := len([]rune(c.Description)); n > 255 {
				t.Errorf("%s: client %s có description %d ký tự (tối đa 255)", name, c.ClientID, n)
			}
		}
	}
}
