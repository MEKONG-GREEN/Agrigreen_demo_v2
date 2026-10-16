package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestApplicationNamespaceDiscoveryAndWiring(t *testing.T) {
	root := t.TempDir()
	dir := "internal/kn/price"
	path := filepath.Join(root, filepath.FromSlash(dir))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "module.go"), []byte("package price\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err := moduleDirs(root)
	if err != nil || !slices.Equal(dirs, []string{dir}) {
		t.Fatalf("discovery = %v, %v", dirs, err)
	}
	for _, suffix := range []string{"", "/app", "/port", "/port/history"} {
		got := moduleOf(modulePath + "/" + dir + suffix)
		if got != "pf/price" {
			t.Fatalf("moduleOf(%s) = %q", suffix, got)
		}
		want := suffix == "" || suffix == "/app"
		if inProcess(dir+suffix) != want {
			t.Fatalf("wiring(%s)", suffix)
		}
	}
	wired, err := wiredModules("../..", []string{"api-platform"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pf/extension", "pf/price", "pf/hrm", "pf/contact"} {
		if !slices.Contains(wired["api-platform"], name) {
			t.Errorf("assembly lost %s", name)
		}
	}
}
