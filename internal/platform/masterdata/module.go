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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "masterdata_app"

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
func (m *Module) AdminUnits() port.AdminUnits { return app.UnitsPort{Svc: m.svc} }

// Params cho module khác: tham số đã duyệt theo thời điểm của sự việc.
func (m *Module) Params() port.Params { return app.ParamsPort{Svc: m.svc} }

// Catalogs cho module khác: danh mục dùng chung.
func (m *Module) Catalogs() port.Catalogs { return app.CatalogsPort{Svc: m.svc} }

// Seed nạp dữ liệu nền nhúng cho một tenant (chạy lặp an toàn) — gọi khi tạo tenant.
func (m *Module) Seed(ctx context.Context, tenantID uuid.UUID) (app.SeedResult, error) {
	return m.svc.Seed(ctx, tenantID)
}
