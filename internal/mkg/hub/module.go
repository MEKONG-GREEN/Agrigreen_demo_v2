// Package hub là module mkg/hub (docs/design/mkg.md mục 4): dữ liệu chủ hub /
// kho / bãi có phiên bản và phê duyệt, lô có N nguồn kèm tỷ lệ, mẻ biến đổi
// (sấy, nén), dịch chuyển tồn bất biến, tồn theo lô, xuất theo rủi ro xuống
// cấp, và van công bố hub_facility / hub_capacity sang nền tảng.
//
// Thuộc vùng nội bộ MKG: chỉ được nối ở gốc lắp ráp cmd/api-internal (cụm mkg,
// vai hub_app). Module khác chỉ được import hub/port (R-02); vùng nền tảng
// không được import gì của mkg (R-03).
package hub

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = "hub_app"

// Module là mkg/hub đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai hub_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk}}
}

// RegisterApp gắn route vận hành (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route dữ liệu chủ và phê duyệt (bề mặt quản trị, MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Lots cho module mkg khác đọc nguồn gốc và tồn của lô (qua port).
func (m *Module) Lots() port.Lots { return m.svc }
