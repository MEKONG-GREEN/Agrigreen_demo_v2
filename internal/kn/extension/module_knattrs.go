package extension

// Nối hồ sơ KN riêng (BF-SRV-KN-ATTRS, #119): năm sản xuất theo loại máy của HTX (TT-10) và cây trồng của thửa
// (TT-11). HTX là hồ sơ KN loại HTX (knmachine, #306), thửa ở kho KN (knplot, #271); không gọi module HTX/CGH trong tiến
// trình, không đọc HTX nền tảng, không ghi dữ liệu HTX.

import (
	"github.com/gin-gonic/gin"

	kaapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/app"
	kahandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// newKNAttrs dựng use case trên pool đã gắn vai của module. HTX đọc từ hồ sơ KN (knmachine, #306), thửa từ kho KN
// (knplot, #271) — không còn publication HTX.
func newKNAttrs(moduleDB tenant.Beginner, clk clock.Clock, catalogs mdport.Catalogs, coops port.CooperativeDirectory, plots port.PlotDirectory) *kaapp.Service {
	return &kaapp.Service{DB: moduleDB, Clock: clk, Catalogs: catalogs, Coops: coops, Plots: plots}
}

func (m *Module) registerKNAttrs(rg *gin.RouterGroup) {
	kahandler.Handler{Svc: m.attrs}.RegisterApp(rg)
}
