package main

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// TenantGate là chốt tạm của review H4 (trước k_ref.share_scope, mục 17.3): vai module vẫn đặt được
// app.tenant_id tùy ý, nên quyền ĐỔI tenant trong mã Go bị gom về hai cửa có kiểm:
//
//   - tenant.WithID chỉ ở middleware xác thực và các nơi có tên trong withIDAllowed (tenant của phong bì
//     sự kiện, job theo tenant, tenant của Principal…). Đổi sang tenant bên cấp sau kiểm cấp quyền chia
//     sẻ dùng tenant.WithGrant / tenant.Within — cả hai đòi tenant.Grant.
//   - tenant.MintGrant (đúc tenant.Grant) chỉ ở mã kiểm cấp quyền: mintAllowed.
//   - Gói giả có quyền đúc (testOnlyImports) chỉ được import từ tệp _test.go.
//
// Tệp _test.go được miễn (kiểm thử dựng ngữ cảnh tenant trực tiếp).
var TenantGate = &analysis.Analyzer{
	Name: "agtenantgate",
	Doc: "H4: tenant.WithID chỉ ở middleware xác thực / danh sách duyệt; tenant.MintGrant chỉ ở mã kiểm cấp quyền chia sẻ (pf/party); " +
		"gói giả đúc bằng chứng chỉ import từ _test.go",
	Run: runTenantGate,
}

// tenantPkg là gói cửa DB nghiệp vụ (tương đối với modulePath).
const tenantPkg = "internal/shared/tenant"

// withIDAllowed: tệp (đường dẫn tương đối gốc module) được gọi tenant.WithID, kèm lý do. Danh sách chỉ
// được co lại; mục mới phải là tenant đã xác thực / của chính phong bì — KHÔNG BAO GIỜ tenant bên cấp.
var withIDAllowed = map[string]string{
	"internal/shared/httpx/httpx.go":                       "middleware xác thực OIDC (R-16)",
	"internal/platform/apiclient/public/server.go":         "middleware khóa API (tenant khách hàng từ bản ghi khóa); kFor đọc tham số k của bên cấp sau GrantLookup — chờ grantlookup trả Grant (review M9)",
	"internal/shared/inbox/inbox.go":                       "tenant của phong bì sự kiện, cùng giao dịch inbox",
	"internal/platform/accounts/app/kn_extension.go":       "job worker: tenant truyền tường minh, không Principal",
	"internal/platform/cropcycle/app/dataproduct.go":       "tenant dữ liệu dp_* đã kiểm GrantLookup ở apiclient/public — chờ grantlookup trả Grant (review M9)",
	"internal/platform/cropcycle/app/aggregate.go":         "W5.3: tenant đóng góp tập tổng hợp (AG_INTEGRATION_CONTRIBUTORS, tạm) — chờ cấp phép aggregate của pf/party trả Grant (ADR-0035 §1)",
	"internal/platform/apiclient/app/feedbuild.go":         "W5.3 job dựng feed: tham số k của CHÍNH tenant đơn vị vận hành (không phải bên cấp)",
	"internal/platform/cropcycle/app/wiring.go":            "tenant của phong bì sự kiện",
	"internal/platform/extension/app/gpslog.go":            "tenant của phong bì sự kiện",
	"internal/platform/extension/app/schedule_job.go":      "job theo từng tenant",
	"internal/platform/extension/app/support.go":           "tenant của Principal đã xác thực",
	"internal/platform/extension/app/support_sync.go":      "tenant của Principal đã xác thực",
	"internal/platform/extension/reportcycle/app/job.go":   "job theo từng tenant",
	"internal/platform/geo/app/dataproduct.go":             "tenant dữ liệu dp_* đã kiểm GrantLookup ở apiclient/public — chờ grantlookup trả Grant (review M9)",
	"internal/platform/mechanization/app/areasync.go":      "tenant của phong bì sự kiện",
	"internal/platform/mechanization/app/areasynclog.go":   "job theo từng tenant",
	"internal/platform/mechanization/app/machine_count.go": "tenant của Principal đã xác thực",
	"internal/platform/metering/app/service.go":            "tham số đã duyệt của tenant mà sổ đang ghi (vận hành / chủ dữ liệu đã kiểm cấp quyền)",
	"internal/platform/metrics/app/gisread.go":             "tenant của Principal đã xác thực",
	"internal/platform/mrv/app/g3.go":                      "tenant của phong bì sự kiện (bên cấp đi qua WithGrant)",
	"internal/platform/party/app/kn_collect.go":            "tenant của Principal đã xác thực",
	"internal/platform/party/app/wiring.go":                "tenant của Principal đã xác thực",
	"internal/platform/reports/app/build.go":               "job theo từng tenant",
	"internal/platform/reports/app/schedules.go":           "job theo từng tenant",
	"internal/platform/simulation/app/adapters.go":         "tenant người gọi truyền tường minh qua cổng mô phỏng",
	"cmd/api-platform/assembly/wiring.go":                  "bộ nối cổng chéo module: tenant người gọi truyền tường minh",
}

// mintAllowed: gói (hoặc tệp .go) được đúc tenant.Grant. Chỉ mã kiểm cấp quyền chia sẻ (ADR-0020).
var mintAllowed = map[string]string{
	"internal/platform/party/app":              "kiểm cấp quyền chia sẻ (Authorize, ValveGrantors) và thao tác cấp quyền của bên cấp",
	"internal/platform/party/store":            "VerifiedActive: kiểm phía bên cấp của dòng chiếu",
	"internal/platform/party/port/porttest":    "bản giả cho kiểm thử — chỉ import từ _test.go (testOnlyImports)",
	"internal/platform/metering/app/ledger.go": "egress public_api vào tenant chủ dữ liệu đã kiểm GrantLookup — chờ grantlookup trả Grant (review M9)",
}

// testOnlyImports: gói có quyền đúc bằng chứng nhưng chỉ dành cho kiểm thử.
var testOnlyImports = []string{"internal/platform/party/port/porttest"}

func listed(m map[string]string, pkgRel, fileRel string) bool {
	_, okPkg := m[pkgRel]
	_, okFile := m[fileRel]
	return okPkg || okFile
}

func runTenantGate(pass *analysis.Pass) (any, error) {
	pkg := strings.TrimSuffix(pass.Pkg.Path(), "_test")
	if !appPkg(pkg) {
		return nil, nil
	}
	pkgRel, ok := strings.CutPrefix(pkg, modulePath+"/")
	if !ok || pkgRel == tenantPkg {
		return nil, nil
	}
	tenantPath := modulePath + "/" + tenantPkg
	for _, f := range pass.Files {
		name := pass.Fset.Position(f.Pos()).Filename
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		fileRel := pkgRel + "/" + filepath.Base(name)
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			for _, t := range testOnlyImports {
				if imp == modulePath+"/"+t && pkgRel != t {
					pass.Reportf(spec.Pos(), "H4: %s đúc được tenant.Grant — chỉ import từ tệp _test.go", t)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != tenantPath {
				return true
			}
			switch fn.Name() {
			case "WithID":
				if !listed(withIDAllowed, pkgRel, fileRel) {
					pass.Reportf(id.Pos(), "H4: tenant.WithID chỉ ở middleware xác thực và danh sách duyệt (tools/archlint/tenantgate.go) — "+
						"đổi sang tenant bên cấp dùng tenant.WithGrant với bằng chứng của pf/party")
				}
			case "MintGrant":
				if !listed(mintAllowed, pkgRel, fileRel) {
					pass.Reportf(id.Pos(), "H4: tenant.MintGrant chỉ ở mã kiểm cấp quyền chia sẻ (pf/party) — nhận tenant.Grant qua port (ShareGrant.Access, ValveGrants)")
				}
			}
			return true
		})
	}
	return nil, nil
}
