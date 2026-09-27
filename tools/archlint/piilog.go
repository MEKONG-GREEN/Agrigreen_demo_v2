package main

import (
	"go/ast"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
)

// PIILog cấm đưa trường có tag pii:"true" (hoặc struct chứa trường đó) vào log,
// trace, thông báo lỗi (R-14).
var PIILog = &analysis.Analyzer{
	Name: "agpiilog",
	Doc:  `cấm truyền trường có tag pii:"true", hoặc struct chứa trường đó, vào log/slog, log, fmt, errors (R-14)`,
	Run:  runPIILog,
}

var sinkPkgs = map[string]bool{"log/slog": true, "log": true, "fmt": true, "errors": true}

func runPIILog(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isSink(pass, call) {
				return true
			}
			for _, arg := range call.Args {
				if field := piiField(pass, arg); field != "" {
					pass.Reportf(arg.Pos(), "R-14: không ghi dữ liệu cá nhân (%s) vào log hoặc thông báo lỗi — chỉ ghi mã tham chiếu", field)
				}
			}
			return true
		})
	}
	return nil, nil
}

func isSink(pass *analysis.Pass, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return false
	}
	obj, ok := pass.TypesInfo.Uses[id].(*types.Func)
	return ok && obj.Pkg() != nil && sinkPkgs[obj.Pkg().Path()]
}

// piiField trả tên trường PII nếu biểu thức là trường PII hoặc giá trị struct chứa trường PII.
func piiField(pass *analysis.Pass, e ast.Expr) string {
	if sel, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
		if s, ok := pass.TypesInfo.Selections[sel]; ok && s.Kind() == types.FieldVal {
			if tagged(s.Recv(), sel.Sel.Name) {
				return sel.Sel.Name
			}
		}
	}
	if tv, ok := pass.TypesInfo.Types[e]; ok {
		return structPII(tv.Type, 0)
	}
	return ""
}

func tagged(recv types.Type, name string) bool {
	st := asStruct(recv)
	if st == nil {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == name && reflect.StructTag(st.Tag(i)).Get("pii") == "true" {
			return true
		}
	}
	return false
}

func asStruct(t types.Type) *types.Struct {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, _ := t.Underlying().(*types.Struct)
	return st
}

func structPII(t types.Type, depth int) string {
	if depth > 3 {
		return ""
	}
	st := asStruct(t)
	if st == nil {
		return ""
	}
	for i := 0; i < st.NumFields(); i++ {
		if reflect.StructTag(st.Tag(i)).Get("pii") == "true" {
			return st.Field(i).Name()
		}
		if name := structPII(st.Field(i).Type(), depth+1); name != "" {
			return st.Field(i).Name() + "." + name
		}
	}
	return ""
}
