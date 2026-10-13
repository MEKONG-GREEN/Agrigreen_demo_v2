package handler

// W11.44: tổng hợp CNTS Cấp Tỉnh — BM-02 KSGM tuần/tháng + dòng điều chỉnh, BM-18 Phụ lục 2 + nuôi trồng, chốt/mở lại
// bảng tổng hợp (số công bố cho KHTC), phiên bản tệp chia sẻ Chi cục (Excel).

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func (h Handler) registerCNTSAgg(g *gin.RouterGroup) {
	g.GET("/cnts/bm02", h.cntsBM02)
	g.POST("/cnts/bm02/adjustments", h.cntsAdjust)
	g.GET("/cnts/bm18", h.cntsBM18)
	g.POST("/cnts/bm18/aquaculture", h.cntsAqua)
	g.POST("/cnts/aggregates/close", h.cntsAggClose)
	g.POST("/cnts/aggregates/reopen", h.cntsAggReopen)
	g.GET("/cnts/shares", h.cntsShares)
	g.POST("/cnts/shares", h.cntsShare)
	g.GET("/cnts/shares/:id/xlsx", h.cntsShareFile)
}

func (h Handler) cntsBM02(c *gin.Context) {
	v, err := h.Svc.BM02(c.Request.Context(), c.Query("province"), c.Query("period_type"), c.Query("key"), c.Query("basis"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type cntsAdjustReq struct {
	WeekKey     string            `json:"week_key"`
	FacilityRef string            `json:"facility_ref"`
	Deltas      map[string]string `json:"deltas"`
	Reason      string            `json:"reason"`
}

func (h Handler) cntsAdjust(c *gin.Context) {
	var req cntsAdjustReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.AddAdjustment(c.Request.Context(), app.AdjustmentInput{WeekKey: req.WeekKey, FacilityRef: req.FacilityRef,
		Deltas: req.Deltas, Reason: req.Reason}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h Handler) cntsBM18(c *gin.Context) {
	v, err := h.Svc.BM18(c.Request.Context(), c.Query("province"), c.Query("week"), c.Query("basis"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type cntsAquaReq struct {
	Week    string `json:"week"`
	Entries []struct {
		Object string  `json:"object"`
		Area   *string `json:"area"`
		Output *string `json:"output"`
		Note   string  `json:"note"`
	} `json:"entries"`
}

func (h Handler) cntsAqua(c *gin.Context) {
	var req cntsAquaReq
	if !bind(c, &req) {
		return
	}
	in := make([]app.AquaInput, 0, len(req.Entries))
	for _, e := range req.Entries {
		in = append(in, app.AquaInput{Object: e.Object, Area: e.Area, Output: e.Output, Note: e.Note})
	}
	v, err := h.Svc.SaveAqua(c.Request.Context(), req.Week, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type cntsAggReq struct {
	Form      string `json:"form"`
	PeriodKey string `json:"period_key"`
	Reason    string `json:"reason"`
}

func (h Handler) cntsAggClose(c *gin.Context) {
	var req cntsAggReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.CloseAggregate(c.Request.Context(), req.Form, req.PeriodKey, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h Handler) cntsAggReopen(c *gin.Context) {
	var req cntsAggReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.ReopenAggregate(c.Request.Context(), req.Form, req.PeriodKey, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h Handler) cntsShares(c *gin.Context) {
	v, err := h.Svc.Shares(c.Request.Context(), c.Query("province"), c.Query("form"), c.Query("period_type"), c.Query("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": v})
}

type cntsShareReq struct {
	Form       string `json:"form"`
	PeriodType string `json:"period_type"`
	PeriodKey  string `json:"period_key"`
}

func (h Handler) cntsShare(c *gin.Context) {
	var req cntsShareReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.CreateShare(c.Request.Context(), req.Form, req.PeriodType, req.PeriodKey, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if r.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"share": r.Share, "replayed": r.Replayed})
}

func (h Handler) cntsShareFile(c *gin.Context) {
	id, ok := pathID(c, app.ErrShareNotFound)
	if !ok {
		return
	}
	ex, err := h.Svc.ShareFile(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+ex.FileName+"\"; filename*=UTF-8''"+url.PathEscape(ex.FileName))
	c.Data(http.StatusOK, ex.ContentType, ex.Body)
}
