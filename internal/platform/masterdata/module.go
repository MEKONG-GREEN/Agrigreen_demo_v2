// Package masterdata là module dữ liệu chủ pf/masterdata (mục 16): đơn vị hành
// chính có hiệu lực theo thời gian, danh mục dùng chung, tham số có phiên bản và
// phê duyệt hai người (R-56).
//
// Module khác chỉ được import masterdata/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package masterdata

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "masterdata_app"

// ParamApprovalKind là loại yêu cầu duyệt phiên bản tham số trong cap/workflow (W6.1).
const ParamApprovalKind = app.ApprovalKind

// Module là pf/masterdata đã nối.
type Module struct {
	svc *app.Service
}

// New tạo module với pool của vai masterdata_app.
func New(db tenant.Beginner, clk clock.Clock) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk}}
}

// RegisterApp gắn route chỉ đọc của Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// AdminUnits cho module khác (qua port), vd party.ScopeResolver dùng Descendants.
func (m *Module) AdminUnits() partyport.AdminUnits { return app.UnitsPort{Svc: m.svc} }

// AdminUnitHistory cho module khác: tra theo thời điểm, chuỗi kế thừa và ánh xạ mã
// qua các lần sắp xếp (R-DB-09, ADR-0032) — cho báo cáo nhóm theo đơn vị tại kỳ.
func (m *Module) AdminUnitHistory() port.AdminUnitHistory { return app.UnitsPort{Svc: m.svc} }

// Params cho module khác: tham số đã duyệt theo thời điểm của sự việc.
func (m *Module) Params() params.Params { return app.ParamsPort{Svc: m.svc} }

// Catalogs cho module khác: danh mục dùng chung.
func (m *Module) Catalogs() port.Catalogs { return app.CatalogsPort{Svc: m.svc} }

// SyncSource cho pf/sync: entity "catalog_item" và "admin_unit" chiều xuống.
func (m *Module) SyncSource() syncport.Source { return app.Sync{Svc: m.svc} }

// UseStaffDirectory nối danh bạ cán bộ của pf/extension (tên người đặt/duyệt
// tham số, G-07). Gốc lắp ráp gọi sau khi dựng pf/extension; không nối → tên null.
func (m *Module) UseStaffDirectory(d port.StaffNames) { m.svc.Staff = d }

// UseFormUsage nối P3 (pf/reports) cho cột "biểu đang dùng" của danh mục nông nghiệp (KN v7.7 FN-AD-04.05).
func (m *Module) UseFormUsage(f port.FormUsage) { m.svc.Forms = f }

// UseApprovals (W6.1) nối cap/workflow (pf/workflow): gửi duyệt/duyệt/từ chối phiên bản tham số ghi thêm yêu cầu
// và quyết định vào sổ pf_workflow cùng giao dịch. Không nối → luồng cũ.
func (m *Module) UseApprovals(a wfport.Approvals) { m.svc.Approvals = a }

// Seed nạp dữ liệu nền nhúng cho một tenant (chạy lặp an toàn) — gọi khi tạo tenant.
func (m *Module) Seed(ctx context.Context, tenantID uuid.UUID) (app.SeedResult, error) {
	return m.svc.Seed(ctx, tenantID)
}
