package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// repoModulePath là đường dẫn Go module thật (TestBoundary đổi biến modulePath sang example.com/ag).
const repoModulePath = "github.com/MEKONG-GREEN/Agrigreen_demo_v2"

// TestModGraphRepo: repo thật qua agmodgraph với tệp ngoại lệ — không vi phạm mới, không ngoại lệ lỗi thời.
func TestModGraphRepo(t *testing.T) {
	res, err := checkModGraph(filepath.Join("..", ".."), repoModulePath, "tools/archlint/modgraph_exceptions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Violations {
		t.Errorf("vi phạm mới: %s", v)
	}
	for _, e := range res.Stale {
		t.Errorf("%s", staleMsg(e))
	}
	for _, s := range res.StaleOps {
		t.Errorf("ops_schemas lỗi thời: %s", s)
	}
	if res.Excepted == 0 {
		t.Error("không vi phạm nào khớp ngoại lệ — tệp ngoại lệ có được đọc không?")
	}
}

// TestModGraphCases chạy các ca trong testdata/modgraph/*.txtar. Mỗi tệp là một repo giả
// (module.yaml, .go, migrations/, exceptions.yaml); phần chú thích liệt kê kết quả mong đợi:
//
//	violation <rule> <from> <to>
//	stale <rule> <from> <to>
//	staleops <schema>
//	error <chuỗi con của lỗi>
func TestModGraphCases(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "modgraph", "*.txtar"))
	if err != nil || len(files) == 0 {
		t.Fatalf("không có ca kiểm thử: %v", err)
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".txtar"), func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, af := range ar.Files {
				p := filepath.Join(dir, filepath.FromSlash(af.Name))
				if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, af.Data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var want []string
			wantErr := ""
			for _, line := range strings.Split(string(ar.Comment), "\n") {
				line = strings.TrimSpace(line)
				if s, ok := strings.CutPrefix(line, "error "); ok {
					wantErr = s
				} else if line != "" && !strings.HasPrefix(line, "#") {
					want = append(want, strings.Join(strings.Fields(line), " "))
				}
			}

			res, err := checkModGraph(dir, "example.com/ag", "exceptions.yaml")
			if wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("lỗi = %v, muốn chứa %q", err, wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, v := range res.Violations {
				got = append(got, "violation "+v.Rule+" "+v.From+" "+v.To)
			}
			for _, e := range res.Stale {
				got = append(got, "stale "+e.Rule+" "+e.From+" "+e.To)
			}
			for _, s := range res.StaleOps {
				schema, _, _ := strings.Cut(s, ":")
				got = append(got, "staleops "+schema)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("kết quả:\n  %s\nmuốn:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
				for _, v := range res.Violations {
					t.Logf("%s", v)
				}
			}
		})
	}
}
