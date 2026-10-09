package modreg

import (
	"strings"
	"testing"
	"testing/fstest"
)

const validYAML = `name: pf/party
aka: [k/orgtree]
kind: kernel
zones: [pf]
version: 1.2.0
depends:
  pf/masterdata: ">=1.0 <2"
provides:
  ports: [Orgs]
  events: [pf.party.org.deactivated/v1]
  internal_events: []
  publications: []
  data_products: []
permissions: [registry_admin]
shares: []
owns_schema: [pf_party]
enable: always
`

const masterdataYAML = `name: pf/masterdata
aka: [cap/masterdata]
kind: capability
zones: [pf]
version: 1.3.1
depends: {}
provides:
  ports: [Registry]
permissions: []
shares: []
owns_schema: [pf_masterdata]
enable: per_tenant
`

func mustParse(t *testing.T, data, p string) *Manifest {
	t.Helper()
	m, err := Parse([]byte(data), p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestValidManifest(t *testing.T) {
	m := mustParse(t, validYAML, "internal/platform/party/module.yaml")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.SemVer() != (Version{1, 2, 0}) || m.Kind.Rank() != 0 {
		t.Fatalf("phân tích sai: %+v", m)
	}
	// Format rồi Parse lại phải giữ nguyên nội dung (dạng chuẩn ổn định).
	again := mustParse(t, string(Format(m)), m.Path)
	if string(Format(again)) != string(Format(m)) {
		t.Fatalf("Format không ổn định:\n%s", Format(again))
	}
}

func TestParseRejectsUnknownFieldAndBOM(t *testing.T) {
	if _, err := Parse([]byte(validYAML+"extra: 1\n"), "x"); err == nil {
		t.Fatal("trường lạ phải bị bác")
	}
	if _, err := Parse(append([]byte("\xef\xbb\xbf"), validYAML...), "x"); err != nil {
		t.Fatalf("BOM phải được bỏ qua: %v", err)
	}
	for _, empty := range []string{"", "# chỉ có chú thích\n"} {
		if _, err := Parse([]byte(empty), "x"); err == nil || !strings.Contains(err.Error(), "tệp rỗng") {
			t.Fatalf("tệp rỗng %q: cần lỗi \"tệp rỗng\", có %v", empty, err)
		}
	}
	if _, err := Parse([]byte(validYAML+"---\nname: pf/geo\n"), "x"); err == nil || !strings.Contains(err.Error(), "một tài liệu") {
		t.Fatalf("tài liệu thứ hai phải bị bác, có %v", err)
	}
}

// TestConsumes: consumes không thuộc đồ thị depends (R-MOD-05 kiểm ở archlint agmodgraph) nhưng phải
// trỏ tới sự kiện module phát có khai.
func TestConsumes(t *testing.T) {
	mk := func(name, kind, deps, consumes, events string) *Manifest {
		dir := map[string]string{"pf": "platform", "mkg": "mkg"}[strings.Split(name, "/")[0]]
		zone := strings.Split(name, "/")[0]
		y := "name: " + name + "\nkind: " + kind + "\nzones: [" + zone + "]\nversion: 1.0.0\nenable: always\n" +
			"depends: {" + deps + "}\nprovides:\n  events: [" + events + "]\nconsumes:\n  events: [" + consumes + "]\n"
		if kind != "kernel" {
			y = strings.Replace(y, "enable: always", "enable: per_tenant", 1)
		}
		return mustParse(t, y, "internal/"+dir+"/"+strings.Split(name, "/")[1]+"/module.yaml")
	}
	geo2 := mk("pf/geo", "capability", `pf/party: ">=1.0 <2"`, "", "pf.geo.plot.created/v1")
	if _, err := New([]*Manifest{mk("pf/party", "kernel", "", "pf.geo.plot.created/v1", ""), geo2}); err != nil {
		t.Fatalf("consumes hợp lệ: %v", err)
	}
	// Sự kiện tiêu thụ phải được khai bởi module phát.
	bad := mk("pf/party", "kernel", "", "pf.geo.plot.retired/v1", "")
	if _, err := New([]*Manifest{bad, geo2}); err == nil || !strings.Contains(err.Error(), "không khai sự kiện") {
		t.Fatalf("cần lỗi sự kiện không khai, có %v", err)
	}
	if _, err := New([]*Manifest{mk("pf/party", "kernel", "", "pf.nope.x.y/v1", "")}); err == nil || !strings.Contains(err.Error(), "không có module phát") {
		t.Fatalf("cần lỗi module phát không có, có %v", err)
	}
	if EventProducer("mkg.fieldops.roll_count.recorded/v1") != "mkg/fieldops" || EventProducer("x") != "" {
		t.Fatal("EventProducer sai")
	}
}

// TestValidateErrorPathWithPercent: đường dẫn chứa % không làm méo thông báo lỗi.
func TestValidateErrorPathWithPercent(t *testing.T) {
	m := mustParse(t, strings.Replace(validYAML, "kind: kernel", "kind: app", 1), "a%d/module.yaml")
	err := m.Validate()
	if err == nil || strings.Contains(err.Error(), "%!") || !strings.Contains(err.Error(), "a%d/module.yaml: ") {
		t.Fatalf("thông báo lỗi bị méo: %v", err)
	}
}

func TestInvalidManifests(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"kind lạ", "kind: kernel", "kind: app", "kind"},
		{"thiếu zones", "zones: [pf]", "zones: []", "zones bắt buộc"},
		{"zone lạ", "zones: [pf]", "zones: [pf, cloud]", "zone \"cloud\""},
		{"semver sai", "version: 1.2.0", "version: 1.2", "MAJOR.MINOR.PATCH"},
		{"semver số 0 đầu", "version: 1.2.0", "version: 01.2.0", "không hợp lệ"},
		{"semver dấu +", "version: 1.2.0", "version: \"+1.2.0\"", "không hợp lệ"},
		{"tên sai", "name: pf/party", "name: party", "name"},
		{"tên không khớp thư mục", "name: pf/party", "name: pf/geo", "không khớp thư mục"},
		{"aka sai", "aka: [k/orgtree]", "aka: [orgtree]", "aka"},
		{"hosts không phải cap", "aka: [k/orgtree]", "aka: [k/orgtree]\nhosts: [dom/task]", "hosts \"dom/task\""},
		{"hosts lặp", "aka: [k/orgtree]", "aka: [k/orgtree]\nhosts: [cap/task, cap/task]", "lặp"},
		{"hosts trùng aka", "aka: [k/orgtree]", "aka: [cap/task]\nhosts: [cap/task]", "đã là aka"},
		{"event thiếu phiên bản", "pf.party.org.deactivated/v1", "pf.party.org.deactivated", "provides.events"},
		{"ràng buộc hỏng", `">=1.0 <2"`, `">=x"`, "depends pf/masterdata"},
		{"tự phụ thuộc", "pf/masterdata:", "pf/party:", "chính nó"},
		{"enable lạ", "enable: always", "enable: sometimes", "enable"},
		{"kernel phải always", "enable: always", "enable: per_tenant", "kernel"},
		{"schema sai cụm", "owns_schema: [pf_party]", "owns_schema: [mkg_party]", "thuộc cụm mkg"},
		{"schema tiền tố lạ", "owns_schema: [pf_party]", "owns_schema: [party]", "tiền tố"},
		{"publication sai dạng", "publications: []", "publications: [pf_party.org/v1]", "provides.publications"},
		{"view _pub ngoài owns_schema", "publications: []", "publications: [pf_party_pub.org/v1]", "không thuộc owns_schema"},
		{"quyền lặp", "permissions: [registry_admin]", "permissions: [registry_admin, registry_admin]", "lặp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(validYAML, tc.from) {
				t.Fatalf("mẫu không chứa %q", tc.from)
			}
			m := mustParse(t, strings.Replace(validYAML, tc.from, tc.to, 1), "internal/platform/party/module.yaml")
			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("cần lỗi chứa %q, có %v", tc.want, err)
			}
		})
	}
}

func TestRegistryCrossChecks(t *testing.T) {
	party := func(s string) *Manifest {
		return mustParse(t, s, "internal/platform/party/module.yaml")
	}
	md := mustParse(t, masterdataYAML, "internal/platform/masterdata/module.yaml")

	r, err := New([]*Manifest{party(validYAML), md})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Dependents("pf/masterdata"); len(got) != 1 || got[0] != "pf/party" {
		t.Fatalf("Dependents = %v", got)
	}
	if got := r.ByKind(KindCapability); len(got) != 1 || got[0] != "pf/masterdata" {
		t.Fatalf("ByKind = %v", got)
	}
	if o, ok := r.SchemaOwner("pf_party"); !ok || o != "pf/party" {
		t.Fatalf("SchemaOwner = %q %v", o, ok)
	}

	cases := []struct {
		name string
		ms   []*Manifest
		want string
	}{
		{"phụ thuộc không tồn tại", []*Manifest{party(validYAML)}, "không có module này"},
		{"ràng buộc không thỏa", []*Manifest{party(strings.Replace(validYAML, `">=1.0 <2"`, `">=1.4 <2"`, 1)), md}, "không thỏa"},
		{"tên trùng", []*Manifest{party(validYAML), md, party(validYAML)}, "trùng"},
		{"schema hai chủ", []*Manifest{party(validYAML), mustParse(t, strings.Replace(masterdataYAML, "[pf_masterdata]", "[pf_party]", 1), "internal/platform/masterdata/module.yaml")}, "đã thuộc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.ms)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("cần lỗi chứa %q, có %v", tc.want, err)
			}
		})
	}
}

// TestHosts: năng lực chưa tách khai ở hosts (W6.6) — kiểm kê qua Registry.Hosts, Format chỉ ghi khi có, và
// hosts trỏ tới năng lực đã có module (aka) là lỗi.
func TestHosts(t *testing.T) {
	md := mustParse(t, masterdataYAML, "internal/platform/masterdata/module.yaml")
	hosted := strings.Replace(validYAML, "aka: [k/orgtree]", "aka: [k/orgtree]\nhosts: [cap/task, cap/import]", 1)
	p := mustParse(t, hosted, "internal/platform/party/module.yaml")
	r, err := New([]*Manifest{p, md})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Hosts("cap/import"); len(got) != 1 || got[0] != "pf/party" {
		t.Fatalf("Hosts = %v", got)
	}
	if got := r.Hosts("cap/export"); len(got) != 0 {
		t.Fatalf("Hosts(cap/export) = %v", got)
	}
	if out := string(Format(p)); !strings.Contains(out, "aka: [k/orgtree]\nhosts: [cap/import, cap/task]\nkind:") {
		t.Fatalf("Format thiếu hosts đã sắp:\n%s", out)
	}
	if out := string(Format(md)); strings.Contains(out, "hosts:") {
		t.Fatalf("Format ghi hosts rỗng:\n%s", out)
	}
	back := mustParse(t, string(Format(p)), "internal/platform/party/module.yaml")
	if len(back.Hosts) != 2 {
		t.Fatalf("đọc lại hosts = %v", back.Hosts)
	}

	extracted := mustParse(t, strings.Replace(validYAML, "aka: [k/orgtree]", "aka: [k/orgtree]\nhosts: [cap/masterdata]", 1), "internal/platform/party/module.yaml")
	if _, err := New([]*Manifest{extracted, md}); err == nil || !strings.Contains(err.Error(), "đã tách thành module pf/masterdata") {
		t.Fatalf("cần lỗi hosts đã tách, có %v", err)
	}
}

func TestLoadFS(t *testing.T) {
	fsys := fstest.MapFS{
		"internal/platform/party/module.yaml":      {Data: []byte(validYAML)},
		"internal/platform/masterdata/module.yaml": {Data: []byte(masterdataYAML)},
		"internal/shared/db/module.yaml":           {Data: []byte("rác")}, // shared không phải module: bỏ qua
	}
	r, err := LoadFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Names(); len(got) != 2 {
		t.Fatalf("Names = %v", got)
	}
	fsys["internal/mkg/hub/module.yaml"] = &fstest.MapFile{Data: []byte("name: [")}
	if _, err := LoadFS(fsys); err == nil {
		t.Fatal("YAML hỏng phải lỗi")
	}
}

// TestRepoManifests: mọi module.yaml thật trong repo phải hợp lệ (W1.1).
func TestRepoManifests(t *testing.T) {
	r, err := Load("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.Names()); n < 20 {
		t.Fatalf("chỉ thấy %d module", n)
	}
	for _, name := range []string{"pf/accounts", "pf/party", "vault/identity"} {
		if m, ok := r.Get(name); !ok || m.Kind != KindKernel {
			t.Fatalf("%s phải là kernel (ADR-0027)", name)
		}
	}
	// W6.6: cap/import, cap/task, cap/export chưa đủ điều kiện tách (R-MOD-09) — khai hosts ở module đang cài.
	for _, c := range []string{"cap/import", "cap/task", "cap/export"} {
		if len(r.Hosts(c)) == 0 {
			t.Fatalf("%s: không module nào khai hosts (docs/dev/modules.md, W6.6)", c)
		}
	}
}

func TestConstraint(t *testing.T) {
	cases := []struct {
		c, v string
		ok   bool
	}{
		{">=1.0 <2", "1.0.0", true},
		{">=1.0 <2", "1.9.9", true},
		{">=1.0 <2", "2.0.0", false},
		{">=1.2", "1.1.9", false},
		{">1.2.3", "1.2.3", false},
		{"<=1.2", "1.2.0", true},
		{"=1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
	}
	for _, tc := range cases {
		c, err := ParseConstraint(tc.c)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ParseVersion(tc.v)
		if err != nil {
			t.Fatal(err)
		}
		if c.Allows(v) != tc.ok {
			t.Errorf("%q.Allows(%s) != %v", tc.c, tc.v, tc.ok)
		}
	}
	for _, bad := range []string{"", ">=", ">=1.2.3.4", "~1.2", ">=a", ">=+1", ">=1.-0"} {
		if _, err := ParseConstraint(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	if got := DefaultConstraint(Version{2, 3, 1}); got != ">=2.3 <3" {
		t.Fatalf("DefaultConstraint = %q", got)
	}
}

// W6.3 (R-DB-10): mục provides.publications dạng <schema>_pub.<tên>/v<N> là view công bố; tên snake là van.
func TestParsePubView(t *testing.T) {
	v, ok := ParsePubView("pf_cropcycle_pub.season_event/v1")
	if !ok || v.Schema != "pf_cropcycle_pub" || v.Name != "season_event" || v.Version != 1 || v.View() != "pf_cropcycle_pub.v1_season_event" {
		t.Fatalf("%+v %v", v, ok)
	}
	for _, bad := range []string{"hub_capacity", "pf_cropcycle.season_event/v1", "pf_cropcycle_pub.season_event", "pf_x_pub.a/v0"} {
		if _, ok := ParsePubView(bad); ok {
			t.Errorf("%q không phải view công bố", bad)
		}
	}
	if PubRole("pf/cropcycle") != "r_cropcycle_pub" {
		t.Error(PubRole("pf/cropcycle"))
	}
	m := mustParse(t, strings.Replace(strings.Replace(validYAML, "publications: []", "publications: [pf_party_pub.org/v1, valve_x]", 1),
		"owns_schema: [pf_party]", "owns_schema: [pf_party, pf_party_pub]", 1), "internal/platform/party/module.yaml")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

// W3.3 (D-8): sự kiện giữ tên v1 khi chủ dữ liệu chuyển module — module phát là module DUY NHẤT khai sự kiện trong
// provides.events khi module theo tiền tố tên không khai; không ai khai → lỗi như cũ.
func TestConsumesEventMovedProducer(t *testing.T) {
	coop := mustParse(t, `name: pf/coop
kind: domain
zones: [pf]
version: 1.0.0
depends: {}
provides:
  events: [pf.geo.plot.created/v1]
permissions: []
shares: []
owns_schema: [pf_coop]
enable: per_tenant
`, "internal/platform/coop/module.yaml")
	geo := mustParse(t, `name: pf/geo
kind: capability
zones: [pf]
version: 2.0.0
depends: {}
provides:
  events: [pf.geo.facility.recorded/v1]
permissions: []
shares: []
owns_schema: [pf_geo]
enable: per_tenant
`, "internal/platform/geo/module.yaml")
	consumer := func(ev string) *Manifest {
		return mustParse(t, `name: pf/metrics
kind: capability
zones: [pf]
version: 1.0.0
depends: {}
consumes:
  events: [`+ev+`]
permissions: []
shares: []
owns_schema: [pf_metrics]
enable: per_tenant
`, "internal/platform/metrics/module.yaml")
	}
	for _, ev := range []string{"pf.geo.plot.created/v1", "pf.geo.facility.recorded/v1"} {
		if _, err := New([]*Manifest{coop, geo, consumer(ev)}); err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
	}
	if _, err := New([]*Manifest{coop, geo, consumer("pf.geo.road.recorded/v1")}); err == nil ||
		!strings.Contains(err.Error(), "pf/geo không khai sự kiện này") {
		t.Fatalf("sự kiện không ai khai phải lỗi: %v", err)
	}
}
