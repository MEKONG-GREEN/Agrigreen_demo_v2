package assembly

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
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

// wireExport nối KHTC Mẫu 1–5 và HRM MS05/MS06 vào cap/export. Mỗi bên dùng
// kiểm quyền nghiệp vụ trước khi gọi, không cấp quyền HRM từ màn quản trị mẫu chung.
func wireExport(unit tenant.Beginner, clk clock.Clock, kn *extension.Module, hr *hrm.Module) (*export.Module, error) {
	ex, err := export.New(unit, clk, export.Deps{Access: KNExportAccess, Templates: []exportport.TemplateRegistration{extension.ExportTemplates(), hrm.ExportTemplates()}})
	if err != nil {
		return nil, err
	}
	kn.UseExporter(ex.Exporter())
	hr.UseExporter(ex.Exporter())
	return ex, nil
}
