// Package geo là module pf/geo (cap/geo): ranh giới hành chính dùng chung, kho/Hub và sức chứa, hub MKG qua
// van, đường bộ, log GPS (HTX US-GPS-03), lớp bản đồ gộp và bản đồ chuẩn hóa cho TMS (GIS FN-04/06/07/08/15/17).
//
// W3.3: thửa, ranh giới thửa, xung đột, ranh giới/vùng HTX, /v1/kn/plots và /v1/partner/plots chuyển sang
// pf/cooperative (schema pf_coop). geo không import module domain: dữ kiện thửa cho log GPS (port.PlotGeometry) và
// lớp HTX / thửa của bản đồ (port.MapLayers) do gốc lắp ráp nối bằng UsePlots, UseMapLayers; geo cung cấp phép
// tính trên ranh giới hành chính cho pf/cooperative (AdminGeometry).
//
// Module khác chỉ được import geo/port (R-02). Tệp này nối các lớp cho các gốc lắp ráp.
package geo

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "geo_app"

// Deps là port của module khác mà geo cần.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của người gọi (pf/party, ADR-0009). Bắt buộc.
	Scope partyport.ScopeResolver
	// Params: ngưỡng widget Hub, zoom tối thiểu lớp thửa, bản đồ nền (pf/masterdata). Thiếu → mặc định.
	Params params.Params
	// AdminUnits: kiểm mã đơn vị khi nhập ranh giới hành chính, kho/Hub, đường bộ. Bắt buộc cho ghi.
	AdminUnits partyport.AdminUnits
	// Farmers (tùy chọn): nông hộ của tài khoản nông dân (pf/accounts) — vai farmer đọc log
	// GPS của mình (HTX US-GPS-03). Gốc lắp ráp nối sau khi dựng pf/accounts (UseFarmers).
	Farmers port.FarmerHouseholds
}

// Module là pf/geo đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool đăng nhập của đơn vị triển khai (giao dịch chạy dưới r_geo).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	db = forModule(db)
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Params: deps.Params, Units: deps.AdminUnits,
		Farmers: deps.Farmers}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// GpsLog cho module khác ghi log GPS (HTX US-GPS-03) qua port; geo ghi trong giao dịch riêng.
func (m *Module) GpsLog() port.GpsLog { return m.svc }

// HubSink cho consumer van công bố (mkg hub_facility / hub_capacity) ghi mô hình đọc hub MKG.
func (m *Module) HubSink() port.HubSink { return m.svc }

// AdminGeometry cho pf/cooperative tính phần thửa nằm ngoài địa giới xã (W3.3) — gốc lắp ráp nối.
func (m *Module) AdminGeometry() port.AdminGeometry { return m.svc }

// UsePlots nối dữ kiện thửa của pf/cooperative cho log GPS (xã/HTX của lô, khoảng cách tới lô, thửa của nông hộ
// cho vai farmer) — W3.3.
func (m *Module) UsePlots(p port.PlotGeometry) { m.svc.Plots = p }

// UseMapLayers nối lớp ranh giới HTX và lớp thửa của pf/cooperative cho GET /v1/geo/layers và
// GET /v1/geo/standard-map — W3.3.
func (m *Module) UseMapLayers(l port.MapLayers) { m.svc.MapLayers = l }

// UseWaterways nối lớp tuyến đường thủy (pf/waterway) cho bản đồ chuẩn hóa (GIS US-INT-02).
func (m *Module) UseWaterways(l port.Layer) { m.svc.Waterways = l }

// HubCapacitySource (G5, ADR-0022) là nguồn của sản phẩm dữ liệu hub_capacity cho cmd/api-public.
func (m *Module) HubCapacitySource() apiclientport.HubCapacitySource { return m.svc }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho consumer van hub (kênh valve).
// cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
}

// UseSharing nối cổng cấp quyền van của pf/party (ADR-0020): định tuyến van hub_facility / hub_capacity về tenant
// bên cấp. (Bề mặt đối tác /v1/partner/plots chuyển sang pf/cooperative ở W3.3.)
func (m *Module) UseSharing(vg partyport.ValveGrants) { m.svc.ValveGrants = vg }

// UseFarmers nối liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) sau khi module đó được dựng.
func (m *Module) UseFarmers(f port.FarmerHouseholds) { m.svc.Farmers = f }

// HubValveHandler là inbox.Handler của consumer port.HubValveConsumer (van công bố MKG
// hub_facility / hub_capacity → mô hình đọc hub, kiểm payload theo hợp đồng lúc chạy).
func (m *Module) HubValveHandler() inbox.Handler { return m.svc.HandleHubPublication }

// LayerGeometry cho pf/metrics: hình học as-of theo id dòng sổ facility_event (phát lại lớp GIS). Loại
// plot_boundary, cooperative_boundary do pf/cooperative phân giải — gốc lắp ráp gộp hai bộ phân giải (W3.3).
func (m *Module) LayerGeometry() metricsport.GeometryResolver { return app.GeometrySource{Svc: m.svc} }
