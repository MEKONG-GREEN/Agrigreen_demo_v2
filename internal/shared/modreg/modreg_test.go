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
		{"tên sai", "name: pf/party", "name: party", "name"},
		{"tên không khớp thư mục", "name: pf/party", "name: pf/geo", "không khớp thư mục"},
		{"aka sai", "aka: [k/orgtree]", "aka: [orgtree]", "aka"},
		{"event thiếu phiên bản", "pf.party.org.deactivated/v1", "pf.party.org.deactivated", "provides.events"},
		{"ràng buộc hỏng", `">=1.0 <2"`, `">=x"`, "depends pf/masterdata"},
		{"tự phụ thuộc", "pf/masterdata:", "pf/party:", "chính nó"},
		{"enable lạ", "enable: always", "enable: sometimes", "enable"},
		{"kernel phải always", "enable: always", "enable: per_tenant", "kernel"},
		{"schema sai cụm", "owns_schema: [pf_party]", "owns_schema: [mkg_party]", "thuộc cụm mkg"},
		{"schema tiền tố lạ", "owns_schema: [pf_party]", "owns_schema: [party]", "tiền tố"},
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
	for _, bad := range []string{"", ">=", ">=1.2.3.4", "~1.2", ">=a"} {
		if _, err := ParseConstraint(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	if got := DefaultConstraint(Version{2, 3, 1}); got != ">=2.3 <3" {
		t.Fatalf("DefaultConstraint = %q", got)
	}
}
