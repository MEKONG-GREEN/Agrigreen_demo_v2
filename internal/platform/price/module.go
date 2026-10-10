// Package price là module năng lực pf/price (`cap/price`, kind capability — kiến trúc v4.1 §26, ADR-0049, W11.71):
// chuỗi giá có hiệu lực, ngưỡng, bảng giá ngày có phiên bản, công bố là quyền riêng. Bên dùng đầu tiên: Báo giá nông
// sản của App Khuyến nông (BRD v1.3, Phòng Thông tin – Truyền thông); HTX, MKG, NH theo ma trận §5.1.
//
// Khung W11.71 (pf 000138):
//   - Schema pf_price (sổ chỉ ghi thêm, RLS theo tenant) và pf_price_pub (view công bố — view đầu tiên ở W11.76).
//   - Danh mục nền có hiệu lực theo ngày: đơn vị tính, nhóm hàng (port.Catalog); danh mục mặc định ghi theo yêu cầu.
//   - Cấu hình theo bên dùng (port.Profile) ở gốc lắp ráp — không so tên ứng dụng (R-MOD-04).
//   - Cấm float trong toàn module (R-CODE-01, agnofloat floatFreeZones): giá, % là decimal.
//   - HTTP: /v1/price/status, /v1/price/catalog, /v1/price/catalog/defaults (Application API).
//
// W11.73 (pf 000141): mặt hàng theo thời kỳ — app.ItemService (port.Items), nhóm hàng, gán xã, đề xuất, bộ theo vụ,
// "thay thế cho"; điểm nối app.ItemHooks (SetItemHooks) cho W11.72/W11.74/W11.75.
//
// Module khác chỉ import price/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package price

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_price (ADR-0028).
var DBModule = app.DBModule

// Module là pf/price đã nối.
type Module struct {
	svc   *app.Service
	items *app.ItemService // W11.73 mặt hàng theo thời kỳ
}

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_price. Cấu hình bên dùng sai → lỗi.
func New(b tenant.Beginner, clk clock.Clock, profiles []port.Profile) (*Module, error) {
	if err := app.ValidateProfiles(profiles); err != nil { // W11.74: lịch/ngưỡng mặc định của Profile
		return nil, err
	}
	svc, err := app.NewService(db.ForModule(b, DBModule), clk, profiles)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc, items: app.NewItemService(svc)}, nil
}

// Catalog cho module khác: danh mục nền (đơn vị tính, nhóm hàng) có hiệu lực theo ngày.
func (m *Module) Catalog() port.Catalog { return m.svc }

// Items cho module khác (KHTC — Q-082; phiếu báo giá W11.75): mặt hàng theo thời kỳ, mặt hàng của xã theo ngày.
func (m *Module) Items() port.Items { return m.items }

// SetItemHooks nối điểm mở rộng của mặt hàng (W11.72 cờ QTNV, W11.74 lịch báo giá, W11.75 đã có giá/phiếu) — gọi ở gốc
// lắp ráp trước khi phục vụ.
func (m *Module) SetItemHooks(h app.ItemHooks) { m.items.Hooks = h }

// WireItemCalendar nối "ngày báo giá kế tiếp" của mặt hàng (BR-02) với lịch báo giá W11.74 (port.Calendar) — gọi ở
// gốc lắp ráp; giữ các hook khác.
func (m *Module) WireItemCalendar() { m.items.Hooks.NextDay = m.svc.NextQuoteDay }

// RegisterApp gắn route Application API /v1/price/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.RegisterApp(rg)
	handler.ItemHandler{Svc: m.items}.RegisterApp(rg) // W11.73
}

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
