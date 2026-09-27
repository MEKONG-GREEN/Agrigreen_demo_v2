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
	Roles struct {
		Realm []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"realm"`
	} `json:"roles"`
	ClientScopes []struct {
		Name            string `json:"name"`
		ProtocolMappers []struct {
			Name           string            `json:"name"`
			ProtocolMapper string            `json:"protocolMapper"`
			Config         map[string]string `json:"config"`
		} `json:"protocolMappers"`
	} `json:"clientScopes"`
	Clients []struct {
		ClientID            string   `json:"clientId"`
		WebOrigins          []string `json:"webOrigins"`
		DefaultClientScopes []string `json:"defaultClientScopes"`
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
	for _, admin := range []string{"platform_admin", "masterdata_admin", "registry_admin", "geo_admin", "device_admin", "vault_eraser"} {
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
	for _, c := range realm.Clients {
		for _, s := range c.DefaultClientScopes {
			if !scopes[s] {
				t.Errorf("client %s dùng scope %s không khai trong realm", c.ClientID, s)
			}
		}
		if c.ClientID == "agrigreen-web-ops" && (!slices.Contains(c.WebOrigins, "http://localhost:3000") || slices.Contains(c.WebOrigins, "*") || slices.Contains(c.WebOrigins, "+")) {
			t.Errorf("webOrigins của web-ops: %v", c.WebOrigins)
		}
	}
}
