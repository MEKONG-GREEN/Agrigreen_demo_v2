package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerUnified gắn GET /v1/dashboard: bảng điều hành gộp nhiều module (G-02).
func (h Handler) registerUnified(rg *gin.RouterGroup) {
	rg.GET("/dashboard", h.unifiedDashboard)
}

type unifiedDTO struct {
	SownAreaHa       string   `json:"sown_area_ha"`
	TargetAreaHa     *string  `json:"target_area_ha"`
	Cooperatives     int64    `json:"cooperatives"`
	Households       int64    `json:"households"`
	CoopsReporting   int64    `json:"cooperatives_reporting"`
	DigitizedPlots   int64    `json:"digitized_plots"`
	DigitizedAreaHa  string   `json:"digitized_area_ha"`
	OpenTasks        int64    `json:"open_tasks"`
	OverdueTasks     int64    `json:"overdue_tasks"`
	ExpectedCommunes int64    `json:"expected_communes"`
	ReportedCommunes int64    `json:"reported_communes"`
	MissingCommunes  []string `json:"missing_communes"`
	Provisional      bool     `json:"provisional"`
	ProvisionalLabel *string  `json:"provisional_label"`
}

func toUnified(f app.UnifiedFigures) unifiedDTO {
	d := unifiedDTO{
		SownAreaHa: f.SownAreaHa.StringFixed(4), TargetAreaHa: nil, Cooperatives: f.Cooperatives, Households: f.Households,
		CoopsReporting: f.CoopsReporting, DigitizedPlots: f.DigitizedPlots, DigitizedAreaHa: f.DigitizedAreaHa.StringFixed(4),
		OpenTasks: f.OpenTasks, OverdueTasks: f.OverdueTasks, ExpectedCommunes: f.ExpectedCommunes, ReportedCommunes: f.ReportedCommunes,
		MissingCommunes: nonNil(f.MissingCommunes), Provisional: f.Provisional,
	}
	if f.TargetAreaHa != nil {
		s := f.TargetAreaHa.StringFixed(4)
		d.TargetAreaHa = &s
	}
	if f.Provisional {
		l := "Số liệu tạm tính"
		d.ProvisionalLabel = &l
	}
	return d
}

type unifiedChildDTO struct {
	Unit string  `json:"unit"`
	Name *string `json:"name"`
	unifiedDTO
}

func (h Handler) unifiedDashboard(c *gin.Context) {
	d, err := h.Svc.GetUnifiedDashboard(c.Request.Context(), c.Query("unit"), c.Query("period"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	children := make([]unifiedChildDTO, 0, len(d.Children))
	for _, ch := range d.Children {
		children = append(children, unifiedChildDTO{Unit: ch.Unit, Name: opt(ch.Name), unifiedDTO: toUnified(ch.UnifiedFigures)})
	}
	var unit any
	if d.Unit != "" {
		unit = d.Unit
	}
	c.JSON(http.StatusOK, gin.H{
		"unit": unit, "level": d.Level, "period": d.Period, "as_of": ts(d.AsOf), "total": toUnified(d.Total),
		"children": children, "sources": d.Sources,
	})
}
