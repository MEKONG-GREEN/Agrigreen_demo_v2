package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerForecastApp gắn route đọc dự báo nhu cầu (FN-09) trên Application API.
func (h Handler) registerForecastApp(g *gin.RouterGroup) {
	g.GET("/forecasts", h.listForecasts)
	g.GET("/forecasts/:id", h.getForecast)
}

// registerForecastAdmin gắn route chạy dự báo (FN-09) trên Admin API.
func (h Handler) registerForecastAdmin(g *gin.RouterGroup) {
	g.POST("/forecasts", h.runForecast)
}

type forecastRunDTO struct {
	ID            string                `json:"id"`
	SeasonCode    string                `json:"season_code"`
	SeasonYear    int                   `json:"season_year"`
	SeasonKey     string                `json:"season_key"`
	ReferenceDate string                `json:"reference_date"`
	RuleVersion   string                `json:"rule_version"`
	Provisional   bool                  `json:"method_provisional"`
	Method        domain.ForecastMethod `json:"method"`
	PilotStages   []string              `json:"pilot_stages"`
	ResultCount   int                   `json:"result_count"`
	MissingNorms  []string              `json:"missing_norms"`
	ComputedAt    string                `json:"computed_at"`
	Seq           int64                 `json:"seq"`
}

func toForecastRunDTO(r domain.ForecastRun) forecastRunDTO {
	d := forecastRunDTO{
		ID: r.ID.String(), SeasonCode: r.Season.Code, SeasonYear: r.Season.Year, SeasonKey: r.Season.Key(),
		ReferenceDate: dateStr(r.ReferenceDate), RuleVersion: r.RuleVersion, Provisional: true, Method: r.Method,
		PilotStages: r.PilotStages, ResultCount: r.ResultCount, MissingNorms: r.MissingNorms, ComputedAt: ts(r.EventTime), Seq: r.Seq,
	}
	if d.PilotStages == nil {
		d.PilotStages = []string{}
	}
	if d.MissingNorms == nil {
		d.MissingNorms = []string{}
	}
	return d
}

type forecastResultDTO struct {
	CooperativeRef string                `json:"cooperative_ref"`
	CommuneCode    *string               `json:"commune_code"`
	ProvinceCode   *string               `json:"province_code"`
	FarmStage      string                `json:"farm_stage"`
	MachineType    string                `json:"machine_type"`
	BaseAreaHa     *decimal.Decimal      `json:"base_area_ha"`
	GrowthPct      *decimal.Decimal      `json:"growth_pct"`
	ForecastAreaHa *decimal.Decimal      `json:"forecast_area_ha"`
	NormKey        string                `json:"norm_key"`
	Norm           *paramDTO             `json:"norm"`
	Required       *decimal.Decimal      `json:"required"`
	RequiredWhole  *int                  `json:"required_whole"`
	CapacityNow    int                   `json:"capacity_now"`
	Gap            *int                  `json:"gap"`
	Status         string                `json:"status"`
	Inputs         domain.ForecastInputs `json:"inputs"`
	Seq            int64                 `json:"seq"`
}

func toForecastResultDTO(r domain.ForecastResult) forecastResultDTO {
	return forecastResultDTO{
		CooperativeRef: r.CooperativeRef.String(), CommuneCode: strPtr(r.CommuneCode), ProvinceCode: strPtr(r.ProvinceCode),
		FarmStage: r.FarmStage, MachineType: r.MachineType, BaseAreaHa: r.BaseAreaHa, GrowthPct: r.GrowthPct,
		ForecastAreaHa: r.ForecastAreaHa, NormKey: r.NormKey, Norm: toParamDTO(r.Norm), Required: r.Required,
		RequiredWhole: r.RequiredWhole, CapacityNow: r.CapacityNow, Gap: r.Gap, Status: r.Status, Inputs: r.Inputs, Seq: r.Seq,
	}
}

func (h Handler) runForecast(c *gin.Context) {
	var req balanceReq
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.Svc.RunForecast(c.Request.Context(), app.ForecastRequest{
		Season: domain.Season{Code: req.SeasonCode, Year: req.SeasonYear}, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1"+base+"/forecasts/"+out.Run.ID.String())
	c.JSON(created(out.Replayed), gin.H{"run": toForecastRunDTO(out.Run)})
}

func (h Handler) listForecasts(c *gin.Context) {
	var fe fieldErrs
	s := fe.season(c, false)
	if fe.fail(c) {
		return
	}
	runs, err := h.Svc.ListForecasts(c.Request.Context(), s.Code, s.Year)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]forecastRunDTO, 0, len(runs))
	for _, r := range runs {
		items = append(items, toForecastRunDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getForecast(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrForecastNotFound)
	if !ok {
		return
	}
	var fe fieldErrs
	f := store.ResultFilter{ProvinceCode: c.Query("province_code"), CommuneCode: c.Query("commune_code"),
		CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref")), FarmStage: c.Query("farm_stage"), MachineType: c.Query("machine_type")}
	if fe.fail(c) {
		return
	}
	v, err := h.Svc.GetForecast(c.Request.Context(), id, f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res := make([]forecastResultDTO, 0, len(v.Results))
	for _, r := range v.Results {
		res = append(res, toForecastResultDTO(r))
	}
	stages := map[string]string{}
	for k, d := range v.RequiredByStage {
		stages[k] = d.StringFixed(4)
	}
	c.JSON(http.StatusOK, gin.H{
		"run": toForecastRunDTO(v.Run), "results": res,
		"summary": gin.H{"forecast_area_ha": v.TotalForecastAreaHa.StringFixed(4), "required_by_stage": stages},
	})
}
