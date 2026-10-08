package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerG1 gắn route đọc bổ sung: ranh giới hành chính theo cấp (G-05). Chi tiết xung đột ranh giới
// (US-SYNC-03) do pf/cooperative phục vụ (W3.3).
func (h Handler) registerG1(rg *gin.RouterGroup) {
	rg.GET("/geo/admin-boundaries", h.adminBoundaries)
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
