package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h Handler) openPeriodCount(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	n, err := h.Svc.OpenPeriodCount(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"open_count": n})
}
