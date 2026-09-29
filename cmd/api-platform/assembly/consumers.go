package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
)

// Consumer là một consumer liên module của worker chạy trên Modules đã nối ở Build.
// Handler chạy trong giao dịch inbox trên pool của Role (vai DB của module nhận, R-05).
type Consumer struct {
	Name    string
	Role    string
	Events  []string
	Handler func(Modules) inbox.Handler
}

// Consumers là các consumer G2 đợt 2 cần port của nhiều module (liệt kê không cần DB):
//   - pf.mechanization.area_sync: diện tích vụ App HTX → CGH (CGH FN-07, QT-03);
//   - pf.geo.hub_valve: van công bố MKG hub_facility / hub_capacity → mô hình đọc hub;
//   - pf.cropcycle.gps_log, pf.extension.gps_log: log GPS sau commit (HTX US-GPS-03);
//   - pf.simulation.snapshot: ảnh chụp đầu vào qua SupplySource + WaterwayRouter.
var Consumers = []Consumer{
	{Name: mechport.AreaSyncConsumer, Role: mechanization.DBRole, Events: []string{ccport.EventCoopSeasonAreaChanged},
		Handler: func(m Modules) inbox.Handler { return m.Mechanization.AreaSyncHandler() }},
	{Name: geoport.HubValveConsumer, Role: geo.DBRole, Events: []string{geoport.EventMKGHubFacilityPublished, geoport.EventMKGHubCapacityPublished},
		Handler: func(m Modules) inbox.Handler { return m.Geo.HubValveHandler() }},
	{Name: ccport.GpsLogConsumer, Role: cropcycle.DBRole, Events: ccport.GpsLogEvents,
		Handler: func(m Modules) inbox.Handler { return m.CropCycle.GpsLogHandler() }},
	{Name: extport.GpsLogConsumer, Role: extension.DBRole, Events: []string{extport.EventSupportRequestSubmitted},
		Handler: func(m Modules) inbox.Handler { return m.Extension.GpsLogHandler() }},
	{Name: simport.SnapshotConsumer, Role: simulation.DBRole, Events: []string{simport.EventRunSubmitted},
		Handler: func(m Modules) inbox.Handler { return m.Simulation.SnapshotHandler() }},
}
