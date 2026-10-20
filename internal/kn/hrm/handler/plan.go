package handler

// W11.27: HTTP kế hoạch nhân lực năm (FN-11) — /v1/hrm/plans*. Hợp đồng ở contracts/openapi/modules/hrm/application.yaml.

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func (h Handler) registerPlans(rg *gin.RouterGroup) {
	rg.GET("/hrm/plans", h.listPlans)
	rg.POST("/hrm/plans", h.openPlan)
	rg.GET("/hrm/plans/:year", h.planSummary)
	rg.GET("/hrm/plans/:year/export", h.exportPlan)
	rg.POST("/hrm/plans/:year/close", h.closePlan)
	rg.GET("/hrm/plans/:year/units/:unit", h.planUnit)
	rg.GET("/hrm/plans/:year/units/:unit/previous", h.previousPlan)
	rg.PUT("/hrm/plans/:year/units/:unit/draft", h.savePlanDraft)
	rg.POST("/hrm/plans/:year/units/:unit/submit", h.submitPlan)
	rg.POST("/hrm/plans/:year/units/:unit/review", h.reviewPlan)
	rg.POST("/hrm/plans/:year/units/:unit/adjustments", h.adjustPlan)
}

func yearParam(c *gin.Context) (int, bool) {
	y, err := strconv.Atoi(c.Param("year"))
	if err != nil || !domain.ValidPlanYear(y) {
		httpx.Fail(c, problem.Validation.WithField("year", "năm 2020–2100"))
		return 0, false
	}
	return y, true
}

type positionDTO struct {
	Title  string `json:"title"`
	Level  string `json:"level"`
	Count  int    `json:"count"`
	When   string `json:"when"`
	Reason string `json:"reason"`
}

type planInputDTO struct {
	Leaving    int           `json:"leaving"`
	Additional int           `json:"additional"`
	Positions  []positionDTO `json:"positions"`
	Note       string        `json:"note"`
}

func (d planInputDTO) input() domain.PlanInput {
	in := domain.PlanInput{Leaving: d.Leaving, Additional: d.Additional, Note: d.Note, Positions: []domain.PlanPosition{}}
	for _, p := range d.Positions {
		in.Positions = append(in.Positions, domain.PlanPosition(p))
	}
	return in
}

func inputOut(in domain.PlanInput) gin.H {
	pos := make([]positionDTO, 0, len(in.Positions))
	for _, p := range in.Positions {
		pos = append(pos, positionDTO(p))
	}
	return gin.H{"leaving": in.Leaving, "additional": in.Additional, "positions": pos, "note": in.Note}
}

func valuesOut(v domain.PlanValues) gin.H {
	out := inputOut(v.PlanInput)
	out["current"], out["retiring"], out["demand"] = v.Current, v.Retiring, v.Demand()
	return out
}

func timeOut(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func periodOut(p app.PlanPeriod) gin.H {
	return gin.H{"year": p.Year, "label": p.Label, "period_ref": p.PeriodRef, "status": p.Status, "due_at": p.DueAt.UTC().Format(time.RFC3339),
		"cutoff_on": p.CutoffOn.Format(time.DateOnly), "units": p.Units, "submitted": p.Submitted, "opened_at": p.OpenedAt.UTC().Format(time.RFC3339)}
}

func planRowOut(r app.PlanRow) gin.H {
	return gin.H{"unit": r.Unit, "unit_name": r.UnitName, "status": r.Status, "late": r.Late, "versions": r.Versions, "decision": r.Decision,
		"review_note": r.ReviewNote, "submitted_at": timeOut(r.SubmittedAt), "values": valuesOut(r.Values), "source": r.Source,
		"adjusted": r.Adjusted}
}

func (h Handler) listPlans(c *gin.Context) {
	xs, err := h.Svc.ListPlans(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, periodOut(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) openPlan(c *gin.Context) {
	var in struct {
		Year     int      `json:"year"`
		DueOn    string   `json:"due_on"`
		CutoffOn string   `json:"cutoff_on"`
		Units    []string `json:"units"`
	}
	if !decode(c, &in) {
		return
	}
	due, err := dateOf(in.DueOn, "due_on")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var cutoff time.Time
	if in.CutoffOn != "" {
		if cutoff, err = dateOf(in.CutoffOn, "cutoff_on"); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	p, err := h.Svc.OpenPlan(c.Request.Context(), app.OpenPlanInput{Year: in.Year, DueOn: due, CutoffOn: cutoff, Units: in.Units}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, periodOut(p))
}

func (h Handler) planSummary(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	v, err := h.Svc.PlanSummary(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(v.Rows))
	for _, r := range v.Rows {
		rows = append(rows, planRowOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"period": periodOut(v.Period), "rows": rows, "figures_available": v.Figures, "can_manage": v.CanManage,
		"my_unit": v.MyUnit, "totals": gin.H{"current": v.Totals.Current, "retiring": v.Totals.Retiring, "leaving": v.Totals.Leaving,
			"additional": v.Totals.Additional, "demand": v.Totals.Demand}})
}

func (h Handler) exportPlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	b, name, err := h.Svc.ExportPlan(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, b)
}

func (h Handler) closePlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	if err := h.Svc.ClosePlan(c.Request.Context(), y, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	h.planSummary(c)
}

func (h Handler) planUnit(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	v, err := h.Svc.PlanUnit(c.Request.Context(), y, c.Param("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ret := make([]gin.H, 0, len(v.Figures.Retirees))
	for _, r := range v.Figures.Retirees {
		ret = append(ret, gin.H{"staff_id": r.StaffID, "code": r.Code, "full_name": r.FullName, "retire_on": r.RetireOn.Format(time.DateOnly)})
	}
	subs := make([]gin.H, 0, len(v.Submissions))
	for _, s := range v.Submissions {
		subs = append(subs, gin.H{"id": s.ID, "version": s.Version, "values": valuesOut(s.Values), "submitted_at": s.SubmittedAt.UTC().Format(time.RFC3339),
			"late": s.Late, "decision": s.Decision, "review_note": s.ReviewNote, "reviewed_at": timeOut(s.ReviewedAt)})
	}
	adjs := make([]gin.H, 0, len(v.Adjustments))
	for _, a := range v.Adjustments {
		adjs = append(adjs, adjustmentOut(a))
	}
	var draft any
	if v.Draft != nil {
		draft = gin.H{"values": inputOut(v.Draft.Values), "recorded_at": v.Draft.RecordedAt.UTC().Format(time.RFC3339)}
	}
	c.JSON(http.StatusOK, gin.H{"period": periodOut(v.Period), "row": planRowOut(v.Row),
		"figures": gin.H{"available": v.Figures.Available, "current": v.Figures.Current, "retiring": v.Figures.Retiring,
			"unknown": v.Figures.Unknown, "retirees": ret},
		"draft": draft, "submissions": subs, "adjustments": adjs, "can_edit": v.CanEdit, "can_review": v.CanReview, "can_adjust": v.CanAdjust})
}

func adjustmentOut(a app.PlanAdjustment) gin.H {
	return gin.H{"seq": a.Seq, "values": valuesOut(a.Values), "reason": a.Reason, "actor_ref": a.Actor,
		"recorded_at": a.RecordedAt.UTC().Format(time.RFC3339)}
}

func (h Handler) previousPlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	in, from, err := h.Svc.PreviousPlan(c.Request.Context(), y, c.Param("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"from_year": from, "values": inputOut(in)})
}

func (h Handler) savePlanDraft(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	var in planInputDTO
	if !decode(c, &in) {
		return
	}
	d, err := h.Svc.SavePlanDraft(c.Request.Context(), y, c.Param("unit"), in.input(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"values": inputOut(d.Values), "recorded_at": d.RecordedAt.UTC().Format(time.RFC3339)})
}

func (h Handler) submitPlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	var in struct {
		planInputDTO
		BaseVersion int64 `json:"base_version"`
	}
	if !decode(c, &in) {
		return
	}
	s, err := h.Svc.SubmitPlan(c.Request.Context(), y, c.Param("unit"), in.input(), in.BaseVersion, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": s.ID, "version": s.Version, "values": valuesOut(s.Values),
		"submitted_at": s.SubmittedAt.UTC().Format(time.RFC3339), "late": s.Late})
}

func (h Handler) reviewPlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decode(c, &in) {
		return
	}
	s, err := h.Svc.ReviewPlan(c.Request.Context(), y, c.Param("unit"), in.Decision, in.Note, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": s.ID, "version": s.Version, "decision": s.Decision, "review_note": s.ReviewNote,
		"reviewed_at": timeOut(s.ReviewedAt)})
}

func (h Handler) adjustPlan(c *gin.Context) {
	y, ok := yearParam(c)
	if !ok {
		return
	}
	var in struct {
		planInputDTO
		Reason string `json:"reason"`
	}
	if !decode(c, &in) {
		return
	}
	a, err := h.Svc.AdjustPlan(c.Request.Context(), y, c.Param("unit"), in.input(), in.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, adjustmentOut(a))
}
