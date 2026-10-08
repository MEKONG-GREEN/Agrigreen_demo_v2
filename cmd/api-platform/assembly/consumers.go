package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
)

// Consumer là một consumer liên module của worker chạy trên Modules đã nối ở Build.
// Handler chạy trong giao dịch inbox dưới vai nhóm của Module (module nhận; SET LOCAL ROLE r_<module>, W2.6).
type Consumer struct {
	Name    string
	Module  db.ModuleID
	Events  []string
	Handler func(Modules) inbox.Handler
}

// Consumers là các consumer G2 đợt 2 cần port của nhiều module (liệt kê không cần DB):
//   - pf.mechanization.area_sync: diện tích vụ App HTX → CGH (CGH FN-07, QT-03);
//   - pf.geo.hub_valve: van công bố MKG hub_facility / hub_capacity → mô hình đọc hub;
//   - pf.cropcycle.htx_collection_valve (G3-2): van MKG collection_schedule / htx_collection_statement
//     → sổ nhận + bảng chiếu cho quản lý HTX (kiểm hợp đồng van lúc chạy);
//   - pf.cropcycle.gps_log, pf.extension.gps_log: log GPS sau commit (HTX US-GPS-03);
//   - pf.simulation.snapshot: ảnh chụp đầu vào qua SupplySource + WaterwayRouter;
//   - pf.mrv.farm_log_link, pf.mrv.collection_evidence (G3): liên kết nhật ký HTX / van
//     mrv_collection_evidence → pf/mrv, ước tính CO2 TẠM đọc tham số masterdata.
//   - pf.metrics.layer_log (G3): nhật ký thay đổi lớp GIS từ sự kiện geo/cropcycle/mechanization/waterway (GIS US-EVL-01).
//   - pf.metering.statement (G5): bảng kê kỳ tháng của pf/metering (ADR-0023).
var Consumers = []Consumer{
	{Name: mechport.AreaSyncConsumer, Module: mechanization.DBModule, Events: []string{coopport.EventCoopSeasonAreaChanged},
		Handler: func(m Modules) inbox.Handler { return m.Mechanization.AreaSyncHandler() }},
	{Name: geoport.HubValveConsumer, Module: geo.DBModule, Events: []string{geoport.EventMKGHubFacilityPublished, geoport.EventMKGHubCapacityPublished},
		Handler: func(m Modules) inbox.Handler { return m.Geo.HubValveHandler() }},
	{Name: ccport.CollectionValveConsumer, Module: cropcycle.DBModule, Events: ccport.CollectionValveEvents,
		Handler: func(m Modules) inbox.Handler { return m.CropCycle.CollectionValveHandler() }},
	{Name: coopport.GpsLogConsumer, Module: cooperative.DBModule, Events: coopport.GpsLogEvents,
		Handler: func(m Modules) inbox.Handler { return m.Cooperative.GpsLogHandler() }},
	{Name: extport.GpsLogConsumer, Module: extension.DBModule, Events: []string{extport.EventSupportRequestSubmitted},
		Handler: func(m Modules) inbox.Handler { return m.Extension.GpsLogHandler() }},
	{Name: simport.SnapshotConsumer, Module: simulation.DBModule, Events: []string{simport.EventRunSubmitted},
		Handler: func(m Modules) inbox.Handler { return m.Simulation.SnapshotHandler() }},
	{Name: mrvport.FarmLogLinkConsumer, Module: mrv.DBModule, Events: mrvport.FarmLogLinkEvents,
		Handler: func(m Modules) inbox.Handler { return m.MRV.FarmLogLinkHandler() }},
	{Name: mrvport.CollectionEvidenceConsumer, Module: mrv.DBModule, Events: mrvport.CollectionEvidenceEvents,
		Handler: func(m Modules) inbox.Handler { return m.MRV.CollectionEvidenceHandler() }},
	{Name: metricsport.LayerLogConsumer, Module: metrics.DBModule, Events: metrics.LayerEvents,
		Handler: func(m Modules) inbox.Handler { return m.Metrics.LayerLogHandler() }},
	// G5 (ADR-0023 mục 4): bảng kê kỳ tháng — ảnh chụp bất biến + bảng kê draft (mẫu §12).
	{Name: meteringport.StatementConsumer, Module: metering.DBModule, Events: []string{meteringport.EventStatementRequested},
		Handler: func(m Modules) inbox.Handler { return m.Metering.StatementHandler() }},
	// G5 (ADR-0022 mục 8): việc xuất Public API — dựng tệp tất định qua nguồn của module chủ dữ liệu.
	{Name: apiclientport.ExportConsumer, Module: apiclient.DBModule, Events: []string{apiclientport.EventExportRequested},
		Handler: func(m Modules) inbox.Handler { return m.APIClient.ExportHandler() }},
}
