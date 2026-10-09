package migrator

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

var testMods = []Module{
	{Name: "pf/kref", Kind: modreg.KindKernel, OwnsSchema: []string{"k_ref"}},
	{Name: "pf/party", Kind: modreg.KindKernel, Depends: []string{"pf/kref", "pf/sync"}, OwnsSchema: []string{"pf_party"}},
	{Name: "pf/sync", Kind: modreg.KindCapability, Depends: []string{"pf/party"}, OwnsSchema: []string{"pf_sync"}},
	{Name: "pf/notify", Kind: modreg.KindCapability, Depends: []string{"pf/party"}, OwnsSchema: []string{"pf_notify"}},
	{Name: "pf/geo", Kind: modreg.KindCapability, OwnsSchema: []string{"pf_geo", "pf_geo_pub"}},
	{Name: "pf/cropcycle", Kind: modreg.KindDomain, Depends: []string{"pf/party"}, OwnsSchema: []string{"pf_cropcycle"}},
}

func keys(fs []File) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key())
	}
	return out
}

// Ranks: chu trình depends (party ↔ sync) gộp một hạng; module phụ thuộc đứng sau.
func TestRanks_W71(t *testing.T) {
	r := Ranks(testMods)
	if r["pf/kref"] != 0 || r["pf/geo"] != 0 {
		t.Errorf("gốc phải hạng 0: %v", r)
	}
	if r["pf/party"] != r["pf/sync"] || r["pf/party"] != 1 {
		t.Errorf("chu trình party↔sync phải cùng hạng 1: %v", r)
	}
	if r["pf/notify"] != 2 || r["pf/cropcycle"] != 2 {
		t.Errorf("module phụ thuộc party phải hạng 2: %v", r)
	}
}

// Gắn module: tiền tố tên, suy từ schema đụng, còn lại là tệp toàn cụm (module giả).
func TestAttribution_W71(t *testing.T) {
	fsys := fstest.MapFS{
		"pf/000001_pf_party_base.up.sql":      {Data: []byte("CREATE SCHEMA pf_party; CREATE TABLE pf_party.x (id int);")},
		"pf/000002_misc.up.sql":               {Data: []byte("CREATE TABLE pf_geo.y (id int); -- pf_party.z\n")},
		"pf/000003_pf_module_roles.up.sql":    {Data: []byte("GRANT USAGE ON SCHEMA pf_party TO r_party; CREATE TABLE pf_sync.a (id int); CREATE TABLE pf_party.b (id int);")},
		"pf/000004_pf_kref_projection.up.sql": {Data: []byte("CREATE SCHEMA k_ref;")},
		"shared/000001_baseline.up.sql":       {Data: []byte("CREATE SCHEMA shared;")},
	}
	fs, err := LoadDir(fsys, "pf", testMods)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"pf/000001_pf_party_base.up.sql":      "pf/party",
		"pf/000002_misc.up.sql":               "pf/geo", // chú thích không tính
		"pf/000003_pf_module_roles.up.sql":    "pf",     // nhiều module → toàn cụm
		"pf/000004_pf_kref_projection.up.sql": "pf/kref",
	}
	for _, f := range fs {
		if want[f.Key()] != f.Owner {
			t.Errorf("%s: chủ %q, muốn %q", f.Key(), f.Owner, want[f.Key()])
		}
		if (f.Owner == "pf") != f.Pseudo {
			t.Errorf("%s: Pseudo=%v", f.Key(), f.Pseudo)
		}
	}
	sh, err := LoadDir(fsys, SharedDir, testMods)
	if err != nil || len(sh) != 1 || sh[0].Owner != SharedDir || !sh[0].Pseudo {
		t.Fatalf("shared/: %+v %v", sh, err)
	}
	if _, err := LoadDir(fstest.MapFS{"pf/1_x.up.sql": {}}, "pf", testMods); err == nil {
		t.Error("tên tệp sai phải lỗi")
	}
}

// Thứ tự: topo module giữa hai rào chắn, nhưng giữ ràng buộc lịch sử (cùng module theo số, rào chắn toàn cụm,
// phụ thuộc có số nhỏ hơn, tham chiếu schema).
func TestOrder_W71(t *testing.T) {
	fsys := fstest.MapFS{
		"pf/000010_pf_cropcycle_a.up.sql": {Data: []byte("CREATE SCHEMA pf_cropcycle;")},
		"pf/000011_pf_geo_a.up.sql":       {Data: []byte("CREATE SCHEMA pf_geo;")},
		"pf/000012_pf_party_a.up.sql":     {Data: []byte("CREATE SCHEMA pf_party;")},
		"pf/000013_pf_cropcycle_b.up.sql": {Data: []byte("CREATE TABLE pf_cropcycle.t (id int);")},
		// toàn cụm: rào chắn
		"pf/000014_pf_roles.up.sql":    {Data: []byte("CREATE TABLE pf_party.r (id int); CREATE TABLE pf_geo.r (id int);")},
		"pf/000015_pf_notify_a.up.sql": {Data: []byte("CREATE SCHEMA pf_notify;")},
		"pf/000016_pf_geo_b.up.sql":    {Data: []byte("CREATE TABLE pf_geo.v AS SELECT * FROM pf_notify.x;")}, // tham chiếu pf_notify → sau 15
		"pf/000017_pf_kref_a.up.sql":   {Data: []byte("CREATE SCHEMA k_ref;")},
		"pf/000018_pf_party_b.up.sql":  {Data: []byte("CREATE TABLE pf_party.p (id int);")},
		"pf/000019_pf_notify_b.up.sql": {Data: []byte("CREATE TABLE pf_notify.n (id int);")},
	}
	files, err := LoadDir(fsys, "pf", testMods)
	if err != nil {
		t.Fatal(err)
	}
	got := keys(Order(files, testMods))
	want := []string{
		// trước rào chắn 14: geo (hạng 0) trước party (hạng 1) trước cropcycle (hạng 2) — nhưng cropcycle 10 đứng
		// trước 13 (cùng module); party 12 < cropcycle 13 và cropcycle phụ thuộc party nên 12 trước 13.
		"pf/000011_pf_geo_a.up.sql",
		"pf/000012_pf_party_a.up.sql",
		"pf/000010_pf_cropcycle_a.up.sql",
		"pf/000013_pf_cropcycle_b.up.sql",
		"pf/000014_pf_roles.up.sql",
		// sau rào chắn: kref (hạng 0, kernel) trước geo 16? geo 16 tham chiếu pf_notify (15) nên chờ 15.
		"pf/000017_pf_kref_a.up.sql",
		"pf/000018_pf_party_b.up.sql",
		"pf/000015_pf_notify_a.up.sql",
		"pf/000016_pf_geo_b.up.sql",
		"pf/000019_pf_notify_b.up.sql",
	}
	if !slices.Equal(got, want) {
		t.Errorf("thứ tự:\n got %v\nwant %v", got, want)
	}
}

// Kế hoạch thật của mọi cụm dựng được; thứ tự giữ ràng buộc "cùng module theo số" và rào chắn toàn cụm.
func TestRealPlan_W71(t *testing.T) {
	mods := CatalogModules()
	for _, c := range migrations.Clusters {
		p, err := BuildPlan(migrations.FS, c, mods)
		if err != nil {
			t.Fatal(err)
		}
		last := map[string]int64{}
		var barrier int64
		for i, f := range p.Files {
			if i > 0 && p.Files[i-1].Dir == SharedDir && f.Dir != SharedDir {
				barrier = 0
			}
			if f.Version <= last[f.Dir+"|"+f.Owner] {
				t.Errorf("%s: %s đứng sau tệp số lớn hơn của cùng module", c, f.Key())
			}
			last[f.Dir+"|"+f.Owner] = f.Version
			if f.Pseudo {
				if f.Version < barrier {
					t.Errorf("%s: rào chắn %s không theo số", c, f.Key())
				}
				barrier = f.Version
			} else if f.Version < barrier {
				t.Errorf("%s: %s đứng sau rào chắn số %d", c, f.Key(), barrier)
			}
			t.Logf("%s %-55s %s", c, f.Key(), f.Owner)
		}
	}
}

// Chờ áp: vắng trong sổ — kể cả số nhỏ hơn số đã áp; tệp v1 đã áp được ghi sổ (không chạy lại).
func TestReconcile_W71(t *testing.T) {
	f := func(dir string, v int64) File {
		return File{Dir: dir, Name: "x" + string(rune('a'+v)) + ".up.sql", Version: v, Checksum: "c"}
	}
	plan := []File{f("shared", 1), f("shared", 2), f("pf", 1), f("pf", 2), f("pf", 3), f("pf", 4), f("pf", 5)}
	row := func(x File) Row { return Row{Dir: x.Dir, File: x.Name, Version: x.Version, Checksum: x.Checksum} }

	// Lần chuyển đầu: v1 ở shared 2, pf 4 → ghi sổ tất cả ≤ phiên bản, áp pf 5.
	r := Reconcile(plan, nil, map[string]V1State{"shared": {Exists: true, Version: 2}, "pf": {Exists: true, Version: 4}})
	if len(r.Import) != 6 || len(r.Apply) != 1 || r.Apply[0].Version != 5 || r.Blocking() {
		t.Fatalf("chuyển đầu: %+v", r)
	}
	// Đã chuyển; tệp pf 2 thêm sau (ngoài thứ tự, số < 4 đã áp) → áp, không bị bỏ qua.
	rows := []Row{row(plan[0]), row(plan[1]), row(plan[2]), row(plan[4]), row(plan[5])}
	v1 := map[string]V1State{"shared": {Exists: true, Version: 2}, "pf": {Exists: true, Version: 4}}
	r = Reconcile(plan, rows, v1)
	if got := keys(r.Apply); !slices.Equal(got, []string{"pf/xc.up.sql", "pf/xf.up.sql"}) || len(r.Import) != 0 {
		t.Fatalf("ngoài thứ tự: apply %v import %v", got, keys(r.Import))
	}
	if s := SkippedByV1(r.Apply, v1); len(s) != 1 || s[0].Version != 2 {
		t.Errorf("SkippedByV1: %v", keys(s))
	}
	// v1 chạy sau v2 (pf 5 lên phiên bản 5): ghi sổ 5, vẫn áp pf 2.
	v1["pf"] = V1State{Exists: true, Version: 5}
	r = Reconcile(plan, rows, v1)
	if !slices.Equal(keys(r.Import), []string{"pf/xf.up.sql"}) || !slices.Equal(keys(r.Apply), []string{"pf/xc.up.sql"}) {
		t.Errorf("v1 sau v2: import %v apply %v", keys(r.Import), keys(r.Apply))
	}
	// Lệch tổng kiểm, dòng sổ lạ, ghi trùng, golang-migrate bẩn → chặn.
	bad := append(slices.Clone(rows), Row{Dir: "pf", File: "gone.up.sql", Version: 9, Checksum: "c"}, row(plan[0]))
	bad[3].Checksum = "khác"
	r = Reconcile(plan, bad, map[string]V1State{"pf": {Exists: true, Version: 4, Dirty: true}})
	if len(r.Drift) != 1 || len(r.Unknown) != 1 || len(r.Duplicate) != 1 || len(r.Problems) != 1 || r.Err() == nil {
		t.Errorf("chặn: %+v", r)
	}
	// Database mới hoàn toàn: áp hết theo thứ tự.
	r = Reconcile(plan, nil, map[string]V1State{})
	if len(r.Apply) != len(plan) || len(r.Import) != 0 {
		t.Errorf("mới: %+v", r)
	}
}

func TestChecksumIgnoresCRLF_W71(t *testing.T) {
	if Checksum("a\r\nb\n") != Checksum("a\nb\n") || Checksum("a") == Checksum("b") {
		t.Error("tổng kiểm")
	}
}
