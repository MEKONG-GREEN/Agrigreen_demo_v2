package assembly

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// KNExportAccess: màn quản trị mẫu văn bản và lịch sử xuất (/v1/exports/*) của App Khuyến nông — Admin trong ngữ cảnh
// phiên KN; không ngữ cảnh KN thì quản trị nền tảng (W10.17, ADR-0051).
var KNExportAccess = exportport.AccessFunc(func(ctx context.Context) bool {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return false
	}
	if p.KN != nil {
		return p.KN.Role == authz.KNAdmin
	}
	return p.Has(authz.RolePlatformAdmin)
})

// wireExport dựng pf/export (`cap/export`) với mẫu của bên dùng và nối vào bên dùng: KHTC Mẫu 1–5 (W10.17). Bên dùng sau
// (CNTS BM-11, HCTH MS05/06, Mẫu KNQG) thêm TemplateRegistration và gọi Use* tương ứng ở đây.
func wireExport(unit tenant.Beginner, clk clock.Clock, kn *extension.Module) (*export.Module, error) {
	ex, err := export.New(unit, clk, export.Deps{Access: KNExportAccess, Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		return nil, err
	}
	kn.UseExporter(ex.Exporter())
	return ex, nil
}
