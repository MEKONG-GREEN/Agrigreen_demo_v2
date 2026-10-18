package modreg

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A source namespace move must retain database identities and reject duplicate owners.
func TestApplicationNamespaceRetainsDatabaseIdentity(t *testing.T) {
	for _, module := range []string{"extension", "price", "hrm", "contact"} {
		dir := "internal/kn/" + module
		name := "pf/" + module
		if got := NameForDir(dir); got != name {
			t.Fatalf("NameForDir(%s) = %s", dir, got)
		}
		data := "name: " + name + "\nkind: domain\nzones: [pf]\nversion: 1.0.0\nenable: per_tenant\ndepends: {}\nowns_schema: [pf_" + module + "]\n"
		fsys := fstest.MapFS{dir + "/module.yaml": {Data: []byte(data)}}
		r, err := LoadFS(fsys)
		if err != nil {
			t.Fatal(err)
		}
		m, ok := r.Get(name)
		if !ok || m.Dir() != dir || DBRole(name) != "r_"+module {
			t.Fatalf("identity lost: %+v", m)
		}
		if owner, ok := r.SchemaOwner("pf_" + module); !ok || owner != name {
			t.Fatalf("schema owner = %q %v", owner, ok)
		}
		fsys["internal/platform/"+module+"/module.yaml"] = &fstest.MapFile{Data: []byte(data)}
		if _, err := LoadFS(fsys); err == nil || !strings.Contains(err.Error(), "trùng") {
			t.Fatalf("duplicate owner accepted: %v", err)
		}
	}
}
