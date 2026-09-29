package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

type stageDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type stagePlotDTO struct {
	PlotRef             string  `json:"plot_ref"`
	PlotCode            string  `json:"plot_code"`
	SeasonRef           string  `json:"season_ref"`
	CooperativeRef      string  `json:"cooperative_ref"`
	CommuneCode         string  `json:"commune_code"`
	VarietyCode         string  `json:"variety_code"`
	SeasonStatus        string  `json:"season_status"`
	Stage               string  `json:"stage"`
	SowingDate          string  `json:"sowing_date"`
	HarvestOpenDate     string  `json:"harvest_open_date"`
	ExpectedHarvestDate string  `json:"expected_harvest_date"`
	DaysToHarvest       int     `json:"days_to_harvest"`
	Ripening            bool    `json:"ripening"`
	AreaHa              string  `json:"area_ha"`
	ExpectedTons        *string `json:"expected_tons"`
}

type stageCoopDTO struct {
	CooperativeRef       string            `json:"cooperative_ref"`
	CommuneCode          string            `json:"commune_code"`
	DominantStage        string            `json:"dominant_stage"`
	StageAreaHa          map[string]string `json:"stage_area_ha"`
	PlotCount            int               `json:"plot_count"`
	RipeningPlotCount    int               `json:"ripening_plot_count"`
	RipeningAreaHa       string            `json:"ripening_area_ha"`
	RipeningExpectedTons *string           `json:"ripening_expected_tons"`
	EarliestRipeningDate *string           `json:"earliest_ripening_date"`
}

// stageMap: GET /v1/cropcycle/stage-map — GIS US-SEASON-01 (FN-13), US-YIELD-01 (FN-14).
func (h Handler) stageMap(c *gin.Context) {
	f := newFields()
	date := c.Query("date")
	in := app.StageMapInput{Date: f.day("date", &date), CooperativeRef: qRef(c, f, "cooperative_ref"), CommuneCode: c.Query("commune_code"),
		Limit: qInt(c, f, "limit")}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	m, err := h.Svc.StageMap(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	stages := make([]stageDTO, 0, len(domain.Stages))
	for _, s := range domain.Stages {
		stages = append(stages, stageDTO{Code: s, Label: domain.StageLabel(s)})
	}
	plots := make([]stagePlotDTO, 0, len(m.Plots))
	for _, p := range m.Plots {
		plots = append(plots, stagePlotDTO{PlotRef: p.PlotRef.String(), PlotCode: p.PlotCode, SeasonRef: p.SeasonRef.String(),
			CooperativeRef: p.CooperativeRef.String(), CommuneCode: p.CommuneCode, VarietyCode: p.VarietyCode, SeasonStatus: p.Status,
			Stage: p.Stage, SowingDate: domain.FormatDay(p.SowingDate), HarvestOpenDate: domain.FormatDay(p.HarvestOpenDate),
			ExpectedHarvestDate: domain.FormatDay(p.ExpectedHarvestDate), DaysToHarvest: p.DaysToHarvest, Ripening: p.Ripening,
			AreaHa: p.AreaHa.StringFixed(4), ExpectedTons: dec(p.ExpectedTons, 3)})
	}
	coops := make([]stageCoopDTO, 0, len(m.Cooperatives))
	for _, x := range m.Cooperatives {
		area := make(map[string]string, len(x.StageAreaHa))
		for k, v := range x.StageAreaHa {
			area[k] = v.StringFixed(4)
		}
		var earliest *string
		if x.EarliestRipening != nil {
			s := domain.FormatDay(*x.EarliestRipening)
			earliest = &s
		}
		coops = append(coops, stageCoopDTO{CooperativeRef: x.CooperativeRef.String(), CommuneCode: x.CommuneCode, DominantStage: x.DominantStage,
			StageAreaHa: area, PlotCount: x.PlotCount, RipeningPlotCount: x.RipeningPlotCount, RipeningAreaHa: x.RipeningAreaHa.StringFixed(4),
			RipeningExpectedTons: dec(x.RipeningExpectedTons, 3), EarliestRipeningDate: earliest})
	}
	c.JSON(http.StatusOK, gin.H{
		"date":          domain.FormatDay(m.Date),
		"stages":        stages,
		"ripening_days": gin.H{"from": domain.RipeningFromDays, "to": domain.RipeningToDays},
		"plots":         plots,
		"cooperatives":  coops,
		"truncated":     m.Truncated,
		"limit":         app.StageMapLimit,
	})
}
