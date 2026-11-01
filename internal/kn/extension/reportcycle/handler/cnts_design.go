package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h Handler) registerCNTSDesign(g *gin.RouterGroup) {
	g.GET("/cnts/form-designs", h.cntsDesign)
	httpx.AllowBody(g, http.MethodPost, "/cnts/form-designs/import-preview", app.MaxDesignImportBytes+1024)
	g.POST("/cnts/form-designs/import-preview", h.cntsDesignImport)
}
func (h Handler) cntsDesign(c *gin.Context) {
	items, err := h.Svc.CNTSDesign(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h Handler) cntsDesignImport(c *gin.Context) {
	result, err := h.Svc.PreviewCNTSDesignImport(c.Request.Context(), c.Request.Body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
