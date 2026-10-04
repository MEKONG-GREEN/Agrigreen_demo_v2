package modreg

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// catalog_gen.go khớp module.yaml của repo (modgen -check cũng kiểm).
func TestCatalogMatchesManifests(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := FormatCatalog(reg.Modules())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(CatalogFile)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("catalog_gen.go lỗi thời — chạy go run ./tools/modgen")
	}
	if len(Catalog()) != len(reg.Modules()) {
		t.Fatalf("Catalog có %d mục, registry %d", len(Catalog()), len(reg.Modules()))
	}
}

func TestPerTenantAndRoleBlocked(t *testing.T) {
	if PerTenant("pf/party") || PerTenant("mkg/hub") || PerTenant("pf/khong_co") {
		t.Fatal("lõi/mkg/module lạ không phải per_tenant")
	}
	if !PerTenant("pf/geo") || !PerTenant("pf/metering") {
		t.Fatal("pf/geo, pf/metering là per_tenant")
	}
	off := func(mods ...string) func(string) bool {
		return func(m string) bool { return slices.Contains(mods, m) }
	}
	// metering_viewer chỉ do pf/metering khai.
	if b, mods := RoleBlocked("metering_viewer", off("pf/metering")); !b || !slices.Equal(mods, []string{"pf/metering"}) {
		t.Fatalf("metering_viewer: %v %v", b, mods)
	}
	if b, _ := RoleBlocked("metering_viewer", off()); b {
		t.Fatal("module bật thì cấp được")
	}
	// coop_manager do pf/party (lõi) cùng khai → không bao giờ bị chặn.
	if b, _ := RoleBlocked("coop_manager", off("pf/cropcycle", "pf/geo")); b {
		t.Fatal("vai có module lõi khai không bị chặn")
	}
	// plot_editor chỉ pf/geo; geo_admin chỉ pf/geo.
	if b, _ := RoleBlocked("plot_editor", off("pf/geo")); !b {
		t.Fatal("plot_editor phải bị chặn khi pf/geo tắt")
	}
	// Vai không module nào khai (platform_admin) cấp được.
	if b, _ := RoleBlocked("platform_admin", off("pf/geo")); b {
		t.Fatal("platform_admin không thuộc module nào")
	}
	if e, ok := Entry("pf/party"); !ok || e.Kind != KindKernel || e.Enable != EnableAlways {
		t.Fatalf("Entry pf/party: %+v", e)
	}
}
