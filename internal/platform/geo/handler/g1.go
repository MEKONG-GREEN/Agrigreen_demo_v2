package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerG1 gắn route đọc bổ sung: ranh giới hành chính theo cấp (G-05) và chi
// tiết xung đột ranh giới giữ cả hai phiên bản (US-SYNC-03).
func (h Handler) registerG1(rg *gin.RouterGroup) {
	rg.GET("/geo/admin-boundaries", h.adminBoundaries)
	rg.GET("/geo/conflicts/:id", h.conflict)
}

func (h Handler) adminBoundaries(c *gin.Context) {
	zoom, err := intQuery(c, "zoom", 12)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out, err := h.Svc.ListAdminBoundaries(c.Request.Context(), c.Query("level"), c.Query("parent"), zoom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(out.Items))
	for _, b := range out.Items {
		feats = append(feats, gin.H{
			"type": "Feature", "id": b.UnitCode, "geometry": json.RawMessage(b.GeoJSON),
			"properties": gin.H{"code": b.UnitCode, "level": b.Level, "name": b.Name, "parent_code": strOrNil(b.ParentCode),
				"source": b.Source, "area_ha": b.AreaHa.StringFixed(4), "imported_at": ts(b.ImportedAt)},
		})
	}
	c.Header("Content-Type", "application/geo+json")
	c.JSON(http.StatusOK, gin.H{"type": "FeatureCollection", "features": feats, "missing": out.Missing})
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
