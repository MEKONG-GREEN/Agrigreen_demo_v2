package price

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

// Middleware checks current weekly-program permissions before shared idempotency can replay a write.
func (m *Module) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && strings.HasPrefix(c.FullPath(), "/v1/price/weekly-programs/") {
			if err := m.weekly.AuthorizeWeekly(c.Request.Context(), c.Query("profile")); err != nil {
				httpx.Fail(c, err)
				return
			}
		}
		c.Next()
	}
}
