// Package orders là module mkg/orders (docs/design/mkg.md §5.5; docs/dev/g4-conventions.md): chứng từ
// thương mại của MKG — chiều mua (hợp đồng mua rơm với HTX theo tấn / theo cuộn, đơn mua, phiếu nhận
// hàng ↔ đơn mua > 3 % chỉ cảnh báo) và chiều bán (đơn bán, lịch giao, biên bản giao nhận liên kết
// phiếu cân CUSTOMER_IN, sự kiện nội bộ mkg.orders.delivery.accepted).
//
// Vùng nội bộ MKG: chỉ nối ở cmd/api-internal (vai orders_app) và cmd/worker-internal (consumer
// mkg.orders.weigh_ticket). Module khác chỉ import orders/port (R-02). Không có gì của module qua van:
// giá mua/bán, chiết khấu, khách, điều khoản ở lại vùng mkg (mkg.md §3.1).
package orders

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Module là mkg/orders đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai orders_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock) *Module { return NewWith(db, clk, Options{}) }

// Options là phụ thuộc tùy chọn của module (G4 tích hợp).
type Options struct {
	// Stock: tồn hub theo dạng hàng (hub/port.Stock) — cảnh báo thiếu tồn đơn bán (O-05). Nil → không kiểm.
	Stock hubport.Stock
	// Files (G4): kho đối tượng trong lãnh thổ (shared/objstore) cho tệp chứng từ — POST /v1/orders/files.
	Files objstore.Store
}

// NewWith tạo module với phụ thuộc tùy chọn.
func NewWith(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Stock: o.Stock, Files: o.Files}}
}

// RegisterApp gắn route (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route quản trị (MFA): dựng lại bảng chiếu (R-22).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// RebuildTenant dựng lại bảng chiếu của tenant từ sổ, tenant tường minh, không kiểm vai — CHỈ cho công cụ diễn
// tập R-22 trên bản sao database (cmd/rebuilddrill). Trả số dòng theo bảng chiếu.
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	return m.svc.RebuildTenant(ctx, tid)
}

// Contracts cho mkg/costing (quyết toán HTX) đọc điều khoản hợp đồng mua rơm (tenant tường minh).
func (m *Module) Contracts() port.Contracts { return m.svc }

// Deliveries cho mkg/costing đọc giao nhận đã chấp nhận (tenant tường minh, asOf).
func (m *Module) Deliveries() port.Deliveries { return m.svc }

// Receipts cho module mkg khác đọc kết quả kiểm phiếu nhận hàng ↔ đơn mua.
func (m *Module) Receipts() port.Receipts { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của orders cho cmd/worker-internal (vai orders_app, giao
// dịch inbox — không cần pool).
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của orders.
func NewConsumers(clk clock.Clock) Consumers { return Consumers{svc: &app.Service{Clock: clk}} }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → HUB_IN ↔ phiếu nhận hàng (kiểm đơn mua),
// CUSTOMER_IN → giao nhận (port.WeighTicketConsumer).
func (c Consumers) WeighTicket() inbox.Handler { return c.svc.HandleWeighTicket }
