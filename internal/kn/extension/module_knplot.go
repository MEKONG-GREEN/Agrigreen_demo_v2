package extension

// Nối thửa ruộng KN (Trồng trọt FN-XA-03.05) — KN-STANDALONE-5-TT-DATA (#271, quyết định D1): route /v1/kn/plots*,
// đồng bộ ngoại tuyến kn_plot / kn_plot_boundary, nguồn thửa cho hồ sơ cây trồng (knattrs). Dữ liệu ở pf_extension; KN
// không gọi module HTX (pf/cooperative) nên chạy được khi tenant tắt module đó.

import (
	"github.com/gin-gonic/gin"

	kpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/app"
	kphandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/handler"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func (m *Module) newKNPlots(moduleDB tenant.Beginner, clk clock.Clock, p params.Params) *kpapp.Service {
	return &kpapp.Service{DB: moduleDB, Clock: clk, Params: p, Names: func() partyport.ActorNames { return m.rc.Names }}
}

// UseKNPlotVault nối kho định danh cho tên nông dân của thửa (mục đích extension.contact). Chưa nối → vẽ lô trả 503.
func (m *Module) UseKNPlotVault(v client.Client) { m.plots.Vault = v }

// UseKNPlotGeometry nối phép tính địa giới xã của pf/geo (thửa phải nằm trong xã). Chưa nối → bỏ kiểm.
func (m *Module) UseKNPlotGeometry(g geoport.AdminGeometry) { m.plots.Geometry = g }

// KNPlotSyncSource cho pf/sync: chiều xuống "kn_plot".
func (m *Module) KNPlotSyncSource() syncport.Source { return kpapp.SyncSource{Svc: m.plots} }

// KNPlotSyncSink cho pf/sync: chiều lên "kn_plot_boundary".
func (m *Module) KNPlotSyncSink() syncport.Sink { return kpapp.SyncSink{Svc: m.plots} }

func (m *Module) registerKNPlots(rg *gin.RouterGroup) {
	kphandler.Handler{Svc: m.plots}.RegisterApp(rg)
}
