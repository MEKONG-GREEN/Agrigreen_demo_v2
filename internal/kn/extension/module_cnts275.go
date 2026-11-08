package extension

import (
	rchandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/handler"
	"github.com/gin-gonic/gin"
)

// Middleware is assembled after current account session resolution and before
// idempotency cache replay. Only the FN-07 write routes are handled here.
func (m *Module) Middleware() gin.HandlerFunc {
	return rchandler.Handler{Svc: m.rc}.CNTSWriteAuthorization()
}
