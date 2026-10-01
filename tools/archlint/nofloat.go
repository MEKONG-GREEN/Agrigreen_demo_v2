package main

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Package tiền và khối lượng (R-10).
var floatFreeSegments = []string{"costing", "measurement", "orders", "acctbridge", "metering", "money", "mass"}

// NoFloat cấm float32/float64 trong package tiền và khối lượng (R-10).
var NoFloat = &analysis.Analyzer{
	Name: "agnofloat",
	Doc:  "cấm float32/float64 trong costing, measurement, orders, acctbridge, metering, mkg/hub, mkg/logistics, reports/{formula,form,formimport}, shared/money, shared/mass (R-10)",
	Run:  runNoFloat,
}

// floatFreeZones là module có khối lượng ở mọi lớp (lô, tồn, hao hụt): cấm float
// trong toàn module, không chỉ theo tên package. G4-0: mkg/logistics (khối lượng chở, cước,
// km, tọa độ đều decimal); costing, orders, acctbridge đã phủ theo tên đoạn (floatFreeSegments)
// nên mọi package con app/, domain/, store/, handler/, adapter/ của chúng cũng bị cấm float.
// KN v7.7 P3: engine mẫu báo cáo (diện tích, sản lượng, năng suất, tỷ lệ — decimal), bộ tính
// công thức con của Excel và bộ nạp Excel.
var floatFreeZones = []string{"/internal/mkg/hub", "/internal/mkg/logistics",
	"/internal/platform/reports/formula", "/internal/platform/reports/form", "/internal/platform/reports/formimport"}

func floatFree(pkg string) bool {
	for _, z := range floatFreeZones {
		if strings.Contains(pkg, z+"/") || strings.HasSuffix(strings.TrimSuffix(pkg, "_test"), z) {
			return true
		}
	}
	for _, seg := range strings.Split(strings.TrimSuffix(pkg, "_test"), "/") {
		for _, s := range floatFreeSegments {
			if seg == s {
				return true
			}
		}
	}
	return false
}

func isFloat(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

func runNoFloat(pass *analysis.Pass) (any, error) {
	if !floatFree(pass.Pkg.Path()) {
		return nil, nil
	}
	seen := map[int]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			tv, ok := pass.TypesInfo.Types[e]
			if !ok || !isFloat(tv.Type) {
				return true
			}
			line := pass.Fset.Position(e.Pos()).Line
			if !seen[line] {
				seen[line] = true
				pass.Reportf(e.Pos(), "R-10: không dùng float trong package tiền/khối lượng — dùng shared/money hoặc shared/mass (decimal)")
			}
			return false
		})
	}
	return nil, nil
}
