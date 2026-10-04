package modreg

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const validAppYAML = `app: KN
description: Khuyến nông
modules:
  pf/party: ">=1.0 <2"
  pf/masterdata: ">=1.3 <2"
roles: [kn_officer]
context_roles: [kn_xa]
landing: {kn_xa: /kn/nhiem-vu/cong-viec, kn_officer: /}
deployment_units: [api-platform]
frontends: [apps/web-ops]
clients: [agrigreen-web-ops]
`

const realmJSON = `{"roles":{"realm":[{"name":"kn_officer"},{"name":"farmer"}]},"clients":[{"clientId":"agrigreen-web-ops"}]}`

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := New([]*Manifest{
		mustParse(t, validYAML, "internal/platform/party/module.yaml"),
		mustParse(t, masterdataYAML, "internal/platform/masterdata/module.yaml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func testFS(apps map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{
		RealmFile:                      {Data: []byte(realmJSON)},
		"cmd/api-platform/main.go":     {Data: []byte("package main")},
		"cmd/worker/main.go":           {Data: []byte("package main")},
		"cmd/internal/x/x.go":          {Data: []byte("package x")},
		"apps/web-ops/package.json":    {Data: []byte("{}")},
		"internal/platform/party/a.go": {Data: []byte("package party")},
	}
	for name, data := range apps {
		fsys["app/"+name+"/app.yaml"] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

func TestLoadAppsValid(t *testing.T) {
	apps, env, err := LoadApps(testFS(map[string]string{"KN": validAppYAML}), testRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	if !env.Units["api-platform"] || env.Units["internal"] || !env.Frontends["apps/web-ops"] || !env.RealmRoles["farmer"] {
		t.Fatalf("AppEnv sai: %+v", env)
	}
	if got := apps.AppsOfUnit("api-platform"); len(got) != 1 || got[0] != "KN" {
		t.Fatalf("AppsOfUnit = %v", got)
	}
	if s := apps.ServedModules("api-platform"); !s["pf/party"] || !s["pf/masterdata"] || len(s) != 2 {
		t.Fatalf("ServedModules = %v", s)
	}
}

func TestAppInvalid(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"tên lạ", "app: KN", "app: XX", `app "XX" phải thuộc`},
		{"thư mục", "app: KN", "app: PF", "không khớp thư mục app/KN"},
		{"trường lạ", "description:", "descripton:", "field descripton not found"},
		{"thiếu mô tả", "description: Khuyến nông", "description: ''", "description bắt buộc"},
		{"module lạ", "pf/party:", "pf/nope:", `modules "pf/nope": không có module này`},
		{"tên module sai", "pf/party:", "party:", `tên module "party" không hợp lệ`},
		{"phiên bản", `">=1.3 <2"`, `">=2.0 <3"`, "không thỏa phiên bản hiện có 1.3.1"},
		{"ràng buộc hỏng", `">=1.3 <2"`, `">=x"`, "modules pf/masterdata"},
		{"vai realm", "roles: [kn_officer]", "roles: [kn_officer, kn_ghost]", `roles "kn_ghost" không có trong`},
		{"vai trùng", "roles: [kn_officer]", "roles: [kn_officer, kn_officer]", "lặp"},
		{"landing vai lạ", "kn_officer: /}", "farmer: /}", `landing: vai "farmer"`},
		{"landing đường dẫn", "kn_officer: /}", "kn_officer: home}", "phải bắt đầu bằng /"},
		{"context trùng roles", "context_roles: [kn_xa]", "context_roles: [kn_xa, kn_officer]", "đã có trong roles"},
		{"đơn vị lạ", "[api-platform]", "[api-platform, api-ghost]", `deployment_units "api-ghost" không có trong cmd/*/main.go`},
		{"cmd/internal không phải đơn vị", "[api-platform]", "[api-platform, internal]", `deployment_units "internal"`},
		{"giao diện lạ", "[apps/web-ops]", "[apps/web-ghost]", `frontends "apps/web-ghost"`},
		{"client lạ", "[agrigreen-web-ops]", "[agrigreen-ghost]", `clients "agrigreen-ghost"`},
		{"thiếu đơn vị", "deployment_units: [api-platform]", "deployment_units: []", "deployment_units bắt buộc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(validAppYAML, tc.from, tc.to, 1)
			if data == validAppYAML {
				t.Fatalf("thay %q không đổi gì", tc.from)
			}
			_, _, err := LoadApps(testFS(map[string]string{"KN": data}), testRegistry(t))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("lỗi = %v, cần chứa %q", err, tc.want)
			}
		})
	}
}

func TestAppDuplicate(t *testing.T) {
	a := mustParseApp(t, validAppYAML, "app/KN/app.yaml")
	b := mustParseApp(t, validAppYAML, "app/KN/app.yaml")
	env, err := LoadAppEnv(testFS(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewApps([]*App{a, b}, testRegistry(t), env); err == nil || !strings.Contains(err.Error(), "trùng") {
		t.Fatalf("lỗi = %v", err)
	}
}

func mustParseApp(t *testing.T, data, p string) *App {
	t.Helper()
	a, err := ParseApp([]byte(data), p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckAssembly(t *testing.T) {
	reg := testRegistry(t)
	apps, _, err := LoadApps(testFS(map[string]string{"KN": validAppYAML}), reg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		wired map[string][]string
		want  []string
	}{
		{"khớp", map[string][]string{"api-platform": {"pf/masterdata", "pf/party"}}, nil},
		{"ghép ngoài app", map[string][]string{"api-platform": {"pf/masterdata", "pf/party", "pf/geo"}},
			[]string{"cmd/api-platform ghép pf/geo nhưng pf/geo không có trong modules của [KN]"}},
		{"đơn vị không app nào liệt kê", map[string][]string{"api-platform": {"pf/masterdata", "pf/party"}, "worker": {"pf/party"}},
			[]string{"cmd/worker: không ứng dụng nào liệt kê"}},
		{"module app không đơn vị nào ghép", map[string][]string{"api-platform": {"pf/party"}},
			[]string{"module pf/masterdata không được đơn vị nào trong [api-platform] ghép"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := apps.CheckAssembly(reg, tc.wired)
			got := errors.Join(errs...)
			if len(errs) != len(tc.want) {
				t.Fatalf("lỗi = %v, cần %d lỗi", got, len(tc.want))
			}
			for _, w := range tc.want {
				if !strings.Contains(got.Error(), w) {
					t.Fatalf("lỗi = %v, cần chứa %q", got, w)
				}
			}
		})
	}
}

func TestCheckAssemblyModuleWithoutApp(t *testing.T) {
	reg := testRegistry(t)
	data := strings.Replace(validAppYAML, "  pf/masterdata: \">=1.3 <2\"\n", "", 1)
	apps, _, err := LoadApps(testFS(map[string]string{"KN": data}), reg)
	if err != nil {
		t.Fatal(err)
	}
	errs := apps.CheckAssembly(reg, map[string][]string{"api-platform": {"pf/party"}})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "module pf/masterdata không thuộc ứng dụng nào") {
		t.Fatalf("lỗi = %v", errs)
	}
}

// TestRepoApps: app/*/app.yaml thật của repo hợp lệ với module.yaml, realm, cmd/*, apps/*.
func TestRepoApps(t *testing.T) {
	reg, err := Load("../../..")
	if err != nil {
		t.Fatal(err)
	}
	apps, _, err := LoadApps(os.DirFS("../../.."), reg)
	if err != nil {
		t.Fatal(err)
	}
	if got := apps.Names(); len(got) != len(AppNames) {
		t.Fatalf("ứng dụng = %v, cần %v", got, AppNames)
	}
}
