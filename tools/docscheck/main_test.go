package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, p, body string) {
	t.Helper()
	f := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantProblems(t *testing.T, root string, subs ...string) {
	t.Helper()
	p, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != len(subs) {
		t.Fatalf("muốn %d lỗi %q, nhận %d: %v", len(subs), subs, len(p), p)
	}
	for _, s := range subs {
		found := false
		for _, x := range p {
			found = found || strings.Contains(x, s)
		}
		if !found {
			t.Errorf("thiếu lỗi chứa %q trong %v", s, p)
		}
	}
}

const readme = `# ADR

| # | Quyết định | Trạng thái |
|---|---|---|
| 0001 | [A](0001-a.md) | x |
| 0050 | [HRM](0050-dom-hrm.md) | x |

## Số đã giữ chỗ

| # | Dành cho | Nguồn |
|---|---|---|
| 0051 | cap/export | |
`

func TestRepoIsClean_WHB(t *testing.T) {
	p, err := Check(filepath.Join("..", ".."))
	if err != nil || len(p) != 0 {
		t.Fatalf("repo phải sạch: %v %v", p, err)
	}
}

// Bài học 0050: cap/export giữ 0050 trong khi pf/hrm viết ADR-0050.
func TestADRRegistry_WHB(t *testing.T) {
	root := t.TempDir()
	put(t, root, "docs/adr/0001-a.md", "# A")
	put(t, root, "docs/adr/0050-dom-hrm.md", "# HRM")
	put(t, root, "docs/adr/README.md", readme)
	wantProblems(t, root)

	put(t, root, "docs/adr/README.md", strings.Replace(readme, "| 0051 |", "| 0050 |", 1))
	wantProblems(t, root, "số 0050 đang giữ chỗ nhưng đã có ADR 0050-dom-hrm.md")

	put(t, root, "docs/adr/README.md", readme)
	put(t, root, "docs/adr/0050-cap-export.md", "# export")
	wantProblems(t, root, "trùng số ADR 0050", "dòng 0050 trỏ tới 0050-dom-hrm.md, tệp là")

	_ = os.Remove(filepath.Join(root, "docs/adr/0050-cap-export.md"))
	put(t, root, "docs/adr/0002-b.md", "# B")
	put(t, root, "docs/adr/README.md", strings.Replace(readme, "| 0001 | [A](0001-a.md) | x |", "| 0001 | [A](0001-a.md) | x |\n| 0003 | [C](0003-c.md) | x |", 1))
	wantProblems(t, root, "0002-b.md: thiếu dòng trong bảng chỉ mục", "dòng 0003 trỏ tới 0003-c.md — không có tệp")
}

func TestPerPackageDocs_WHB(t *testing.T) {
	root := t.TempDir()
	put(t, root, "docs/adr/README.md", "# ADR\n")
	put(t, root, "docs/adr/0050-x.md", "# x")
	put(t, root, "docs/kn/gia-dinh/README.md", "# quy ước")
	put(t, root, "docs/kn/gia-dinh/W11.42.md", "## W11.42 — Biểu kỳ\n\n| a | [ADR](../../adr/0050-x.md) |\n")
	put(t, root, "docs/roadmap.d/kn-2026-10/W11.42.md", "- [x] W11.42 (13/10/2026): xong — [giả định](../../kn/gia-dinh/W11.42.md)\n")
	put(t, root, "docs/errors.md", "| HTTP | code |\n|---|---|\n| 409 | `price.old` | x |\n")
	put(t, root, "docs/errors/pf-price.md", "### pf/price\n\n| HTTP | code | Khi nào |\n|---|---|---|\n| 409 | `price.new` | x |\n")
	wantProblems(t, root, "docs/adr/0050-x.md: thiếu dòng")
	put(t, root, "docs/adr/README.md", "| 0050 | [x](0050-x.md) | y |\n")
	wantProblems(t, root)

	put(t, root, "docs/kn/gia-dinh/W11.43.md", "## W11.42 — sai gói\n\n## W11.43 — đúng\n[x](../adr/0050-x.md)\n")
	wantProblems(t, root, `dòng đầu phải là "## W11.43`, "không thuộc gói W11.43", "liên kết hỏng ../adr/0050-x.md")
	_ = os.Remove(filepath.Join(root, "docs/kn/gia-dinh/W11.43.md"))

	put(t, root, "docs/kn/gia-dinh/KHTCQA.md", "## KHTCQA — Ma trận nghiệm thu\n")
	put(t, root, "docs/roadmap.d/kn-2026-10/KHTCQA.md", "- [x] KHTCQA (13/10/2026): xong\n")
	wantProblems(t, root)
	put(t, root, "docs/kn/gia-dinh/khtc_qa.md", "## khtc_qa — chữ thường\n")
	wantProblems(t, root, "tên tệp phải là mã gói")
	_ = os.Remove(filepath.Join(root, "docs/kn/gia-dinh/khtc_qa.md"))

	put(t, root, "docs/roadmap.d/kn-2026-10/W11.76.md", "- [x] W11.75: dòng của gói khác\n")
	wantProblems(t, root, `mỗi dòng phải là "- [ ] W11.76…"`)
	_ = os.Remove(filepath.Join(root, "docs/roadmap.d/kn-2026-10/W11.76.md"))

	put(t, root, "docs/errors/pf-hrm.md", "### pf/hrm\n\n| 409 | `price.new` | x |\n| 409 | `price.old` | x |\n")
	wantProblems(t, root, "mã price.new đã có ở docs/errors/pf-hrm.md", "mã price.old đã có ở docs/errors.md")
	put(t, root, "docs/errors/pf-hrm.md", "| 409 | `hrm.x` | x |\n")
	wantProblems(t, root, "thiếu tiêu đề")
}
