// Lệnh contractcheck là cổng CI cho hợp đồng:
//
//   - Sự kiện (R-64): contracts/events/<event_type>.v<N>.json khớp tên và phiên
//     bản; struct Go chú thích //ag:event <type> v<N> không có trường ngoài
//     schema và có đủ trường required; so với nhánh gốc (-base) chỉ được thêm
//     trường tùy chọn — xóa trường, đổi kiểu, thêm required → phải tăng phiên bản.
//   - Van công bố (mục 5): struct chú thích //ag:publication <tên> chỉ được có
//     trường nằm trong contracts/publication/<tên>.json; không có hợp đồng → hỏng.
//
// Chạy: go run ./tools/contractcheck [-base origin/main]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	root := flag.String("root", ".", "gốc repo")
	base := flag.String("base", "", "ref git để so tương thích ngược, vd origin/main (rỗng = bỏ qua)")
	flag.Parse()
	problems, err := Check(*root, *base)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

// Schema là phần JSON Schema mà cổng quan tâm.
type Schema struct {
	EventType     string                     `json:"x-event-type"`
	SchemaVersion int                        `json:"x-schema-version"`
	Required      []string                   `json:"required"`
	Properties    map[string]json.RawMessage `json:"properties"`
}

// Publication là hợp đồng van công bố.
type Publication struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

var (
	eventFileRe = regexp.MustCompile(`^((?:pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+)\.v(\d+)\.json$`)
	eventTagRe  = regexp.MustCompile(`ag:event\s+(\S+)\s+v(\d+)`)
	pubTagRe    = regexp.MustCompile(`ag:publication\s+(\S+)`)
)

// Check chạy toàn bộ kiểm tra.
func Check(root, base string) ([]string, error) {
	var problems []string
	schemas := map[string]Schema{}
	evDir := filepath.Join(root, "contracts", "events")
	files, _ := filepath.Glob(filepath.Join(evDir, "*.json"))
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, "envelope.") {
			continue
		}
		m := eventFileRe.FindStringSubmatch(name)
		if m == nil {
			problems = append(problems, f+": tên tệp phải là <vùng>.<module>.<thực thể>.<động từ>.v<N>.json (R-64)")
			continue
		}
		var s Schema
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			problems = append(problems, f+": JSON không hợp lệ: "+err.Error())
			continue
		}
		v, _ := strconv.Atoi(m[2])
		if s.EventType != m[1] || s.SchemaVersion != v {
			problems = append(problems, fmt.Sprintf("%s: x-event-type/x-schema-version (%s v%d) không khớp tên tệp", f, s.EventType, s.SchemaVersion))
		}
		for _, r := range s.Required {
			if _, ok := s.Properties[r]; !ok {
				problems = append(problems, f+": required "+r+" không có trong properties")
			}
		}
		schemas[fmt.Sprintf("%s v%d", m[1], v)] = s
		if base != "" {
			problems = append(problems, compat(root, base, f, s)...)
		}
	}

	pubs := map[string]Publication{}
	pubFiles, _ := filepath.Glob(filepath.Join(root, "contracts", "publication", "*.json"))
	for _, f := range pubFiles {
		var p Publication
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &p); err != nil || p.Name == "" {
			problems = append(problems, f+": hợp đồng công bố không hợp lệ")
			continue
		}
		pubs[p.Name] = p
	}

	structs, err := annotatedStructs(filepath.Join(root, "internal"))
	if err != nil {
		return nil, err
	}
	for _, st := range structs {
		switch st.kind {
		case "event":
			s, ok := schemas[st.key]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: struct %s chú thích ag:event %s nhưng không có schema trong contracts/events/", st.pos, st.name, st.key))
				continue
			}
			for _, f := range st.fields {
				if _, ok := s.Properties[f]; !ok {
					problems = append(problems, fmt.Sprintf("%s: %s.%s không có trong schema %s (trường ngoài hợp đồng)", st.pos, st.name, f, st.key))
				}
			}
			for _, r := range s.Required {
				if !contains(st.fields, r) {
					problems = append(problems, fmt.Sprintf("%s: %s thiếu trường required %s của schema %s", st.pos, st.name, r, st.key))
				}
			}
		case "publication":
			p, ok := pubs[st.key]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: %s chú thích ag:publication %s nhưng không có hợp đồng — mặc định từ chối", st.pos, st.name, st.key))
				continue
			}
			for _, f := range st.fields {
				if !contains(p.Fields, f) {
					problems = append(problems, fmt.Sprintf("%s: %s.%s không có trong hợp đồng công bố %s", st.pos, st.name, f, st.key))
				}
			}
		}
	}
	sort.Strings(problems)
	return problems, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// compat so schema hiện tại với bản ở ref git base.
func compat(root, base, path string, cur Schema) []string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "show", base+":"+filepath.ToSlash(rel)).Output()
	if err != nil {
		return nil // tệp mới hoặc không có git: không có gì để so
	}
	var old Schema
	if json.Unmarshal(out, &old) != nil {
		return nil
	}
	var problems []string
	for name, oldDef := range old.Properties {
		newDef, ok := cur.Properties[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: xóa trường %s — phải tăng schema_version (R-64)", path, name))
			continue
		}
		if typeOf(oldDef) != typeOf(newDef) {
			problems = append(problems, fmt.Sprintf("%s: đổi kiểu trường %s (%s → %s) — phải tăng schema_version (R-64)", path, name, typeOf(oldDef), typeOf(newDef)))
		}
	}
	for _, r := range cur.Required {
		if !contains(old.Required, r) {
			problems = append(problems, fmt.Sprintf("%s: thêm trường bắt buộc %s — consumer cũ sẽ hỏng, phải tăng schema_version (R-64)", path, r))
		}
	}
	return problems
}

func typeOf(raw json.RawMessage) string {
	var d struct {
		Type any `json:"type"`
	}
	_ = json.Unmarshal(raw, &d)
	return fmt.Sprint(d.Type)
}

type annotated struct {
	kind, key, name, pos string
	fields               []string
}

func annotatedStructs(dir string) ([]annotated, error) {
	var out []annotated
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				doc := gd.Doc.Text()
				if ts.Doc != nil {
					doc += ts.Doc.Text()
				}
				// ast.CommentGroup.Text() bỏ dòng chỉ thị //ag:...; đọc thô.
				for _, cg := range []*ast.CommentGroup{gd.Doc, ts.Doc} {
					if cg == nil {
						continue
					}
					for _, c := range cg.List {
						doc += "\n" + c.Text
					}
				}
				a := annotated{name: ts.Name.Name, pos: fset.Position(ts.Pos()).String(), fields: jsonFields(st)}
				if m := eventTagRe.FindStringSubmatch(doc); m != nil {
					a.kind, a.key = "event", m[1]+" v"+m[2]
					out = append(out, a)
				} else if m := pubTagRe.FindStringSubmatch(doc); m != nil {
					a.kind, a.key = "publication", m[1]
					out = append(out, a)
				}
			}
		}
		return nil
	})
	return out, err
}

func jsonFields(st *ast.StructType) []string {
	var out []string
	for _, f := range st.Fields.List {
		name := ""
		if len(f.Names) > 0 {
			name = f.Names[0].Name
		}
		if f.Tag != nil {
			tag, _ := strconv.Unquote(f.Tag.Value)
			if j := reflect.StructTag(tag).Get("json"); j != "" {
				if j == "-" {
					continue
				}
				name = strings.Split(j, ",")[0]
			}
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
