package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// srcFile là một tệp Go đã phân tích, kèm ngữ cảnh để phân giải kiểu.
type srcFile struct {
	path    string // đường dẫn tệp
	rel     string // đường dẫn tương đối từ gốc repo, dạng /
	pkgPath string // import path của package
	file    *ast.File
	imports map[string]string // tên dùng trong tệp → import path
}

// typeDecl là một khai báo kiểu có tên.
type typeDecl struct {
	name string
	expr ast.Expr
	file *srcFile
	pos  token.Pos
	doc  string // chú thích thô, gồm cả dòng chỉ thị //ag:...
}

// index là chỉ mục kiểu của mọi package dưới internal/.
type index struct {
	fset     *token.FileSet
	module   string
	files    []*srcFile
	pkgNames map[string]string               // import path → tên package
	types    map[string]map[string]*typeDecl // import path → tên kiểu → khai báo
	methods  map[string]map[string][]*ast.FuncDecl
}

var moduleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// modulePath đọc go.mod ở gốc; không có (thư mục kiểm thử) → example.com/ag.
func modulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err == nil {
		if m := moduleRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return "example.com/ag"
}

func buildIndex(root string) (*index, error) {
	ix := &index{
		fset: token.NewFileSet(), module: modulePath(root), pkgNames: map[string]string{},
		types: map[string]map[string]*typeDecl{}, methods: map[string]map[string][]*ast.FuncDecl{},
	}
	dir := filepath.Join(root, "internal")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return ix, nil
	}
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
		f, err := parser.ParseFile(ix.fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		sf := &srcFile{path: path, rel: rel, pkgPath: ix.module + "/" + filepath.ToSlash(filepath.Dir(rel)), file: f, imports: map[string]string{}}
		ix.pkgNames[sf.pkgPath] = f.Name.Name
		ix.files = append(ix.files, sf)
		if ix.types[sf.pkgPath] == nil {
			ix.types[sf.pkgPath] = map[string]*typeDecl{}
			ix.methods[sf.pkgPath] = map[string][]*ast.FuncDecl{}
		}
		for _, decl := range f.Decls {
			switch dd := decl.(type) {
			case *ast.GenDecl:
				if dd.Tok != token.TYPE {
					continue
				}
				for _, spec := range dd.Specs {
					ts := spec.(*ast.TypeSpec)
					ix.types[sf.pkgPath][ts.Name.Name] = &typeDecl{
						name: ts.Name.Name, expr: ts.Type, file: sf, pos: ts.Pos(), doc: rawDoc(dd.Doc, ts.Doc),
					}
				}
			case *ast.FuncDecl:
				if dd.Recv == nil || len(dd.Recv.List) == 0 {
					continue
				}
				if n := recvName(dd.Recv.List[0].Type); n != "" {
					ix.methods[sf.pkgPath][n] = append(ix.methods[sf.pkgPath][n], dd)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Tên dùng trong tệp cho mỗi import: bí danh, hoặc tên package thật (nếu đã
	// chỉ mục), hoặc phần cuối đường dẫn.
	for _, sf := range ix.files {
		for _, im := range sf.file.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			name := ""
			switch {
			case im.Name != nil:
				name = im.Name.Name
			case ix.pkgNames[p] != "":
				name = ix.pkgNames[p]
			default:
				name = p[strings.LastIndex(p, "/")+1:]
				if strings.HasPrefix(name, "v") && len(name) <= 3 && strings.Contains(p, "/") { // .../pgx/v5
					q := strings.TrimSuffix(p, "/"+name)
					name = q[strings.LastIndex(q, "/")+1:]
				}
			}
			if name != "_" && name != "." {
				sf.imports[name] = p
			}
		}
	}
	return ix, nil
}

// rawDoc gộp chú thích của GenDecl và TypeSpec, giữ cả dòng chỉ thị //ag:...
// (ast.CommentGroup.Text() bỏ các dòng chỉ thị).
func rawDoc(groups ...*ast.CommentGroup) string {
	var b strings.Builder
	for _, cg := range groups {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			b.WriteString(c.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	}
	return ""
}

func (ix *index) pos(p token.Pos) string { return ix.fset.Position(p).String() }

// lookup phân giải một biểu thức kiểu có tên (Ident hoặc pkg.Ident) về import
// path + tên. ok = false nếu không phải kiểu có tên.
func (ix *index) lookup(e ast.Expr, sf *srcFile) (pkg, name string, ok bool) {
	switch t := e.(type) {
	case *ast.Ident:
		return sf.pkgPath, t.Name, true
	case *ast.SelectorExpr:
		x, isIdent := t.X.(*ast.Ident)
		if !isIdent {
			return "", "", false
		}
		p, found := sf.imports[x.Name]
		if !found {
			return "", "", false
		}
		return p, t.Sel.Name, true
	}
	return "", "", false
}

// decl trả khai báo kiểu đã chỉ mục.
func (ix *index) decl(pkg, name string) *typeDecl {
	if m := ix.types[pkg]; m != nil {
		return m[name]
	}
	return nil
}

// hasMethod cho biết kiểu có phương thức tên m.
func (ix *index) hasMethod(pkg, typ, m string) bool {
	for _, fd := range ix.methods[pkg][typ] {
		if fd.Name.Name == m {
			return true
		}
	}
	return false
}

// jsonField là một trường JSON của struct (sau khi làm phẳng struct nhúng).
type jsonField struct {
	name string
	typ  ast.Expr
	file *srcFile
	pos  token.Pos
}

// jsonFields liệt kê trường JSON của một struct theo quy tắc encoding/json:
// tag json đặt tên, "-" bỏ qua, trường không xuất bỏ qua, struct nhúng không
// có tag được làm phẳng.
func (ix *index) jsonFields(st *ast.StructType, sf *srcFile, depth int) []jsonField {
	var out []jsonField
	for _, f := range st.Fields.List {
		tagName := ""
		if f.Tag != nil {
			tag, _ := strconv.Unquote(f.Tag.Value)
			if j := reflect.StructTag(tag).Get("json"); j != "" {
				if j == "-" {
					continue
				}
				tagName = strings.Split(j, ",")[0]
			}
		}
		if len(f.Names) == 0 { // nhúng
			if tagName == "" && depth < 8 {
				base := f.Type
				if s, ok := base.(*ast.StarExpr); ok {
					base = s.X
				}
				if pkg, name, ok := ix.lookup(base, sf); ok {
					if d := ix.decl(pkg, name); d != nil {
						if est, ok := d.expr.(*ast.StructType); ok {
							out = append(out, ix.jsonFields(est, d.file, depth+1)...)
							continue
						}
					}
				}
				continue
			}
			if tagName == "" {
				continue
			}
			out = append(out, jsonField{name: tagName, typ: f.Type, file: sf, pos: f.Pos()})
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			name := n.Name
			if tagName != "" {
				name = tagName
			}
			out = append(out, jsonField{name: name, typ: f.Type, file: sf, pos: n.Pos()})
		}
	}
	return out
}
