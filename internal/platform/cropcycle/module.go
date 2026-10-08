// Package cropcycle là module pf/cropcycle sau W3.3: van thu gom rơm MKG ↔ HTX (G3-2, pf 000040 — lịch gom,
// bảng kê, phản hồi của HTX, luồng phản hồi cho MKG trên bề mặt đối tác) và sản phẩm dữ liệu collection_volume
// (G5). Vụ mùa, nhật ký canh tác, SOP, sản lượng chuyển sang pf/cooperative (schema pf_coop,
// docs/design/cooperative.md) — route giữ nguyên đường dẫn /v1/cropcycle/*.
//
// Module khác chỉ được import cropcycle/port (R-02). Tệp này nối các lớp cho cmd/api-platform.
package cropcycle

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/handler"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Deps là port của module khác mà cropcycle cần.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của cán bộ KN (pf/party, ADR-0009) — đọc lịch gom.
	Scope partyport.ScopeResolver
	// Plots: mã thửa của bảng kê (pf/cooperative).
	Plots coopport.Plots
}

// Module là pf/cropcycle đã nối.
type Module struct{ svc *app.Service }

// New tạo module; mọi giao dịch chạy dưới r_cropcycle (db.ForModule).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	db = forModule(db)
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Plots: deps.Plots}}
}

// UseSharing nối cổng cấp quyền chia sẻ của pf/party (ADR-0020): luồng phản hồi HTX trên bề mặt đối tác
// /v1/partner/htx-collection/responses và định tuyến van collection_* về tenant bên cấp.
func (m *Module) UseSharing(pa partyport.PartnerAccess, vg partyport.ValveGrants) {
	m.svc.Sharing, m.svc.ValveGrants = pa, vg
}

// UseCollectionVolume (G5) nối nguồn của sản phẩm dữ liệu collection_volume: dữ kiện thửa và mã vụ
// (pf/cooperative Plots.Facts, Seasons.Codes/Progress) và đơn vị hành chính (pf/masterdata).
func (m *Module) UseCollectionVolume(pf app.PlotFacts, seasons app.SeasonFacts, units partyport.AdminUnits) {
	m.svc.PlotFacts, m.svc.Seasons, m.svc.Units = pf, seasons, units
}

// CollectionVolumeSource (G5, ADR-0022) là nguồn của sản phẩm dữ liệu collection_volume cho
// cmd/api-public (ô xã × vụ × tháng, ngưỡng k — domain.KAnonRule).
func (m *Module) CollectionVolumeSource() apiclientport.CollectionVolumeSource { return m.svc }

// AggregateSource (W5.3) là nguồn ô thô của tập tổng hợp harvest_progress_by_commune và
// collection_volume_by_commune cho job dựng feed của pf/apiclient (cmd/worker). Cần UseCollectionVolume.
func (m *Module) AggregateSource() apiclientport.AggregateSource { return m.svc }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering) cho consumer van collection_*
// (kênh valve). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (không còn route nào từ W3.3).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// CollectionValveHandler là inbox.Handler của consumer port.CollectionValveConsumer (van MKG
// collection_schedule / htx_collection_statement → sổ nhận + bảng chiếu, vai r_cropcycle).
func (m *Module) CollectionValveHandler() inbox.Handler { return m.svc.HandleCollectionValve }

// SyncSource là chiều xuống entity htx_collection_schedule, htx_collection_statement cho pf/sync.
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// SyncSink là chiều lên entity htx_collection_response cho pf/sync.
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }
