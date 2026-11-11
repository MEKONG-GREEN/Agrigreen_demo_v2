package extension

// Nối máy móc HTX của KN (Trồng trọt FN-XA-03.04) — KN-STANDALONE-5-TT-DATA-MAY (#306, quyết định D1): route
// /v1/kn/machine-counts* do chủ KN phục vụ, dữ liệu ở pf_extension, HTX là hồ sơ KN loại HTX. KN không gọi module CGH
// (pf/mechanization) hay HTX nền tảng nên chạy được khi tenant tắt các module đó.

import (
	"github.com/gin-gonic/gin"

	kmhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/handler"
)

func (m *Module) registerKNMachines(rg *gin.RouterGroup) {
	kmhandler.Handler{Svc: m.machines}.RegisterApp(rg)
}
