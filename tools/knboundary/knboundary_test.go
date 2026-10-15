package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture dựng một repo nhỏ đủ mọi quy tắc: mỗi tệp gây đúng các ghép nối liệt kê ở wantFixture.
var fixture = map[string]string{
	"go.mod": "module example.com/ag\n\ngo 1.26\n",
	"tools/knboundary/boundary.yaml": `kn_modules: [internal/kn, internal/platform/extension]
htx_modules: [internal/platform/cooperative]
composition_roots: [cmd/api-platform]
test_roots: [tests]
go_roots: [internal, cmd, tests]
symbol_roots: [internal]
migrations_root: migrations
kn_migration_pattern: '_pf_extension(_|\.)'
free_schemas: [k_ref, shared]
symbol_marker: '(^|[a-z0-9_])(KN|Kn|CNTS|Cnts|KHTC|Khtc)([A-Z0-9_]|s?$)'
file_marker: '(^|_)(kn|cnts|khtc)(_|\.)'
kn_inventory_files: [internal/platform/party/menu.go, internal/platform/party/gone.go]
openapi: [contracts/openapi/application.yaml]
kn_tags: [extension]
kn_route_prefixes: [/v1/extension/, /v1/kn/]
kn_shared_route_prefixes: [/v1/menu]
kn_htx_write_prefixes: [/v1/kn/plots]
web:
  root: apps/web-ops/src
  alias: "@/"
  kn_paths: [kn/, app/kn/]
  kn_may_import: [kn/, app/kn/, lib/]
`,
	"internal/platform/extension/module.yaml":      "name: pf/extension\nowns_schema: [pf_extension, pf_extension_pub]\n",
	"internal/platform/cooperative/module.yaml":    "name: pf/cooperative\nowns_schema: [pf_coop, pf_coop_pub]\n",
	"internal/platform/party/module.yaml":          "name: pf/party\nowns_schema: [pf_party]\n",
	"internal/platform/extension/port/port.go":     "package port\n\ntype Task struct{}\n",
	"internal/platform/cooperative/port/port.go":   "package port\n\ntype Plots interface{}\n",
	"internal/platform/cooperative/store/store.go": "package store\n",
	"internal/mkg/orders/orders.go":                "package orders\n",
	// KN: port HTX cũng không (PO 14/10, kn-import-htx); store HTX và mkg không (kn-import-internal); SQL bảng HTX không được, _pub/k_ref được; chú thích không tính.
	"internal/platform/extension/app/app.go": `package app

import (
	_ "example.com/ag/internal/mkg/orders"
	_ "example.com/ag/internal/platform/cooperative/port"
	_ "example.com/ag/internal/platform/cooperative/store"
	_ "example.com/ag/internal/platform/extension/port"
)

// SELECT * FROM pf_party.org — chú thích, không tính.
const q = "SELECT p.ref FROM pf_coop.plot p JOIN pf_coop_pub.v1_plot v USING (ref) JOIN k_ref.org o ON true"
const src = "pf_coop.bm-04"
const own = "SELECT 1 FROM pf_extension.task"
`,
	// Không gian tên đích internal/kn cũng là KN.
	"internal/kn/price/price.go":         "package price\n\nimport _ \"example.com/ag/internal/platform/party/app\"\n",
	"internal/platform/party/app/app.go": "package app\n",
	// Nền tảng import port KN, đọc bảng KN, có khai báo dấu KN.
	"internal/platform/notify/notify.go": `package notify

import _ "example.com/ag/internal/platform/extension/port"

func KNThing() {}

type Svc struct{}

func (Svc) RegisterKNRoutes() {}

func Unknown() {}

const q = "SELECT 1 FROM pf_extension.task_current UNION SELECT 1 FROM pf_extension_pub.v1_task"
`,
	"internal/platform/notify/notify_test.go":    "package notify\n\nfunc TestKNx() {}\n",
	"internal/platform/party/kn_menu.go":         "package party\n",
	"internal/platform/party/menu.go":            "package party\n",
	"cmd/api-platform/main.go":                   "package main\n\nimport _ \"example.com/ag/internal/platform/extension\"\n",
	"internal/platform/extension/extension.go":   "package extension\n",
	"cmd/api-internal/main.go":                   "package main\n\nimport _ \"example.com/ag/internal/platform/extension\"\n",
	"tests/e2e/e2e_test.go":                      "package e2e\n\nimport _ \"example.com/ag/internal/platform/extension\"\n\nconst q = \"pf_extension.task\"\n",
	"migrations/pf/000001_pf_extension_x.up.sql": "-- pf_party.org trong chú thích\nCREATE VIEW pf_extension.v AS SELECT * FROM pf_coop.plot;\n",
	"migrations/pf/000002_pf_party_x.up.sql":     "GRANT SELECT ON pf_extension.task TO r_party;\n",
	"migrations/pf/000003_pf_pub_x.up.sql":       "CREATE VIEW pf_extension_pub.v1 AS SELECT 1;\n",
	"contracts/openapi/application.yaml": `openapi: 3.1.0
paths:
  /v1/extension/tasks:
    get:
      tags: [extension]
    post:
      summary: không tag — chủ suy từ tiền tố KN
  /v1/kn/plots:
    get:
      tags: [geo]
    post:
      tags: [geo]
  /v1/menu:
    get:
      summary: không tag trên bề mặt dùng chung
  /v1/orgs:
    get:
      tags: [party]
  /v1/hub/x:
    get:
      tags: [extension]
components:
  schemas:
    X:
      tags: [extension]
`,
	"apps/web-ops/src/kn/a.ts":          "import { r } from \"@/htx/rules\";\nimport { d } from \"@/lib/decimal\";\nimport { b } from \"./b\";\n",
	"apps/web-ops/src/kn/b.ts":          "import x from \"../admin/accounts\";\nimport React from \"react\";\n",
	"apps/web-ops/src/shell/menu.ts":    "import type { MenuTree } from \"@/kn/contract\";\nconst m = await import(\"../app/kn/page\");\n",
	"apps/web-ops/src/app/kn/page.tsx":  "export default function P() { return null }\n",
	"apps/web-ops/src/lib/decimal.ts":   "export const d = 1;\n",
	"apps/web-ops/src/htx/rules.ts":     "import { a } from \"@/lib/decimal\";\n",
	"apps/web-ops/node_modules/x/kn.ts": "import \"@/kn/a\";\n",
}

var wantFixture = []Coupling{
	{RuleImportIntoKN, "cmd/api-internal/main.go", "internal/platform/extension"},
	{RuleImportIntoKN, "internal/platform/notify/notify.go", "internal/platform/extension/port"},
	{RuleKNImportInternal, "internal/kn/price/price.go", "internal/platform/party/app"},
	{RuleKNImportInternal, "internal/platform/extension/app/app.go", "internal/mkg/orders"},
	{RuleKNImportInternal, "internal/platform/extension/app/app.go", "internal/platform/cooperative/store"},
	{RuleKNImportHTX, "internal/platform/extension/app/app.go", "internal/platform/cooperative/port"},
	{RuleKNSQLForeign, "internal/platform/extension/app/app.go", "pf_coop.plot"},
	{RuleKNSQLForeign, "migrations/pf/000001_pf_extension_x.up.sql", "pf_coop.plot"},
	{RuleSQLIntoKN, "internal/platform/notify/notify.go", "pf_extension.task_current"},
	{RuleSQLIntoKN, "migrations/pf/000002_pf_party_x.up.sql", "pf_extension.task"},
	{RuleKNSymbolInShared, "internal/platform/notify/notify.go", "KNThing"},
	{RuleKNSymbolInShared, "internal/platform/notify/notify.go", "Svc.RegisterKNRoutes"},
	{RuleKNSymbolInShared, "internal/platform/party/kn_menu.go", "(tệp)"},
	{RuleKNSymbolInShared, "internal/platform/party/menu.go", "(kiểm kê)"},
	{RuleRouteOwner, "application.yaml /v1/hub/x", "extension"},
	{RuleRouteOwner, "application.yaml /v1/kn/plots", "geo"},
	{RuleRouteOwner, "application.yaml /v1/menu", "(không tag)"},
	{RuleKNWriteHTX, "application.yaml POST /v1/kn/plots", "dữ liệu HTX"},
	{RuleWebImportIntoKN, "apps/web-ops/src/shell/menu.ts", "app/kn/page"},
	{RuleWebImportIntoKN, "apps/web-ops/src/shell/menu.ts", "kn/contract"},
	{RuleWebKNImportApp, "apps/web-ops/src/kn/a.ts", "htx/rules"},
	{RuleWebKNImportApp, "apps/web-ops/src/kn/b.ts", "admin/accounts"},
}

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range fixture {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func scanFixture(t *testing.T, root string) []Coupling {
	t.Helper()
	cfg, err := LoadConfig(filepath.Join(root, "tools/knboundary/boundary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Scan(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Mỗi quy tắc bắt đúng ghép nối của nó và không bắt gì khác: gốc lắp ráp, tests/, chú thích, _pub, k_ref, mã nguồn
// kiểu "pf_coop.bm-04", tệp kiểm thử (dấu KN), node_modules, gói npm, route của module KN dưới tiền tố KN, khóa tags
// ngoài paths đều không tính.
func TestScanFixture(t *testing.T) {
	root := writeFixture(t)
	got := scanFixture(t, root)
	want := slices.Clone(wantFixture)
	sortCouplings(want)
	if !slices.Equal(got, want) {
		var b strings.Builder
		for _, c := range got {
			b.WriteString("\n  got  " + c.String())
		}
		for _, c := range want {
			b.WriteString("\n  want " + c.String())
		}
		t.Fatalf("ghép nối lệch:%s", b.String())
	}
}

// Cổng: baseline đúng → 0; ghép nối mới → hỏng "MỚI"; mục lỗi thời → hỏng "LỖI THỜI"; -prune chỉ bỏ mục lỗi thời,
// không thêm ghép nối mới.
func TestRunGate(t *testing.T) {
	root := writeFixture(t)
	cfg, bl := "tools/knboundary/boundary.yaml", "tools/knboundary/baseline.yaml"
	if err := os.WriteFile(filepath.Join(root, bl), FormatBaseline(wantFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if rc := run(&out, root, cfg, bl, "", false, false); rc != 0 {
		t.Fatalf("baseline đúng: rc=%d\n%s", rc, out.String())
	}

	// Ghép nối mới: nền tảng import KN.
	newFile := filepath.Join(root, "internal/platform/party/app/new.go")
	if err := os.WriteFile(newFile, []byte("package app\n\nimport _ \"example.com/ag/internal/platform/extension/port\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if rc := run(&out, root, cfg, bl, "", false, false); rc != 1 || !strings.Contains(out.String(), "MỚI    import-into-kn: internal/platform/party/app/new.go") {
		t.Fatalf("ghép nối mới phải hỏng: rc=%d\n%s", rc, out.String())
	}
	// -prune không ghi nhận ghép nối mới.
	out.Reset()
	if rc := run(&out, root, cfg, bl, "", false, true); rc != 1 {
		t.Fatalf("-prune không được nuốt ghép nối mới: rc=%d\n%s", rc, out.String())
	}
	if err := os.Remove(newFile); err != nil {
		t.Fatal(err)
	}

	// Gỡ một ghép nối → mục baseline lỗi thời → hỏng; -prune bỏ mục và cổng xanh lại.
	if err := os.Remove(filepath.Join(root, "internal/platform/party/kn_menu.go")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if rc := run(&out, root, cfg, bl, "", false, false); rc != 1 || !strings.Contains(out.String(), "LỖI THỜI kn-symbol-in-shared: internal/platform/party/kn_menu.go") {
		t.Fatalf("mục lỗi thời phải hỏng: rc=%d\n%s", rc, out.String())
	}
	out.Reset()
	if rc := run(&out, root, cfg, bl, "", false, true); rc != 0 {
		t.Fatalf("-prune: rc=%d\n%s", rc, out.String())
	}
	got, err := LoadBaseline(filepath.Join(root, bl))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(wantFixture)-1 {
		t.Fatalf("-prune: baseline %d mục, muốn %d", len(got), len(wantFixture)-1)
	}
}

// -base: baseline không được có mục vắng ở baseline của nhánh đích.
func TestGrowth(t *testing.T) {
	old := wantFixture[:3]
	if p := Growth(old, old[:2]); len(p) != 0 {
		t.Fatalf("co lại hợp lệ: %v", p)
	}
	p := Growth(old, wantFixture[:4])
	if len(p) != 1 || !strings.Contains(p[0], "THÊM VÀO BASELINE") {
		t.Fatalf("thêm mục phải báo: %v", p)
	}
}

// Baseline đọc lại đúng như ghi; mục trùng hoặc thiếu trường bị từ chối.
func TestBaselineFormat(t *testing.T) {
	cs := append(slices.Clone(wantFixture), Coupling{RuleRouteOwner, "admin.yaml /v1/accounts/users/{id}/assignments", "accounts"})
	got, err := parseBaseline(FormatBaseline(cs), "x")
	if err != nil {
		t.Fatal(err)
	}
	sortCouplings(cs)
	if !slices.Equal(got, cs) {
		t.Fatalf("vòng ghi/đọc lệch:\n%v\n%v", got, cs)
	}
	if _, err := parseBaseline([]byte("couplings:\n  - {rule: a, from: b, to: c}\n  - {rule: a, from: b, to: c}\n"), "x"); err == nil {
		t.Fatal("mục trùng phải bị từ chối")
	}
	if _, err := parseBaseline([]byte("couplings:\n  - {rule: a, from: b}\n"), "x"); err == nil {
		t.Fatal("mục thiếu to phải bị từ chối")
	}
}

// Cổng trên repo thật: ghép nối hiện có đúng baseline đã commit (giống `go run ./tools/knboundary`).
func TestRepoMatchesBaseline(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools/knboundary/baseline.yaml")); err != nil {
		t.Skip("không chạy trong repo")
	}
	var out bytes.Buffer
	if rc := run(&out, root, "tools/knboundary/boundary.yaml", "tools/knboundary/baseline.yaml", "", false, false); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out.String())
	}
}
