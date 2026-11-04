package extension

import (
	khtchandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/handler"
	"github.com/gin-gonic/gin"
)

// AfterContextMiddleware runs after accounts has resolved current owner state,
// before shared HTTP idempotency can replay any unpublished draft data.
func (m *Module) AfterContextMiddleware() gin.HandlerFunc {
	return khtchandler.InitialDraftPolicy225(m.khtc)
}
