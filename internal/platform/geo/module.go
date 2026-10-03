// Package geo là module pf/geo: thửa ruộng có ranh giới theo phiên bản (sổ chỉ
// ghi thêm), kiểm chồng lấn, nhập GeoJSON/KML, ranh giới hành chính dùng chung
// và đồng bộ ngoại tuyến (KN FN-05, HTX FN-07, GIS FN-05/FN-07); G2: ranh giới
// HTX, kho/Hub và sức chứa, hub MKG qua van, đường bộ, log GPS (GIS FN-04/06/08/15,
// HTX US-GPS-03).
//
// Module khác chỉ được import geo/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package geo

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "geo_app"

// Deps là port của module khác mà geo cần.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của người gọi (pf/party, ADR-0009). Bắt buộc.
	Scope partyport.ScopeResolver
	// Params: ngưỡng chồng lấn geo.overlap_tolerance_pct (pf/masterdata). Thiếu → 1%.
	Params mdport.Params
	// AdminUnits: kiểm mã xã khi tạo thửa, mã đơn vị khi nhập ranh giới hành chính. Bắt buộc cho ghi.
	AdminUnits mdport.AdminUnits
	// Orgs (tùy chọn): kiểm HTX tồn tại, đang hoạt động khi tạo/gán thửa.
	Orgs partyport.Orgs
	// Households (tùy chọn): kiểm nông hộ là thành viên đang hoạt động của HTX sở hữu thửa khi gán.
	Households partyport.Households
	// CoopMarkers (tùy chọn): điểm vị trí HTX (pf_party.cooperative.location) làm
	// marker/dự phòng cho lớp ranh giới HTX (GIS US-BND-02). pf/party cài ở đợt 2.
	CoopMarkers port.CoopMarkers
	// Farmers (tùy chọn): nông hộ của tài khoản nông dân (pf/accounts) — vai farmer đọc log
	// GPS của mình (HTX US-GPS-03). Gốc lắp ráp nối sau khi dựng pf/accounts (UseFarmers).
	Farmers port.FarmerHouseholds
	// Vault (tùy chọn, KN v7.7 FN-XA-03.05): tên nông dân của thửa vào kho định danh. Có thể nối sau bằng UseKN.
	Vault port.FarmerIdentity
}

// Module là pf/geo đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai geo_app.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Params: deps.Params, Units: deps.AdminUnits, Orgs: deps.Orgs, Households: deps.Households,
		Markers: deps.CoopMarkers, Farmers: deps.Farmers, Vault: deps.Vault}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Plots cho module khác tra cứu thửa (qua port), vd kiểm vị trí GPS ≤ 50 m (HTX US-GPS-01).
func (m *Module) Plots() port.Plots { return m.svc }

// PlotStats cho module khác đọc thống kê thửa đã số hoá (qua port, G-02/G-03).
func (m *Module) PlotStats() port.PlotStats { return m.svc }

// GpsLog cho module khác ghi log GPS (HTX US-GPS-03) qua port; geo ghi trong giao dịch riêng.
func (m *Module) GpsLog() port.GpsLog { return m.svc }

// HubSink cho consumer van công bố (mkg hub_facility / hub_capacity) ghi mô hình đọc hub MKG.
func (m *Module) HubSink() port.HubSink { return m.svc }

// UseWaterways nối lớp tuyến đường thủy (pf/waterway) cho bản đồ chuẩn hóa (GIS US-INT-02).
func (m *Module) UseWaterways(l wwport.Layer) { m.svc.Waterways = l }

// HubCapacitySource (G5, ADR-0022) là nguồn của sản phẩm dữ liệu hub_capacity cho cmd/api-public.
func (m *Module) HubCapacitySource() apiclientport.HubCapacitySource { return m.svc }

// PlotFacts (G5) tra dữ kiện nhiều thửa cho sản phẩm tổng hợp collection_volume của pf/cropcycle.
func (m *Module) PlotFacts() port.PlotFacts { return m.svc }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho consumer van hub (kênh valve).
// cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meteringport.Checkpoint, mode meteringport.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
}

// UseSharing nối cổng cấp quyền chia sẻ của pf/party (ADR-0020): bề mặt đối tác /v1/partner/plots
// và định tuyến van hub_facility / hub_capacity về tenant bên cấp.
func (m *Module) UseSharing(pa partyport.PartnerAccess, vg partyport.ValveGrants) {
	m.svc.Sharing, m.svc.ValveGrants = pa, vg
}

// UseKN nối kho định danh (tên nông dân của thửa) và danh bạ tên người thao tác (Log) cho route /v1/kn/plots
// (KN v7.7 FN-XA-03.05). names có thể nil.
func (m *Module) UseKN(v port.FarmerIdentity, names partyport.ActorNames) {
	m.svc.Vault, m.svc.Names = v, names
}

// UseFarmers nối liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) sau khi module đó được dựng.
func (m *Module) UseFarmers(f port.FarmerHouseholds) { m.svc.Farmers = f }

// PlotHouseholds cho gốc lắp ráp tìm nông hộ có thửa trong các xã (cảnh báo khẩn, HTX US-NEWS-03).
func (m *Module) PlotHouseholds() port.PlotHouseholds { return m.svc }

// CoopCentroids cho gốc lắp ráp đọc tâm ranh giới HTX (nguồn cung của pf/simulation).
func (m *Module) CoopCentroids() port.CoopCentroids { return m.svc }

// HubValveHandler là inbox.Handler của consumer port.HubValveConsumer (van công bố MKG
// hub_facility / hub_capacity → mô hình đọc hub, kiểm payload theo hợp đồng lúc chạy).
func (m *Module) HubValveHandler() inbox.Handler { return m.svc.HandleHubPublication }

// SyncSource là chiều xuống entity "plot" cho pf/sync.
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// SyncSink là chiều lên entity "plot_boundary" cho pf/sync.
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }

// MetricsSource cho pf/metrics (qua gốc lắp ráp): số thửa đã số hoá như hệ biết lúc hết ngày D.
func (m *Module) MetricsSource() metricsport.Source { return app.MetricsSource{Svc: m.svc} }

// LayerGeometry cho pf/metrics: hình học as-of theo id dòng sổ bất biến (phát lại lớp GIS).
func (m *Module) LayerGeometry() metricsport.GeometryResolver { return app.MetricsSource{Svc: m.svc} }
