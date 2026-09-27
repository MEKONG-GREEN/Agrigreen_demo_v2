package main

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// NoTimeNow cấm time.Now/Since/Until trong package domain/ (R-12).
var NoTimeNow = &analysis.Analyzer{
	Name: "agnotimenow",
	Doc:  "cấm time.Now(), time.Since(), time.Until() trong domain/ — nhận clock.Clock hoặc thời điểm qua tham số (R-12)",
	Run:  runNoTimeNow,
}

func isDomainPkg(pkg string) bool {
	for _, seg := range strings.Split(strings.TrimSuffix(pkg, "_test"), "/") {
		if seg == "domain" {
			return true
		}
	}
	return false
}

func runNoTimeNow(pass *analysis.Pass) (any, error) {
	if !isDomainPkg(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
				return true
			}
			switch fn.Name() {
			case "Now", "Since", "Until":
				pass.Reportf(sel.Pos(), "R-12: không gọi time.%s trong domain/ — nhận thời gian qua clock.Clock", fn.Name())
			}
			return true
		})
	}
	return nil, nil
}
