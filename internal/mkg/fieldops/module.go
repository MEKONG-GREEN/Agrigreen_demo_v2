// Package fieldops là module mkg/fieldops (docs/design/mkg.md §5.1): lệnh gom có
// duyệt, ca tổ gom, đếm cuộn theo công đoạn (proto FM-03/04/09), xác nhận của
// HTX và đối soát chiều mua (RB-6), học kg/cuộn theo HTX, đồng bộ ngoại tuyến tối
// giản cho app hiện trường MKG, van collection_schedule / htx_collection_statement.
//
// Thuộc vùng nội bộ MKG: chỉ nối ở cmd/api-internal (cụm mkg, vai fieldops_app).
// Module khác chỉ import fieldops/port (R-02); vùng nền tảng không import gì của
// mkg (R-03). Thửa/HTX của nền tảng đọc qua Application API công khai (adapter).
package fieldops

import (
	"crypto/rand"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Options là phụ thuộc tùy chọn của module.
type Options struct {
	// Directory: nguồn thửa/HTX; nil → Unconfigured (đóng: lập lệnh gom trả 503).
	Directory port.PlatformDirectory
	// Weighed: khối lượng cân của lô gom; nil → chưa học kg/cuộn (dùng cấu hình / 20).
	Weighed port.WeighedMass
	// KgPerRoll: kg/cuộn cấu hình (tạm, chờ khách); nil = không có.
	KgPerRoll *decimal.Decimal
	// CursorKey: khóa HMAC con trỏ pull; rỗng → khóa ngẫu nhiên theo tiến trình.
	CursorKey []byte
}

// Module là mkg/fieldops đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai fieldops_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, o Options) *Module {
	if o.Directory == nil {
		o.Directory = adapter.Unconfigured{}
	}
	if len(o.CursorKey) == 0 {
		o.CursorKey = make([]byte, 32)
		_, _ = rand.Read(o.CursorKey)
	}
	return &Module{svc: &app.Service{DB: db, Clock: clk, Directory: o.Directory, Weighed: o.Weighed,
		ConfiguredKgPerRoll: o.KgPerRoll, CursorKey: o.CursorKey}}
}

// PlatformConfig là cấu hình tài khoản dịch vụ MKG gọi Application API.
type PlatformConfig = adapter.PlatformConfig

// NewPlatformDirectory tạo PlatformDirectory qua Application API công khai (OIDC client credentials).
func NewPlatformDirectory(cfg PlatformConfig, clk clock.Clock) port.PlatformDirectory {
	return adapter.NewPlatformHTTP(cfg, func() time.Time { return clk.Now() })
}

// WeighedFromMeasurement nối WeighedMass qua measurement/port.LotMass (cùng tiến trình).
func WeighedFromMeasurement(l mport.LotMass) port.WeighedMass { return adapter.LotMassWeighed{Lots: l} }

// RegisterApp gắn route (API nội bộ MKG, bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: fieldops không có route quản trị ở G3.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// KgPerRoll cho measurement đọc ước lượng kg/cuộn có mốc thời điểm (G3-2, tenant tường minh).
func (m *Module) KgPerRoll() port.KgPerRollEstimates { return m.svc }

// Consumers là handler sự kiện NỘI BỘ MKG của fieldops cho cmd/worker-internal (vai
// fieldops_app, giao dịch inbox — không cần pool).
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của fieldops.
func NewConsumers(clk clock.Clock) Consumers { return Consumers{svc: &app.Service{Clock: clk}} }

// WeighTicket: mkg.measurement.weigh_ticket.recorded → bảng đệm học kg/cuộn (G3-2).
func (c Consumers) WeighTicket() inbox.Handler { return c.svc.HandleWeighTicket }

// RollTotals cho module mkg khác (measurement RB-6, costing G4) đọc số cuộn của lô gom.
func (m *Module) RollTotals() port.RollTotals { return m.svc }
