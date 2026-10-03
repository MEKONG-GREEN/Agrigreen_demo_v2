package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
)

// Sản phẩm dữ liệu Public API (G5, ADR-0022 mục 9): cửa nền tảng → bên thứ ba, mặc định từ chối.
//
//	(a) contracts/dataproduct/<tên>.json: siêu dữ liệu đúng (dataproduct.MetaProblems — người đề xuất ≠
//	    người phê duyệt, personal_data = false, grant_kind = dp_<tên>, scope, nguồn chỉ pf/), name = tên tệp;
//	(b) struct //ag:dataproduct <tên> chỉ có trường của hợp đồng, đúng kiểu, đủ required (đệ quy);
//	    không có hợp đồng hợp lệ → hỏng (mặc định từ chối);
//	(c) //ag:dataproduct chỉ dùng trong internal/platform/** (Public API chỉ ở vùng nền tảng, R-03);
//	(d) mỗi sản phẩm có tag cùng tên trong contracts/openapi/public.yaml.
var dpTagRe = regexp.MustCompile(`(?m)^//ag:dataproduct\s+(\S+)`)

const dataProductDir = "dataproduct"

func loadDataProducts(root string) (map[string]*dataproduct.Product, []string, error) {
	var problems []string
	out := map[string]*dataproduct.Product{}
	files, _ := filepath.Glob(filepath.Join(root, "contracts", dataProductDir, "*.json"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		p, err := dataproduct.Parse(raw)
		if err != nil {
			problems = append(problems, f+": hợp đồng sản phẩm dữ liệu không hợp lệ: "+err.Error())
			continue
		}
		bad := false
		for _, mp := range p.MetaProblems() {
			problems = append(problems, f+": "+mp)
			bad = true
		}
		if want := strings.TrimSuffix(filepath.Base(f), ".json"); p.Name != want {
			problems = append(problems, fmt.Sprintf("%s: name %q phải trùng tên tệp %q", f, p.Name, want))
			bad = true
		}
		if !bad {
			out[p.Name] = p
		}
	}
	return out, problems, nil
}

// publicTags đọc tên tag của contracts/openapi/public.yaml; ok = false khi không có tệp.
func publicTags(root string) (map[string]bool, bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, "contracts", "openapi", "public.yaml"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var doc struct {
		Tags []struct {
			Name string `yaml:"name"`
		} `yaml:"tags"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, true, fmt.Errorf("contracts/openapi/public.yaml: YAML không hợp lệ: %w", err)
	}
	tags := map[string]bool{}
	for _, t := range doc.Tags {
		tags[t.Name] = true
	}
	return tags, true, nil
}

func (ix *index) dataProductRules(root string, ann map[string]*annotated, prods map[string]*dataproduct.Product) ([]string, error) {
	var p []string
	names := make([]string, 0, len(prods))
	for n := range prods {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 0 {
		tags, ok, err := publicTags(root)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if !ok {
				p = append(p, fmt.Sprintf("contracts/dataproduct/%s.json: thiếu contracts/openapi/public.yaml (mỗi sản phẩm phải có tag cùng tên)", n))
				continue
			}
			if !tags[n] {
				p = append(p, fmt.Sprintf("contracts/dataproduct/%s.json: contracts/openapi/public.yaml không có tag %q", n, n))
			}
		}
	}
	for _, a := range ann {
		if a.dataproduct == "" {
			continue
		}
		if !strings.HasPrefix(a.decl.file.rel, "internal/platform/") {
			p = append(p, fmt.Sprintf("%s: //ag:dataproduct chỉ dùng trong internal/platform (Public API ở vùng nền tảng, R-03)", a.pos))
			continue
		}
		prod, ok := prods[a.dataproduct]
		if !ok {
			p = append(p, fmt.Sprintf("%s: %s chú thích ag:dataproduct %s nhưng không có hợp đồng sản phẩm hợp lệ — mặc định từ chối", a.pos, a.decl.name, a.dataproduct))
			continue
		}
		if st, isStruct := a.decl.expr.(*ast.StructType); isStruct { // không phải struct: vòng chính đã báo
			p = append(p, ix.checkStruct(a.decl.name, "", st, a.decl.file, &prod.Node, "hợp đồng sản phẩm dữ liệu "+a.dataproduct, 0)...)
		}
	}
	return p, nil
}
