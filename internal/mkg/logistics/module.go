// Package logistics là module mkg/logistics (docs/design/mkg.md §5.4): chuyến xe / ghe / sà lan với
// trạng thái chỉ tiến, chặng đường thủy / đường bộ có ảnh chụp định tuyến nguyên văn + SHA-256, danh mục
// phương tiện có phiên bản (proto GH-01..03), tham số cước có phiên bản (người đặt ≠ người duyệt), chuyến
// sinh từ lượt xuống ghe (proto FM-05) và đóng bằng phiếu cân; phát mkg.logistics.trip.completed (nội bộ).
//
// Thuộc vùng nội bộ MKG: chỉ nối ở cmd/api-internal và cmd/worker-internal (cụm mkg, vai logistics_app).
// Module khác chỉ import logistics/port (R-02); không import vùng nền tảng (R-03) — mạng đường thủy tới
// qua bề mặt đối tác /v1/partner/waterway/route (ADR-0020).
package logistics

import (
	"time"

	"github.com/gin-gonic/gin"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Options là phụ thuộc tùy chọn của module.
type Options struct {
	// Waterway: định tuyến đường thủy (bề mặt đối tác); nil → Unconfigured (mọi chặng thủy dự phòng Haversine).
	Waterway port.WaterwayRoutes
	// Road: định tuyến đường bộ; nil → Haversine × tham số #10 (chưa có OSRM trong lãnh thổ, RB-7).
	Road port.RoadRoutes
}

// Module là mkg/logistics đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai logistics_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Waterway: o.Waterway, Road: o.Road}}
}

// PlatformConfig là cấu hình tài khoản dịch vụ MKG gọi Application API (cùng biến với fieldops).
type PlatformConfig = adapter.PlatformConfig

// NewPlatformWaterway tạo WaterwayRoutes qua GET/POST /v1/partner/waterway/route (cấp quyền waterway_routes).
func NewPlatformWaterway(cfg PlatformConfig, clk clock.Clock) port.WaterwayRoutes {
	return adapter.NewWaterwayHTTP(cfg, func() time.Time { return clk.Now() })
}

// RegisterApp gắn route (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route quản trị (MFA): dựng lại bảng chiếu.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Trips cho mkg/costing, mkg/orders đọc chuyến (tenant tường minh).
func (m *Module) Trips() port.Trips { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của logistics cho cmd/worker-internal (vai logistics_app,
// giao dịch inbox — không cần pool).
type Consumers struct{ c app.Consumer }

// NewConsumers dựng handler consumer; kg nil → 20 kg/cuộn cho tấn ước FM-05.
func NewConsumers(clk clock.Clock, kg fieldport.KgPerRollEstimates) Consumers {
	return Consumers{c: app.Consumer{Clock: clk, KgPerRoll: kg}}
}

// RollCount: mkg.fieldops.roll_count.recorded (boat_load) → chuyến đường thủy planned (proto FM-05).
func (c Consumers) RollCount() inbox.Handler { return c.c.HandleRollCount }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → đóng chuyến với tấn thật.
func (c Consumers) WeighTicket() inbox.Handler { return c.c.HandleWeighTicket }
