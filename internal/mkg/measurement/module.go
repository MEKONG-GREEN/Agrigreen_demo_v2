// Package measurement là module mkg/measurement (tài liệu kiến trúc mục 13;
// docs/design/mkg.md §5.2): sổ thiết bị cân có duyệt, phiên cân (cửa sổ bộ
// đếm), phiếu cân ba điểm HUB_IN / HUB_OUT / CUSTOMER_IN, dung sai và định mức
// hao hụt có phiên bản, đối soát theo lô, sản lượng quy đổi, tranh chấp, nhập số
// cân của khách, van mrv_collection_evidence. Event sourcing mức B: SỔ LÀ NGUỒN.
//
// Vùng nội bộ MKG: chỉ nối ở cmd/api-internal (vai measurement_app) và cmd/ingest
// (vai ingest_mkg, qua NewIngest). Module khác chỉ import measurement/port (R-02).
package measurement

import (
	"github.com/gin-gonic/gin"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Deps là phụ thuộc sang module mkg khác (qua port, cùng tiến trình).
type Deps struct {
	// Rolls: số cuộn từ mkg/fieldops; nil → app.NoRolls (đối soát ghi pending_rolls).
	Rolls port.RollTotals
	// Origins: nguồn gốc lô từ mkg/hub (thửa + tỷ lệ) cho van MRV; nil → van MRV từ chối.
	Origins hubport.Lots
}

// Module là mkg/measurement đã nối.
type Module struct {
	svc *app.Service
	w   *app.Writer
}

// New tạo module với pool của vai measurement_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	w := &app.Writer{DB: db, Clock: clk}
	return &Module{w: w, svc: &app.Service{DB: db, Clock: clk, Writer: w, Rolls: deps.Rolls, Origins: deps.Origins}}
}

// RegisterApp gắn route vận hành (bề mặt ứng dụng).
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route đăng ký / duyệt (bề mặt quản trị, MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// LotMass cho hub / costing đọc khối lượng chuẩn của lô (port.LotMass).
func (m *Module) LotMass() port.LotMass { return m.w }

// Tickets là TicketWriter của module (vai measurement_app).
func (m *Module) Tickets() port.TicketWriter { return m.w }

// Ingest gom các cổng mà cmd/ingest dùng — dựng bằng NewIngest với pool vai
// ingest_mkg (chỉ có quyền ghi phiếu, đọc sổ thiết bị / phiên, cập nhật sức khỏe).
type Ingest struct {
	Tickets  port.TicketWriter
	Devices  port.DeviceRegistry
	Health   port.DeviceHealth
	Sessions port.WeighingSessions
	LotMass  port.LotMass
}

// NewIngest dựng cổng cho cmd/ingest với pool vai port.IngestRole (cụm mkg).
func NewIngest(db tenant.Beginner, clk clock.Clock) Ingest {
	w := &app.Writer{DB: db, Clock: clk}
	return Ingest{Tickets: w, Devices: w, Health: w, Sessions: w, LotMass: w}
}
