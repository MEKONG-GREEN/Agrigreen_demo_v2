package extension

// Nối hồ sơ KN riêng (BF-SRV-KN-ATTRS, #119): năm sản xuất theo loại máy của HTX (TT-10) và cây trồng của thửa
// (TT-11). HTX và thửa chỉ đọc qua publication (platformread, htxread — luôn chạy vai r_extension); không gọi module
// HTX trong tiến trình, không ghi dữ liệu HTX.

import (
	"github.com/gin-gonic/gin"

	kaapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/app"
	kahandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/platformread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// newKNAttrs dựng use case từ pool gốc (bộ đọc publication tự gắn vai r_extension) và pool đã gắn vai của module.
// Thửa đọc từ kho KN (knplot, #271) thay publication HTX pf_cooperative_pub.v1_plots.
func newKNAttrs(pool, moduleDB tenant.Beginner, clk clock.Clock, catalogs mdport.Catalogs, plots port.PlotDirectory) *kaapp.Service {
	return &kaapp.Service{DB: moduleDB, Clock: clk, Catalogs: catalogs, Orgs: platformread.NewOrganizations(pool), Plots: plots}
}

func (m *Module) registerKNAttrs(rg *gin.RouterGroup) {
	kahandler.Handler{Svc: m.attrs}.RegisterApp(rg)
}
