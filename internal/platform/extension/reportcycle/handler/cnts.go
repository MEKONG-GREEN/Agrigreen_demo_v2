package handler

// W11.42: biểu kỳ Cấp Xã CNTS (BM-01/04/05/17) — danh sách biểu, cài biểu vào cap/forms, xem trước và phát hành lô kỳ
// native. Nộp/chốt/mở lại/sao chép kỳ trước ở /v1/forms (profile kn_cnts).

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func (h Handler) registerCNTS(g *gin.RouterGroup) {
	g.GET("/cnts/period-forms", h.cntsForms)
	g.POST("/cnts/period-forms/install", h.cntsInstall)
	g.POST("/cnts/period-series/preview", h.cntsPreview)
	g.POST("/cnts/period-series", h.cntsPublish)
}

type cntsFormDTO struct {
	Code             string             `json:"code"`
	BM               string             `json:"bm"`
	Title            string             `json:"title"`
	UnitKind         string             `json:"unit_kind"`
	PeriodType       string             `json:"period_type"`
	DeadlineOffset   int                `json:"deadline_offset_days"`
	DeadlineTime     string             `json:"deadline_time"`
	Due              string             `json:"due"`
	NilLabel         *string            `json:"nil_label"`
	Fields           []domain.CNTSField `json:"fields"`
	TemplateID       *string            `json:"template_id"`
	TemplateVersion  *int64             `json:"template_version"`
	TemplateUpToDate bool               `json:"template_up_to_date"`
}

func cntsFormsOf(vs []app.CNTSFormView) []cntsFormDTO {
	out := make([]cntsFormDTO, 0, len(vs))
	for _, v := range vs {
		f := v.Form
		d := cntsFormDTO{Code: f.Code, BM: f.BM, Title: f.Title, UnitKind: f.UnitKind, PeriodType: f.PeriodType, DeadlineOffset: f.Deadline.OffsetDays,
			DeadlineTime: f.Deadline.Time, Due: f.Due, Fields: f.Fields, TemplateUpToDate: v.Current}
		if f.NilLabel != "" {
			n := f.NilLabel
			d.NilLabel = &n
		}
		if v.Installed != nil {
			id, ver := v.Installed.ID.String(), v.Installed.Version
			d.TemplateID, d.TemplateVersion = &id, &ver
		}
		out = append(out, d)
	}
	return out
}

func (h Handler) cntsForms(c *gin.Context) {
	vs, err := h.Svc.CNTSForms(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": cntsFormsOf(vs)})
}

func (h Handler) cntsInstall(c *gin.Context) {
	vs, err := h.Svc.InstallCNTSForms(c.Request.Context(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": cntsFormsOf(vs)})
}

type cntsSeriesReq struct {
	Code     string   `json:"code"`
	From     string   `json:"from_date"`
	To       string   `json:"to_date"`
	Units    []string `json:"units"`
	Deadline *struct {
		OffsetDays int    `json:"offset_days"`
		Time       string `json:"time"`
	} `json:"deadline"`
}

func (r cntsSeriesReq) input() app.CNTSSeriesInput {
	in := app.CNTSSeriesInput{Code: r.Code, From: r.From, To: r.To, Units: r.Units}
	if r.Deadline != nil {
		in.Deadline = &domain.Deadline{OffsetDays: r.Deadline.OffsetDays, Time: r.Deadline.Time}
	}
	return in
}

type cntsPeriodDTO struct {
	ID       *string `json:"id"`
	Kind     string  `json:"kind"`
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	FromDate string  `json:"from_date"`
	ToDate   string  `json:"to_date"`
	DueAt    string  `json:"due_at"`
	Rule     string  `json:"rule"`
}

type cntsSeriesDTO struct {
	Code string `json:"code"`
	// Lịch tuần của phòng (D8); null với tuần làm việc BM-05.
	CalendarID      *string         `json:"calendar_id"`
	CalendarVersion *int            `json:"calendar_version"`
	UnitKind        string          `json:"unit_kind"`
	Units           []string        `json:"units"`
	Tasks           int             `json:"tasks"`
	Periods         []cntsPeriodDTO `json:"periods"`
}

func cntsSeriesOf(s app.CNTSSeries) cntsSeriesDTO {
	d := cntsSeriesDTO{Code: s.Form.Code, UnitKind: s.Form.UnitKind, Units: s.Units, Tasks: s.Tasks, Periods: []cntsPeriodDTO{}}
	if s.Calendar != nil {
		id, v := s.Calendar.ID, s.Calendar.Version
		d.CalendarID, d.CalendarVersion = &id, &v
	}
	for i, p := range s.Periods {
		pd := cntsPeriodDTO{Kind: p.Kind, Key: p.Key, Label: p.Label, FromDate: p.From.Format(time.DateOnly), ToDate: p.To.Format(time.DateOnly),
			DueAt: p.DueAt.UTC().Format(time.RFC3339), Rule: p.Rule}
		if i < len(s.Published) {
			id := s.Published[i].ID.String()
			pd.ID = &id
		}
		d.Periods = append(d.Periods, pd)
	}
	return d
}

func (h Handler) cntsPreview(c *gin.Context) {
	var req cntsSeriesReq
	if !bind(c, &req) {
		return
	}
	s, err := h.Svc.PreviewCNTSSeries(c.Request.Context(), req.input())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, cntsSeriesOf(s))
}

func (h Handler) cntsPublish(c *gin.Context) {
	var req cntsSeriesReq
	if !bind(c, &req) {
		return
	}
	s, err := h.Svc.PublishCNTSSeries(c.Request.Context(), req.input(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, cntsSeriesOf(s))
}
