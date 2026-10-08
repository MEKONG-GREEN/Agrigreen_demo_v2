package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// integrationFixture chép contracts/integration và contracts/publication của repo vào một gốc tạm
// để từng ca kiểm sửa một chỗ và xem contractcheck bắt được.
func integrationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range []string{"integration", "publication", "dataproduct"} {
		src := filepath.Join("..", "..", "contracts", sub)
		err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join("..", ".."), p)
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			write(t, root, rel, string(raw))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Hợp đồng van tham chiếu sự kiện: chép schema sự kiện tương ứng.
	for _, ev := range []string{"mkg.hub.facility.published", "mkg.hub.capacity.published", "mkg.fieldops.collection_schedule.published",
		"mkg.fieldops.htx_collection_statement.published", "mkg.measurement.mrv_collection_evidence.published"} {
		rel := filepath.Join("contracts", "events", ev+".v1.json")
		raw, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, rel, string(raw))
	}
	return root
}

func integrationProblems(t *testing.T, root, base string) []string {
	t.Helper()
	p, err := Check(root, base)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range p {
		if strings.Contains(filepath.ToSlash(x), "contracts/integration/") {
			out = append(out, x)
		}
	}
	return out
}

func edit(t *testing.T, root, rel, old, repl string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s không có %q", rel, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), old, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantProblem(t *testing.T, p []string, sub string) {
	t.Helper()
	for _, x := range p {
		if strings.Contains(x, sub) {
			return
		}
	}
	t.Fatalf("phải có vấn đề chứa %q, có: %v", sub, p)
}

func TestIntegrationFixtureClean_W51(t *testing.T) {
	if p := integrationProblems(t, integrationFixture(t), ""); len(p) != 0 {
		t.Fatalf("hợp đồng tích hợp của repo phải sạch: %v", p)
	}
}

// Mỗi ca: sửa một chỗ → contractcheck phải bắt.
func TestIntegrationViolations_W51(t *testing.T) {
	const (
		harvest  = "contracts/integration/feeds/harvest_progress_by_commune.v1.json"
		harvestX = "contracts/integration/examples/harvest_progress_by_commune.v1.json"
		roll     = "contracts/integration/ingest/roll_count_statement.v1.json"
		rollX    = "contracts/integration/examples/roll_count_statement.v1.json"
		sched    = "contracts/integration/ingest/collection_schedule.v1.json"
		admin    = "contracts/integration/feeds/admin_units.v1.json"
	)
	cases := []struct {
		name, file, old, repl, want string
	}{
		{"mẫu có khóa ngoài hợp đồng", rollX, `"rolls": 46,`, `"rolls": 46, "price_vnd": "1000",`, "price_vnd: trường ngoài hợp đồng"},
		{"mẫu sai kiểu", rollX, `"rolls": 46,`, `"rolls": "46",`, "rolls: kiểu string"},
		{"entity_key mẫu lệch bản ghi", harvestX, `"entity_key": "89123:2026-HT"`, `"entity_key": "89123"`, "khác khóa dựng từ bản ghi"},
		{"max_age mẫu khác hợp đồng", harvestX, `"max_age": "PT15M"`, `"max_age": "PT1H"`, "khác freshness.max_age"},
		{"k_used dưới k", harvestX, `"k_used": 10,`, `"k_used": 3,`, "dataset.k_used phải có và ≥ k_default"},
		{"phong bì có khóa lạ", harvestX, `"has_more": false,`, `"has_more": false, "debug": true,`, "debug: trường ngoài hợp đồng"},
		{"k_default < 2", harvest, `"k_default": 10,`, `"k_default": 1,`, "aggregation.k_default phải ≥ 2"},
		{"aggregate thiếu aggregation", harvest, `"grant_class": "aggregate",`, `"grant_class": "partner", "grant_kind": "x",`, "grant_class aggregate khi và chỉ khi"},
		{"tắt ẩn ô bù", harvest, `"complementary_suppression": true,`, `"complementary_suppression": false,`, "ẩn ô bù"},
		{"retention < 30", harvest, `"retention_days": 30,`, `"retention_days": 7,`, "retention_days phải ≥ 30"},
		{"approved còn pending", harvest, `"status": "proposed",`, `"status": "approved",`, "approved nhưng còn pending"},
		{"tự phê duyệt", harvest, `"approved_by": "data-steward-don-vi-van-hanh",`, `"approved_by": "PF-CROPCYCLE-MODULE-OWNER",`, "approved_by phải khác proposed_by"},
		{"entity_key không có trường", harvest, `"entity_key": ["area_code", "season_code"],`, `"entity_key": ["area_code", "commune_ref"],`, "entity_key commune_ref không có"},
		{"song phương vào tổng hợp", roll, `"grant_class": "bilateral",`, `"grant_class": "bilateral", "aggregation": {"k_param":"x","k_default":10,"k_status":"x","unit":"plot","suppression":"omit","complementary_suppression":true,"hysteresis":2},`, "song phương không bao giờ vào tổng hợp"},
		{"song phương thiếu hai bên", roll, `"author_side": "a",`, `"author_side": "c",`, "author_side phải là a | b"},
		{"pair_key không có trường", roll, `"pair_key": ["htx_ref"],`, `"pair_key": ["buyer_ref"],`, "bilateral.pair_key buyer_ref"},
		{"route nạp sai", roll, `"route": "POST /v1/batches/roll_count_statement",`, `"route": "POST /v1/partner/rolls",`, "ingest.route phải là"},
		{"replaces không có thật", roll, `"replaces": ["publication/htx_collection_statement"],`, `"replaces": ["publication/khong_co"],`, "replaces publication/khong_co"},
		{"publication không có", sched, `"publication": "collection_schedule",`, `"publication": "khong_co_van",`, "khong_co_van"},
		{"publication kèm properties", sched, `"publication": "collection_schedule",`, `"publication": "collection_schedule", "type": "object", "properties": {"x": {"type": "string"}},`, "không được có properties riêng"},
		{"valid_to không null được", admin, `"valid_to":    { "type": ["string", "null"], "format": "date" },`, `"valid_to":    { "type": "string", "format": "date" },`, "valid_to phải là trường date có thể null"},
		{"khóa lạ trong hợp đồng", admin, `"personal_data": false,`, `"personal_data": false, "unknown_meta": 1,`, "unknown field"},
		{"name khác tên tệp", admin, `"name": "admin_units",`, `"name": "admin_unit_list",`, "phải trùng tên tệp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := integrationFixture(t)
			edit(t, root, c.file, c.old, c.repl)
			wantProblem(t, integrationProblems(t, root, ""), c.want)
		})
	}
}

func TestIntegrationExamplesRequired_W51(t *testing.T) {
	root := integrationFixture(t)
	if err := os.Rename(filepath.Join(root, "contracts/integration/examples/admin_units.v1.json"),
		filepath.Join(root, "contracts/integration/examples/admin_units.v9.json")); err != nil {
		t.Fatal(err)
	}
	p := integrationProblems(t, root, "")
	wantProblem(t, p, "thiếu mẫu examples/admin_units.v1.json")
	wantProblem(t, p, "mẫu không có hợp đồng admin_units.v9")
}

func TestIntegrationKindMustMatchDir_W51(t *testing.T) {
	root := integrationFixture(t)
	src := filepath.Join(root, "contracts/integration/feeds/admin_units.v1.json")
	if err := os.Rename(src, filepath.Join(root, "contracts/integration/ingest/admin_units.v1.json")); err != nil {
		t.Fatal(err)
	}
	wantProblem(t, integrationProblems(t, root, ""), `kind "feed" nằm trong thư mục ingest/`)
}

// R-INT-08: so với nhánh gốc, xóa trường / đổi kiểu / thêm required phải ra phiên bản mới.
func TestIntegrationCompatAgainstBase_W51(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("không có git")
	}
	root := integrationFixture(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.email=t@example.invalid", "-c", "user.name=t", "add", "-A")
	run("-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "-m", "base")
	if p := integrationProblems(t, root, "HEAD"); len(p) != 0 {
		t.Fatalf("không đổi gì phải sạch: %v", p)
	}
	feed := "contracts/integration/feeds/season_harvest_dates.v1.json"
	edit(t, root, feed, `"season_year":           { "type": "integer" },`, `"season_year":           { "type": "string" },`)
	p := integrationProblems(t, root, "HEAD")
	wantProblem(t, p, "đổi kiểu trường season_year")
}
