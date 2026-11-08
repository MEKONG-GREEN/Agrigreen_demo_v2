package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h Handler) saveOperation(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	operation, ok := path(c, "operationID")
	if !ok {
		return
	}
	result, err := h.Svc.ReadSaveOperation(c.Request.Context(), id, operation)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
func (h Handler) saveAudit(c *gin.Context) {
	id, ok := path(c, "id")
	if !ok {
		return
	}
	items, err := h.Svc.SaveAudit(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}
