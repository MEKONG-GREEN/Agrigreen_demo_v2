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
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
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
}

// Module là pf/geo đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai geo_app.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Params: deps.Params, Units: deps.AdminUnits, Orgs: deps.Orgs, Households: deps.Households,
		Markers: deps.CoopMarkers}}
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

// SyncSource là chiều xuống entity "plot" cho pf/sync.
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// SyncSink là chiều lên entity "plot_boundary" cho pf/sync.
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }
