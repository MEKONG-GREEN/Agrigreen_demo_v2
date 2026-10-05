package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// NoSessionSet cấm trạng thái mức phiên trong chuỗi SQL của mã không phải kiểm thử (ADR-0028 §3, R-DB-02,
// R-DB-03): qua PgBouncer transaction mode, `SET ROLE`, `SET <biến>`, `RESET …`, `SET SESSION …` hay
// `set_config(…, false)` rò sang khách khác dùng chung kết nối máy chủ. Chỉ SET LOCAL / set_config(…, true)
// được phép; riêng `SET LOCAL ROLE` chỉ được viết trong internal/shared/db (vai lấy từ registry, không từ
// tham số chuỗi). Migration: tools/migratecheck có luật tương ứng.
var NoSessionSet = &analysis.Analyzer{
	Name: "agnosessionset",
	Doc: "cấm SET ROLE / SET <biến> / RESET / SET SESSION / set_config(…, false) mức phiên trong chuỗi SQL (ADR-0028, R-DB-02) — " +
		"dùng SET LOCAL hoặc set_config(…, true); SET LOCAL ROLE chỉ trong internal/shared/db (R-DB-03)",
	Run: runNoSessionSet,
}

var (
	// Đầu câu lệnh (đầu chuỗi hoặc sau ;): SET không kèm LOCAL, hoặc RESET. "UPDATE … SET role = …" không khớp.
	sessionSetSQL = regexp.MustCompile(`(?is)(^|;)\s*(RESET\s+[a-z_."]+|SET\s+(SESSION\b|ROLE\b|TIME\s+ZONE\b|[a-z_."]+\s*(=|\bTO\b)))`)
	localRoleSQL  = regexp.MustCompile(`(?is)(^|;)\s*SET\s+LOCAL\s+ROLE\b`)
	setConfigSQL  = regexp.MustCompile(`(?is)\bset_config\s*\([^)]*,\s*false\s*\)`)
)

func roleHomePkg(pkg string) bool { return strings.HasSuffix(pkg, "/internal/shared/db") }

// sessionSetViolation trả thông điệp vi phạm của chuỗi s trong gói pkg, rỗng nếu hợp lệ.
func sessionSetViolation(pkg, s string) string {
	switch {
	case sessionSetSQL.MatchString(s):
		return "ADR-0028/R-DB-02: không đặt trạng thái mức phiên (SET/RESET/SET ROLE/SET SESSION) — PgBouncer transaction mode rò sang khách khác; dùng SET LOCAL trong giao dịch"
	case setConfigSQL.MatchString(s):
		return "ADR-0028/R-DB-02: set_config(…, false) là mức phiên — dùng set_config(…, true)"
	case localRoleSQL.MatchString(s) && !roleHomePkg(pkg):
		return "R-DB-03: SET LOCAL ROLE chỉ trong internal/shared/db — dùng db.ForModule / db.WithContextTx với db.Module(...)"
	}
	return ""
}

// appPkg: mã chạy với vai ứng dụng — internal/* và cmd/* (tools/* không nối DB ứng dụng).
func appPkg(pkg string) bool {
	return strings.Contains(pkg, "/internal/") || strings.Contains(pkg, "/cmd/")
}

func runNoSessionSet(pass *analysis.Pass) (any, error) {
	pkg := pass.Pkg.Path()
	if !appPkg(pkg) {
		return nil, nil
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				s = lit.Value
			}
			if msg := sessionSetViolation(pkg, s); msg != "" {
				pass.Report(analysis.Diagnostic{Pos: lit.Pos(), Message: msg})
			}
			return true
		})
	}
	return nil, nil
}
