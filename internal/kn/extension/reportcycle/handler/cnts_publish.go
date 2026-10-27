package handler

// BF-CN-PUBLISH (#108, CN-07): phần KN của màn theo dõi biểu kỳ CNTS (người phụ trách, giờ chốt, dấu nháp) và nháp
// của cán bộ xã (UR-XA-13 "Lưu nháp").

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerCNTSPublish(g *gin.RouterGroup) {
	g.GET("/cnts/period-tracking", h.cntsTracking)
	g.GET("/cnts/period-drafts/:id", h.cntsDraft)
	g.PUT("/cnts/period-drafts/:id", h.cntsSaveDraft)
}

type cntsTrackedPeriodDTO struct {
	PeriodID        string   `json:"period_id"`
	PublicationID   string   `json:"publication_id"`
	TemplateCode    string   `json:"template_code"`
	Target          string   `json:"target"`
	Regions         []string `json:"regions"`
	OwnerRef        *string  `json:"owner_ref"`
	OwnerName       *string  `json:"owner_name"`
	CloseAfterHours *int     `json:"close_after_hours"`
}

type cntsDraftMarkDTO struct {
	PeriodID string `json:"period_id"`
	Unit     string `json:"unit"`
	SavedAt  string `json:"saved_at"`
}

func (h Handler) cntsTracking(c *gin.Context) {
	var extra []uuid.UUID
	for _, s := range strings.Split(c.Query("period_ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("period_ids", "danh sách UUID cách nhau dấu phẩy"))
			return
		}
		extra = append(extra, id)
	}
	t, err := h.Svc.CNTSTracking(c.Request.Context(), strings.TrimSpace(c.Query("code")), extra)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := struct {
		Periods []cntsTrackedPeriodDTO `json:"periods"`
		Drafts  []cntsDraftMarkDTO     `json:"drafts"`
	}{Periods: []cntsTrackedPeriodDTO{}, Drafts: []cntsDraftMarkDTO{}}
	for _, p := range t.Periods {
		d := cntsTrackedPeriodDTO{PeriodID: p.PeriodRef.String(), PublicationID: p.PublicationID.String(), TemplateCode: p.TemplateCode, Target: p.Target,
			Regions: p.Regions, CloseAfterHours: p.CloseAfterHours}
		if d.Regions == nil {
			d.Regions = []string{}
		}
		if p.OwnerRef != nil {
			o := p.OwnerRef.String()
			d.OwnerRef = &o
			if p.OwnerName != "" {
				n := p.OwnerName
				d.OwnerName = &n
			}
		}
		out.Periods = append(out.Periods, d)
	}
	for _, d := range t.Drafts {
		out.Drafts = append(out.Drafts, cntsDraftMarkDTO{PeriodID: d.PeriodRef.String(), Unit: d.Unit, SavedAt: d.SavedAt.UTC().Format(time.RFC3339)})
	}
	c.JSON(http.StatusOK, out)
}

type cntsDraftDTO struct {
	PeriodID  string            `json:"period_id"`
	Unit      string            `json:"unit"`
	Values    map[string]string `json:"values"`
	NilReport bool              `json:"nil_report"`
	Note      string            `json:"note"`
	SavedAt   string            `json:"saved_at"`
}

func cntsDraftOf(d store.CNTSDraft) cntsDraftDTO {
	v := d.Values
	if v == nil {
		v = map[string]string{}
	}
	return cntsDraftDTO{PeriodID: d.PeriodRef.String(), Unit: d.Unit, Values: v, NilReport: d.NilReport, Note: d.Note,
		SavedAt: d.SavedAt.UTC().Format(time.RFC3339)}
}

func (h Handler) cntsDraft(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	unit := strings.TrimSpace(c.Query("unit"))
	if unit == "" {
		httpx.Fail(c, problem.Validation.WithField("unit", "bắt buộc"))
		return
	}
	d, err := h.Svc.CNTSDraft(c.Request.Context(), id, unit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := struct {
		Draft *cntsDraftDTO `json:"draft"`
	}{}
	if d != nil {
		x := cntsDraftOf(*d)
		out.Draft = &x
	}
	c.JSON(http.StatusOK, out)
}

type cntsDraftReq struct {
	Unit      string            `json:"unit"`
	Values    map[string]string `json:"values"`
	NilReport bool              `json:"nil_report"`
	Note      string            `json:"note"`
}

func (h Handler) cntsSaveDraft(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	var req cntsDraftReq
	if !bind(c, &req) {
		return
	}
	if strings.TrimSpace(req.Unit) == "" {
		httpx.Fail(c, problem.Validation.WithField("unit", "bắt buộc"))
		return
	}
	d, err := h.Svc.SaveCNTSDraft(c.Request.Context(), id, strings.TrimSpace(req.Unit), app.CNTSDraftInput{Values: req.Values, NilReport: req.NilReport, Note: req.Note})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, cntsDraftOf(d))
}
