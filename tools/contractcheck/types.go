package main

import (
	"fmt"
	"go/ast"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

// jtype là kiểu JSON mà một kiểu Go tuần tự hóa ra (encoding/json).
type jtype struct {
	kind     string // string | integer | number | boolean | object | array | any
	nullable bool
	st       *ast.StructType // object có cấu trúc đã biết
	stFile   *srcFile
	elem     *jtype // array
}

// knownExternal là kiểu ngoài chỉ mục (hoặc có MarshalJSON) với dạng JSON đã biết.
func (ix *index) knownExternal(pkg, name string) (string, bool) {
	switch pkg + "." + name {
	case "github.com/google/uuid.UUID", "time.Time", "github.com/shopspring/decimal.Decimal",
		ix.module + "/internal/shared/mass.Kg", ix.module + "/internal/shared/money.Amount":
		return "string", true
	case "time.Duration":
		return "integer", true
	case "encoding/json.Number":
		return "number", true
	case "encoding/json.RawMessage":
		return "any", true
	}
	return "", false
}

var basicKinds = map[string]string{
	"string": "string", "bool": "boolean",
	"int": "integer", "int8": "integer", "int16": "integer", "int32": "integer", "int64": "integer",
	"uint": "integer", "uint8": "integer", "uint16": "integer", "uint32": "integer", "uint64": "integer", "uintptr": "integer",
	"byte": "integer", "rune": "integer",
	"float32": "number", "float64": "number",
	"any": "any", "error": "any",
}

// resolve tính kiểu JSON của một biểu thức kiểu Go.
func (ix *index) resolve(e ast.Expr, sf *srcFile, depth int) jtype {
	if depth > 16 {
		return jtype{kind: "any"}
	}
	switch t := e.(type) {
	case *ast.StarExpr:
		j := ix.resolve(t.X, sf, depth+1)
		j.nullable = true
		return j
	case *ast.ArrayType:
		if id, ok := t.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") && t.Len == nil {
			return jtype{kind: "string", nullable: true} // []byte → base64
		}
		el := ix.resolve(t.Elt, sf, depth+1)
		return jtype{kind: "array", nullable: t.Len == nil, elem: &el}
	case *ast.MapType:
		return jtype{kind: "object", nullable: true}
	case *ast.InterfaceType:
		return jtype{kind: "any"}
	case *ast.StructType:
		return jtype{kind: "object", st: t, stFile: sf}
	case *ast.Ident:
		if k, ok := basicKinds[t.Name]; ok && ix.decl(sf.pkgPath, t.Name) == nil {
			return jtype{kind: k}
		}
	}
	pkg, name, ok := ix.lookup(e, sf)
	if !ok {
		return jtype{kind: "any"}
	}
	if k, ok := ix.knownExternal(pkg, name); ok {
		return jtype{kind: k}
	}
	d := ix.decl(pkg, name)
	if d == nil || ix.hasMethod(pkg, name, "MarshalJSON") {
		return jtype{kind: "any"} // kiểu ngoài chỉ mục hoặc tự tuần tự hóa: không suy được
	}
	return ix.resolve(d.expr, d.file, depth+1)
}

func typeCompatible(want publication.TypeSet, got jtype) bool {
	if len(want) == 0 || got.kind == "any" {
		return true
	}
	if want.Has(got.kind) {
		return true
	}
	return got.kind == "integer" && want.Has("number")
}

// checkStruct so một struct Go với nút schema object: trường ngoài schema, thiếu
// required, sai kiểu — đệ quy vào object và mảng lồng nhau.
func (ix *index) checkStruct(owner, path string, st *ast.StructType, sf *srcFile, n *publication.Node, what string, depth int) []string {
	var p []string
	fields := ix.jsonFields(st, sf, 0)
	have := map[string]jsonField{}
	for _, f := range fields {
		have[f.name] = f
	}
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := have[name]
		fp := join(path, name)
		child, ok := n.Properties[name]
		if n.Properties != nil && !ok {
			p = append(p, fmt.Sprintf("%s: %s.%s không có trong %s (trường ngoài hợp đồng)", ix.pos(f.pos), owner, fp, what))
			continue
		}
		if child == nil {
			continue
		}
		p = append(p, ix.checkType(owner, fp, ix.resolve(f.typ, f.file, 0), child, ix.pos(f.pos), what, depth+1)...)
	}
	for _, r := range n.Required {
		if _, ok := have[r]; !ok {
			p = append(p, fmt.Sprintf("%s: %s thiếu trường required %s của %s", ix.pos(st.Pos()), owner, join(path, r), what))
		}
	}
	return p
}

func (ix *index) checkType(owner, path string, got jtype, n *publication.Node, at, what string, depth int) []string {
	if depth > 16 {
		return nil
	}
	if !typeCompatible(n.Type, got) {
		return []string{fmt.Sprintf("%s: %s.%s kiểu Go ra JSON %s, %s khai %v", at, owner, path, got.kind, what, []string(n.Type))}
	}
	switch got.kind {
	case "object":
		if got.st != nil && (n.Properties != nil || len(n.Required) > 0) {
			return ix.checkStruct(owner, path, got.st, got.stFile, n, what, depth)
		}
	case "array":
		if got.elem != nil && n.Items != nil {
			return ix.checkType(owner, path+"[]", *got.elem, n.Items, at, what, depth+1)
		}
	}
	return nil
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}
