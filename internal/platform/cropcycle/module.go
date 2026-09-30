// Package cropcycle là module pf/cropcycle của App HTX (ADR-0011): danh mục quy
// trình SOP có phiên bản, vụ mùa theo SOP, nhật ký canh tác dạng sổ (bằng chứng
// MRV), duyệt nhật ký, khai báo sản lượng (decimal, shared/mass) và bảng điều
// hành dự kiến – thực tế (HTX FN-03, FN-08, FN-10..12, FN-16).
//
// Module khác chỉ được import cropcycle/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package cropcycle

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "cropcycle_app"

// Deps là port của module khác mà cropcycle cần.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của cán bộ KN (pf/party, ADR-0009).
	Scope partyport.ScopeResolver
	// Orgs (tùy chọn): kiểm HTX đang hoạt động khi mở vụ, tạo SOP riêng của HTX.
	Orgs partyport.Orgs
	// Plots: thửa, diện tích, khoảng cách GPS (pf/geo). Bắt buộc cho ghi.
	Plots geoport.Plots
	// Params (tùy chọn): cropcycle.gps_match_tolerance_m, cropcycle.yield_anomaly_max_t_per_ha[:giống].
	Params mdport.Params
	// Catalogs (tùy chọn): rice_variety, season, machine_type.
	Catalogs mdport.Catalogs
	// Farmers: liên kết tài khoản nông dân ↔ nông hộ (pf/accounts, G2). Thiếu → vai farmer nhận 503.
	Farmers port.FarmerHouseholds
	// GpsLog (tùy chọn): log GPS của pf/geo cho consumer port.GpsLogConsumer (HTX US-GPS-03).
	GpsLog geoport.GpsLog
}

// Module là pf/cropcycle đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai cropcycle_app.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Orgs: deps.Orgs, Plots: deps.Plots, Params: deps.Params,
		Catalogs: deps.Catalogs, Farmers: deps.Farmers, GpsLog: deps.GpsLog}}
}

// UseSharing nối cổng cấp quyền chia sẻ của pf/party (ADR-0020): bề mặt đối tác /v1/partner/*
// (ngày thu hoạch của vụ, luồng phản hồi HTX) và định tuyến van collection_* về tenant bên cấp.
func (m *Module) UseSharing(pa partyport.PartnerAccess, vg partyport.ValveGrants) {
	m.svc.Sharing, m.svc.ValveGrants = pa, vg
}

// UseFarmerHouseholds nối liên kết tài khoản ↔ nông hộ sau khi dựng pf/accounts.
func (m *Module) UseFarmerHouseholds(f port.FarmerHouseholds) { m.svc.Farmers = f }

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Stats cho module khác: vụ mở / nhật ký chờ duyệt theo HTX, diện tích vụ (qua port).
func (m *Module) Stats() port.Stats { return m.svc }

// ReportData cho pf/reports (qua gốc lắp ráp): báo cáo sản lượng HTX (HTX US-DASH-02, US-DASH-03).
func (m *Module) ReportData() port.ReportData { return app.ReportData{Svc: m.svc} }

// UseGpsLog nối log GPS của pf/geo (consumer port.GpsLogConsumer).
func (m *Module) UseGpsLog(g geoport.GpsLog) { m.svc.GpsLog = g }

// SupplyStats cho gốc lắp ráp: sản lượng lúa HTX × vụ (nguồn cung của pf/simulation).
func (m *Module) SupplyStats() port.SupplyStats { return m.svc }

// CollectionValveHandler là inbox.Handler của consumer port.CollectionValveConsumer (van MKG
// collection_schedule / htx_collection_statement → sổ nhận + bảng chiếu, vai cropcycle_app).
func (m *Module) CollectionValveHandler() inbox.Handler { return m.svc.HandleCollectionValve }

// GpsLogHandler là inbox.Handler của consumer port.GpsLogConsumer (vai cropcycle_app).
func (m *Module) GpsLogHandler() inbox.Handler { return m.svc.HandleGpsLog }

// SyncSource là chiều xuống entity sop_version, season, farm_log cho pf/sync.
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// SyncSink là chiều lên entity season_open, farm_log, farm_log_batch, harvest cho pf/sync.
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }

// MetricsSource cho pf/metrics (qua gốc lắp ráp): diện tích vụ, năng suất theo vụ như
// hệ biết lúc hết ngày D.
func (m *Module) MetricsSource() metricsport.Source { return app.MetricsSource{Svc: m.svc} }
