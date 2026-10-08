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
//
// W6.4 (R-DB-11, ADR-0031 §3): đổi vai dạng set_config('role', …) cũng chỉ trong internal/shared/db; tên vai tổng hợp
// r_agg (BYPASSRLS) chỉ được viết trong internal/shared/db và internal/shared/dbadmin; db.WithAggTx — đường duy nhất
// vào r_agg — chỉ được gọi từ bộ chạy job đã khai của cap/analytics (internal/platform/metrics/app).
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
	setRoleSQL    = regexp.MustCompile(`(?is)\bset_config\s*\(\s*'role'`)
	aggRoleName   = regexp.MustCompile(`\br_agg\b`)
)

// aggCallerPkg: gói duy nhất được gọi db.WithAggTx (bộ chạy job tổng hợp đã khai, W6.4).
func aggCallerPkg(pkg string) bool { return strings.HasSuffix(pkg, "/internal/platform/metrics/app") }

// aggRoleHomePkg: gói được viết tên vai r_agg (đổi vai, bootstrap vai).
func aggRoleHomePkg(pkg string) bool {
	return roleHomePkg(pkg) || strings.HasSuffix(pkg, "/internal/shared/dbadmin")
}

func roleHomePkg(pkg string) bool { return strings.HasSuffix(pkg, "/internal/shared/db") }

// sessionSetViolation trả thông điệp vi phạm của chuỗi s trong gói pkg, rỗng nếu hợp lệ.
func sessionSetViolation(pkg, s string) string {
	switch {
	case sessionSetSQL.MatchString(s):
		return "ADR-0028/R-DB-02: không đặt trạng thái mức phiên (SET/RESET/SET ROLE/SET SESSION) — PgBouncer transaction mode rò sang khách khác; dùng SET LOCAL trong giao dịch"
	case setConfigSQL.MatchString(s):
		return "ADR-0028/R-DB-02: set_config(…, false) là mức phiên — dùng set_config(…, true)"
	case (localRoleSQL.MatchString(s) || setRoleSQL.MatchString(s)) && !roleHomePkg(pkg):
		return "R-DB-03: SET LOCAL ROLE chỉ trong internal/shared/db — dùng db.ForModule / db.WithContextTx với db.Module(...)"
	case aggRoleName.MatchString(s) && !aggRoleHomePkg(pkg):
		return "R-DB-11: vai r_agg chỉ được nêu trong internal/shared/db, internal/shared/dbadmin — job tổng hợp đi qua db.WithAggTx"
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
			if call, ok := n.(*ast.CallExpr); ok {
				if isWithAggTx(pass, call) && !aggCallerPkg(pkg) && !roleHomePkg(pkg) {
					pass.Report(analysis.Diagnostic{Pos: call.Pos(),
						Message: "R-DB-11: db.WithAggTx (vai r_agg) chỉ được gọi từ bộ chạy job tổng hợp đã khai internal/platform/metrics/app (W6.4)"})
				}
				return true
			}
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

// isWithAggTx: lời gọi hàm WithAggTx của gói internal/shared/db.
func isWithAggTx(pass *analysis.Pass, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return false
	}
	if id.Name != "WithAggTx" || pass.TypesInfo == nil {
		return false
	}
	obj := pass.TypesInfo.Uses[id]
	return obj != nil && obj.Pkg() != nil && roleHomePkg(obj.Pkg().Path())
}
