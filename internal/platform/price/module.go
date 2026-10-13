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
// W11.72 (pf 000145): xã báo giá ↔ ĐVHC có hiệu lực, nhóm vùng, phân công cán bộ Xã / Cấp Tỉnh theo ngày (port.Points);
// cờ Quản trị nghiệp vụ (Profile.StewardDuty); nhắc nhập giá tới cán bộ Xã phụ trách (app.PointReminders).
//
// W11.75 (pf 000148): phiếu báo giá + xác minh — app.TicketService (port.Tickets): Xã nhập / gửi trước giờ khóa, cảnh báo
// tạm tính, xác minh hai chiều, nhập thay, hiệu chỉnh, sửa ngày, xóa mềm, in phiếu; nối sẵn ItemHooks.HasPrice/HasSheets
// và PointHooks.Submitted; điểm nối app.TicketHooks.Board cho W11.76 (SetTicketHooks).
//
// W11.76 (pf 000151, 000152): duyệt / mở duyệt bảng giá ngày có phiên bản — khóa "Đang duyệt", ảnh chụp dòng bảng giá
// (CT-04) + phiếu của mỗi phiên bản, khóa tuần lưu lúc duyệt, sự kiện pf.price.board.published, port.Boards, view
// pf_price_pub.v1_board_daily / v1_ticket_week; New nối TicketHooks.Board/Approved (giá tham chiếu chỉ từ ngày đã duyệt).
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
	svc     *app.Service
	items   *app.ItemService   // W11.73 mặt hàng theo thời kỳ
	tickets *app.TicketService // W11.75 phiếu báo giá + xác minh
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
	svc.Reminders = app.PointReminders{S: svc} // W11.72: nhắc cán bộ Xã phụ trách từng xã báo giá
	m := &Module{svc: svc, items: app.NewItemService(svc), tickets: app.NewTicketService(svc)}
	// W11.75: "đã gửi phiếu" cho nhắc lần 2 / ngưng xã (W11.72), "đã có giá / đã có phiếu gửi" cho mặt hàng (W11.73).
	svc.Points.Submitted = m.tickets.Submitted
	m.items.Hooks.HasPrice = m.tickets.HasPrice
	m.items.Hooks.HasSheets = m.tickets.Submitted
	m.tickets.WireBoard() // W11.76: Cấp Tỉnh chỉ sửa khi Chưa duyệt / Đang mở duyệt; CT-02 chỉ từ ngày đã duyệt
	return m, nil
}

// Points cho module khác (W11.75 phiếu, W11.77 tiến độ): xã báo giá đang hoạt động, người phụ trách theo ngày (W11.72).
func (m *Module) Points() port.Points { return m.svc }

// UseOfficers nối danh bạ cán bộ KN (gốc lắp ráp — pf/accounts) cho phân công xã báo giá (W11.72): kiểm người được phân
// công đúng cấp, tên ở danh sách xã. Không nối → không kiểm, không tên.
func (m *Module) UseOfficers(o port.Officers) { m.svc.Points.Officers = o }

// SetPointHooks thay toàn bộ điểm nối của xã báo giá. New đã nối Submitted với phiếu W11.75 ("xã đã gửi phiếu") — bên
// gọi giữ trường đó nếu chỉ muốn đổi danh bạ (dùng UseOfficers).
func (m *Module) SetPointHooks(h app.PointHooks) { m.svc.Points = h }

// Tickets cho module khác (W11.76 bảng giá ngày, W11.77 tiến độ): phiếu và dòng giá đã gửi của một ngày.
func (m *Module) Tickets() port.Tickets { return m.tickets }

// SetTicketHooks thay điểm mở rộng của phiếu. New đã nối Board/Approved với bảng giá ngày W11.76 — chỉ dùng để thay
// (kiểm thử, bên dùng khác).
func (m *Module) SetTicketHooks(h app.TicketHooks) { m.tickets.Hooks = h }

// Boards cho module khác (W11.77 Bảng giá tổng hợp, dashboard, báo cáo): phiên bản bảng giá đã duyệt của một ngày.
func (m *Module) Boards() port.Boards { return m.tickets }

// UseWeeks nối lịch kỳ hệ thống (W11.03, pf/extension) cho khóa tuần lưu lúc duyệt bảng giá (W11.76). Không nối → quy
// tắc mặc định QĐ-6.
func (m *Module) UseWeeks(w port.Weeks) { m.tickets.Weeks = w }

// UseTablePDF nối bộ dựng PDF (pf/reports) cho "Tải PDF" phiếu báo giá (FN-XA-03.02). Không nối → 503.
func (m *Module) UseTablePDF(p port.TablePDF) { m.tickets.PDF = p }

// Catalog cho module khác: danh mục nền (đơn vị tính, nhóm hàng) có hiệu lực theo ngày.
func (m *Module) Catalog() port.Catalog { return m.svc }

// Items cho module khác (KHTC — Q-082; phiếu báo giá W11.75): mặt hàng theo thời kỳ, mặt hàng của xã theo ngày.
func (m *Module) Items() port.Items { return m.items }

// SetItemHooks thay điểm mở rộng của mặt hàng (W11.72 cờ QTNV, W11.74 lịch báo giá, W11.75 đã có giá/phiếu — New đã nối
// HasPrice/HasSheets với phiếu) — gọi ở gốc lắp ráp trước khi phục vụ.
func (m *Module) SetItemHooks(h app.ItemHooks) { m.items.Hooks = h }

// WireItemCalendar nối "ngày báo giá kế tiếp" của mặt hàng (BR-02) với lịch báo giá W11.74 (port.Calendar) — gọi ở
// gốc lắp ráp; giữ các hook khác.
func (m *Module) WireItemCalendar() { m.items.Hooks.NextDay = m.svc.NextQuoteDay }

// RegisterApp gắn route Application API /v1/price/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.RegisterApp(rg)
	handler.ItemHandler{Svc: m.items}.RegisterApp(rg)     // W11.73
	handler.PointHandler{Svc: m.svc}.RegisterApp(rg)      // W11.72
	handler.TicketHandler{Svc: m.tickets}.RegisterApp(rg) // W11.75
	handler.BoardHandler{Svc: m.tickets}.RegisterApp(rg)  // W11.76
}

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
