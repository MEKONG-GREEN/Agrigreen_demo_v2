package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

type displayValueDTO struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Default string `json:"default"`
	Version int    `json:"version"`
	Source  string `json:"source"` // approved | default
}

func toDisplayValue(v app.DisplayValue) displayValueDTO {
	src := "default"
	if v.FromDB {
		src = "approved"
	}
	return displayValueDTO{Key: v.Key, Value: v.Value, Default: v.Default, Version: v.Version, Source: src}
}

// displayConfig: GET /v1/masterdata/display-config — quy chuẩn hiển thị GIS (GIS US-CFG-01/02).
func (h Handler) displayConfig(c *gin.Context) {
	cfg, err := h.Svc.DisplayConfig(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	stage := map[string]displayValueDTO{}
	for k, v := range cfg.StageColors {
		stage[k] = toDisplayValue(v)
	}
	capa := map[string]displayValueDTO{}
	for k, v := range cfg.CapacityColors {
		capa[k] = toDisplayValue(v)
	}
	c.JSON(http.StatusOK, gin.H{
		"as_of": cfg.AsOf.UTC().Format(time.RFC3339), "stage_colors": stage, "capacity_colors": capa,
		"ripening_outline_color": toDisplayValue(cfg.RipeningOutline),
		"hub_capacity_warn_pct":  toDisplayValue(cfg.HubWarnPct), "hub_capacity_full_pct": toDisplayValue(cfg.HubFullPct),
		"plot_layer_min_zoom": toDisplayValue(cfg.PlotLayerMinZoom),
	})
}

type restoreDefaultReq struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// restoreDefault: POST /v1/masterdata/params/restore-default — tạo phiên bản nháp mang giá trị
// mặc định (GIS US-CFG-02 AC-2); áp dụng khi được duyệt (R-56).
func (h Handler) restoreDefault(c *gin.Context) {
	var req restoreDefaultReq
	if !bindJSON(c, &req) {
		return
	}
	out, replayed, err := h.Svc.RestoreDefault(c.Request.Context(), app.RestoreDefaultInput{
		Key: req.Key, Reason: req.Reason, IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/masterdata/params/versions/"+out.ID.String())
	c.JSON(status, toVersionDTO(out))
}
