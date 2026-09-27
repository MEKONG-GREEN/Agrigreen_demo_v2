// Package mechanization là module Bản đồ số cơ giới hóa (CGH BRD v1.3, ADR-0011):
// chủ sở hữu máy (FN-04), hồ sơ máy (FN-05), diện tích & lịch mùa vụ theo vụ
// (FN-06, QT-03), cân đối cung–cầu lưu theo vụ (FN-09, QT-01/QT-02) và API đọc
// cho cổng Cục KTHT / Admin (FN-08, FN-10, FN-11).
//
// Ngoài phạm vi G1: FN-07 kéo dữ liệu từ App HTX (App HTX thuộc G2 — chỉ có
// đường nhập tay; port.AreaFeed đã áp QT-03 cho bộ đồng bộ G2), FN-12 nhật ký
// truy cập bị từ chối và lỗi đồng bộ (log nền tảng), FN-01 tài khoản (Keycloak).
//
// Module khác chỉ được import mechanization/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package mechanization

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "mechanization_app"

// Deps là port của module khác mà mechanization cần (xem app.Deps).
type Deps = app.Deps

// Module là pf/mechanization đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai mechanization_app. Thiếu port phụ thuộc thì
// thao tác cần nó trả 503 mechanization.dependency_unavailable.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Deps: deps}}
}

// RegisterApp gắn route Application API (đọc — Cục KTHT và Admin).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (ghi dữ liệu chủ, vụ mùa, chạy cân đối; MFA bắt buộc ở router).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// AreaFeed cho bộ đồng bộ App HTX (FN-07, G2) áp diện tích vụ theo QT-03.
func (m *Module) AreaFeed() port.AreaFeed { return m.svc }
