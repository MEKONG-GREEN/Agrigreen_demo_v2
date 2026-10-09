package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fileOwners gắn mọi tệp của mọi thư mục cụm cho module ("<thư mục>/<tên>" → module) như bộ chạy v2 (W7.1).
// Thư mục có tên tệp sai thì bỏ qua ở đây — kiểm tên tệp báo lỗi riêng.
func fileOwners(root string, entries []os.DirEntry) (map[string]string, error) {
	mods := migrator.CatalogModules()
	fsys := os.DirFS(root)
	out := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := migrator.LoadDir(fsys, e.Name(), mods)
		if err != nil {
			continue
		}
		for _, f := range files {
			out[f.Key()] = f.Owner
		}
	}
	return out, nil
}

// printModules in kế hoạch v2 của mỗi cụm theo module: module "" → bảng tóm tắt mọi module; khác rỗng → thứ tự áp
// các tệp của module đó (theo kế hoạch v2 của từng cụm có tệp của nó).
func printModules(w io.Writer, root, module string) error {
	mods := migrator.CatalogModules()
	fsys := os.DirFS(root)
	for _, c := range migrations.Clusters {
		p, err := migrator.BuildPlan(fsys, c, mods)
		if err != nil {
			return err
		}
		type sum struct {
			n           int
			first, last int64
			ledger      string
		}
		byMod := map[string]*sum{}
		var order []string
		for i, f := range p.Files {
			if f.Dir == migrator.SharedDir && c != migrations.Clusters[0] {
				continue // shared/ giống nhau ở mọi cụm — in một lần
			}
			if module != "" {
				if f.Owner == module {
					if _, err := fmt.Fprintf(w, "%s\t%3d\t%s\n", c, i+1, f.Key()); err != nil {
						return err
					}
				}
				continue
			}
			k := f.Owner
			s, ok := byMod[k]
			if !ok {
				ledger := "shared"
				if len(f.LedgerSchemas) > 0 {
					ledger = strings.Join(f.LedgerSchemas, "|") // đích khi dời sổ vào schema module (bước 2)
				}
				s = &sum{first: f.Version, ledger: ledger}
				byMod[k] = s
				order = append(order, k)
			}
			s.n++
			s.first, s.last = min(s.first, f.Version), max(s.last, f.Version)
		}
		sort.Strings(order)
		for _, k := range order {
			s := byMod[k]
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d tệp\t%06d–%06d\tschema %s (sổ: shared.%s)\n", c, k, s.n, s.first, s.last, s.ledger, migrator.LedgerTable); err != nil {
				return err
			}
		}
	}
	return nil
}
