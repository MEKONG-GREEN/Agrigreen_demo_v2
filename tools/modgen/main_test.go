package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRunInvalidDoesNotOverwrite: bộ manifest không hợp lệ thì tệp đã có giữ nguyên,
// tệp chưa có vẫn được tạo khung để điền tay.
func TestRunInvalidDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"internal/mkg", "internal/vault", "migrations"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("internal/platform/party/party.go", "package party\n")
	mk("internal/platform/geo/geo.go", "package geo\n")
	partyYAML := "# sửa tay\nname: pf/party\nkind: app\nzones: [pf]\nversion: 1.0.0\nenable: always\n"
	mk("internal/platform/party/module.yaml", partyYAML)

	_, err := run(root, false)
	if err == nil || !strings.Contains(err.Error(), "không hợp lệ") {
		t.Fatalf("cần lỗi không hợp lệ, có %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "internal", "platform", "party", "module.yaml"))
	if err != nil || string(got) != partyYAML {
		t.Fatalf("tệp đã có bị ghi đè:\n%s (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "platform", "geo", "module.yaml")); err != nil {
		t.Fatalf("tệp chưa có phải được tạo khung: %v", err)
	}
}

// TestRepoUpToDate là cổng W1.1: module.yaml trong repo khớp mã nguồn và migration.
func TestRepoUpToDate(t *testing.T) {
	stale, err := run("../..", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("module.yaml lỗi thời, chạy go run ./tools/modgen: %v", stale)
	}
}

// TestWiredModules: ghép trong tiến trình tính từ import của cmd/<đơn vị> (không tính port/,
// client/; theo cả cmd/internal/*).
func TestWiredModules(t *testing.T) {
	wired, err := wiredModules("../..", []string{"api-platform", "integration-gateway", "api-public", "identity-vault", "ingest", "migrate"})
	if err != nil {
		t.Fatal(err)
	}
	has := func(u, m string) bool { return slices.Contains(wired[u], m) }
	if !has("api-platform", "pf/party") || !has("api-platform", "pf/sync") || has("api-platform", "vault/identity") {
		t.Errorf("api-platform: %v (vault/identity chỉ qua client/, không ghép)", wired["api-platform"])
	}
	if !has("integration-gateway", "pf/apiclient") || has("integration-gateway", "pf/party") {
		t.Errorf("integration-gateway: %v (pf/party chỉ qua port/)", wired["integration-gateway"])
	}
	// W5.2: bí danh một kỳ ghép đúng module của đơn vị đích.
	if !slices.Equal(wired["api-public"], wired["integration-gateway"]) {
		t.Errorf("api-public (bí danh) %v ≠ integration-gateway %v", wired["api-public"], wired["integration-gateway"])
	}
	if !has("identity-vault", "vault/identity") || !has("ingest", "mkg/measurement") || len(wired["migrate"]) != 0 {
		t.Errorf("wired = %v", wired)
	}
}

func TestInProcess(t *testing.T) {
	cases := map[string]bool{
		"internal/platform/party":                 true,
		"internal/mkg/measurement/ingest":         true,
		"internal/platform/geo/port/farmerid":     false,
		"internal/vault/identity/client":          false,
		"internal/vault/identity/client/clientmt": false,
	}
	for in, want := range cases {
		if got := inProcess(in); got != want {
			t.Errorf("inProcess(%q) = %v", in, got)
		}
	}
}

// TestClassify: import chỉ dùng kiểu sự kiện/hằng → consumes; có interface/hàm → depends.
func TestClassify(t *testing.T) {
	ip := modulePath + "/internal/platform/geo/port"
	idx := map[string]*portPkg{ip: {
		class:  map[string]symClass{"PlotCreatedV1": symEvent, "EventPlotCreated": symConst, "RoleGeo": symConst, "PlotStats": symOther},
		events: map[string][]string{"PlotCreatedV1": {"pf.geo.plot.created/v1"}, "EventPlotCreated": {"pf.geo.plot.created/v1"}},
	}}
	s := &scan{uses: map[string]map[string]map[string]bool{"pf/geo": {ip: {"PlotCreatedV1": true, "EventPlotCreated": true, "RoleGeo": true}}}, opaque: map[string]bool{}}
	s.classify(idx)
	if len(s.deps) != 0 || len(s.consumes) != 1 || s.consumes[0] != "pf.geo.plot.created/v1" {
		t.Fatalf("chỉ hợp đồng dữ liệu → consumes: deps=%v consumes=%v", s.deps, s.consumes)
	}
	s.uses["pf/geo"][ip]["PlotStats"] = true
	s.classify(idx)
	if len(s.deps) != 1 || s.deps[0] != "pf/geo" {
		t.Fatalf("có interface → depends: %v", s.deps)
	}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		modulePath + "/internal/platform/party/port":        "pf/party",
		modulePath + "/internal/vault/identity/client":      "vault/identity",
		modulePath + "/internal/mkg/measurement/port/edgew": "mkg/measurement",
		modulePath + "/internal/shared/db":                  "",
		modulePath + "/contracts/events":                    "",
		"example.com/x/internal/platform/party/port":        "",
	}
	for in, want := range cases {
		if got := moduleOf(in); got != want {
			t.Errorf("moduleOf(%q) = %q, cần %q", in, got, want)
		}
	}
}

func TestMentionsSchema(t *testing.T) {
	cases := []struct {
		lit, schema string
		want        bool
	}{
		{"SELECT 1 FROM ts_weigh.ticket", "ts_weigh", true},
		{"SELECT 1 FROM xts_weigh.ticket", "ts_weigh", false},
		{"ts_weigh", "ts_weigh", false},
		{"a pf_files.object b", "pf_files", true},
	}
	for _, tc := range cases {
		if got := mentionsSchema(tc.lit, tc.schema); got != tc.want {
			t.Errorf("mentionsSchema(%q, %q) = %v", tc.lit, tc.schema, got)
		}
	}
}

// W6.3: <cụm>_<module>_pub thuộc chủ của <cụm>_<module> dù module khác (pf/metrics) nhắc nó trong SQL;
// //ag:pubview vào provides.publications cùng van công bố.
func TestPubSchemaOwnerAndPublications(t *testing.T) {
	scans := map[string]*scan{
		"pf/geo":     {literals: map[string]bool{"SELECT 1 FROM pf_geo.plot": true}},
		"pf/metrics": {literals: map[string]bool{"SELECT 1 FROM pf_geo_pub.v1_plot_event": true}},
	}
	got := schemaOwners(scans, map[string]string{"pf_geo": "", "pf_geo_pub": ""})
	if !slices.Equal(got["pf/geo"], []string{"pf_geo", "pf_geo_pub"}) || len(got["pf/metrics"]) != 0 {
		t.Fatalf("owners: %v", got)
	}
	pubs := publications(map[string][]string{"publication": {"hub_capacity"}, "pubview": {"pf_geo_pub.plot_event/v1"}})
	if !slices.Equal(pubs, []string{"hub_capacity", "pf_geo_pub.plot_event/v1"}) {
		t.Fatalf("publications: %v", pubs)
	}
}

func TestReadPubViewDirectiveBelongsToReader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "platform", "reader", "port")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := "package port\n//ag:readpubview pf_source_pub.data v2\n"
	if err := os.WriteFile(filepath.Join(dir, "publications.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	scan, err := scanModule(root, "internal/platform/reader")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scan.directives["readpubview"], []string{"pf_source_pub.data/v2"}) {
		t.Fatalf("reader directive: %v", scan.directives)
	}
	if len(publications(scan.directives)) != 0 {
		t.Fatal("reader declaration accidentally publishes source contract")
	}
}
