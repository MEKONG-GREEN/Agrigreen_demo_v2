package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerG1 gắn route chi tiết xung đột ranh giới giữ cả hai phiên bản (US-SYNC-03).
func (h Handler) registerG1(rg *gin.RouterGroup) {
	rg.GET("/geo/conflicts/:id", h.conflict)
}

func (h Handler) conflict(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrConflictNotFound)
		return
	}
	d, err := h.Svc.GetConflict(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(d.Versions))
	for _, v := range d.Versions {
		b := boundaryDTO(v.Boundary)
		b["role"], b["plot_code"], b["current"] = v.Role, v.Plot.Code, v.Current
		versions = append(versions, b)
	}
	out := conflictDTO(d.Conflict)
	out["versions"] = versions
	c.JSON(http.StatusOK, out)
}
