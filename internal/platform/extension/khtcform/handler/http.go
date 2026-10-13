// Package handler: route phiếu "Báo cáo cho KHTC" (W11.63 rút gọn — BRD KHTC v2.2 FN-PN-03.01, FN-PN-03.02, FN-TI-02.04)
// dưới /v1/extension/khtc. Ngữ cảnh phiên KN (header X-AG-Context): Cấp Tỉnh của phòng được giao nhập/lưu nháp/gửi; Cấp
// Tỉnh Phòng KHTC rà soát (yêu cầu bổ sung, xác nhận); Phòng KHTC, Admin xem.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/domain"
	pdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gắn route phiếu KHTC.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc")
	g.GET("/form-templates", h.templates)
	g.GET("/my-forms", h.tasks)
	g.GET("/periods/:id/forms/:dept", h.get)
	g.POST("/periods/:id/forms/:dept/draft", h.draft)
	g.POST("/periods/:id/forms/:dept/submit", h.submit)
	g.POST("/periods/:id/forms/:dept/review", h.review)
}

func path(c *gin.Context) (uuid.UUID, string, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return uuid.Nil, "", false
	}
	return id, c.Param("dept"), true
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type templateDTO struct {
	Code         string           `json:"code"`
	Version      int              `json:"version"`
	Department   string           `json:"department"`
	Title        string           `json:"title"`
	Requirements string           `json:"requirements"`
	Columns      []domain.Column  `json:"columns"`
	Sections     []domain.Section `json:"sections"`
}

func templateOf(t domain.Template) templateDTO {
	cols, secs := t.Columns, t.Sections
	if cols == nil {
		cols = []domain.Column{}
	}
	if secs == nil {
		secs = []domain.Section{}
	}
	return templateDTO{Code: t.Code, Version: t.Version, Department: t.Department, Title: t.Title, Requirements: t.Requirements,
		Columns: cols, Sections: secs}
}

type rowDTO struct {
	EntryRef      string            `json:"entry_ref"`
	No            int               `json:"no"`
	Code          string            `json:"code"`
	Name          string            `json:"name"`
	Unit          string            `json:"unit"`
	PlannedQty    *string           `json:"planned_qty"`
	ScaleNote     string            `json:"scale_note"`
	ModuleSourced bool              `json:"module_sourced"`
	SourceKey     string            `json:"source_key"`
	Cells         map[string]string `json:"cells"`
	OverPlan      bool              `json:"over_plan"`
}

type tabDTO struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Plan  string   `json:"plan"`
	Rows  []rowDTO `json:"rows"`
}

type periodDTO struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ReportType     string    `json:"report_type"`
	Year           int       `json:"year"`
	FromDate       string    `json:"from_date"`
	ToDate         string    `json:"to_date"`
	CutoffDate     string    `json:"cutoff_date"`
	DueAt          time.Time `json:"due_at"`
	MaxSubmissions int       `json:"max_submissions"`
	Terminal       string    `json:"terminal"`
}

type planDTO struct {
	Version       int     `json:"version"`
	DecisionNo    string  `json:"decision_no"`
	EffectiveFrom string  `json:"effective_from"`
	VersionID     string  `json:"version_id"`
	DecisionDate  *string `json:"decision_date"`
}

type adjPeriodDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Year int    `json:"year"`
}

type submissionDTO struct {
	ID          string     `json:"id"`
	Attempt     int64      `json:"attempt"`
	SubmittedAt time.Time  `json:"submitted_at"`
	Late        bool       `json:"late"`
	Decision    string     `json:"decision"`
	ReviewNote  string     `json:"review_note"`
	ReviewedAt  *time.Time `json:"reviewed_at"`
}

type formDTO struct {
	Period               periodDTO       `json:"period"`
	Department           string          `json:"department"`
	DepartmentName       string          `json:"department_name"`
	State                string          `json:"state"`
	StateLabel           string          `json:"state_label"`
	Attempt              int64           `json:"attempt"`
	LimitReached         bool            `json:"limit_reached"`
	CanRequestSupplement bool            `json:"can_request_supplement"`
	CanConfirm           bool            `json:"can_confirm"`
	CanReview            bool            `json:"can_review"`
	Editable             bool            `json:"editable"`
	Overdue              bool            `json:"overdue"`
	RequestNote          string          `json:"request_note"`
	Template             templateDTO     `json:"template"`
	Grid                 bool            `json:"grid"`
	PlanMissing          bool            `json:"plan_missing"`
	Plan                 *planDTO        `json:"plan"`
	Tabs                 []tabDTO        `json:"tabs"`
	Content              domain.Sheet    `json:"content"`
	ContentSource        string          `json:"content_source"`
	DraftAt              *time.Time      `json:"draft_at"`
	Valid                bool            `json:"valid"`
	Issues               []domain.Issue  `json:"issues"`
	AdjPeriods           []adjPeriodDTO  `json:"adj_periods"`
	Submissions          []submissionDTO `json:"submissions"`
}

func formOf(f app.Form) formDTO {
	p := f.Period
	out := formDTO{Period: periodDTO{ID: p.ID.String(), Name: p.Name, ReportType: p.Type, Year: p.Year, FromDate: day(p.From), ToDate: day(p.To),
		CutoffDate: day(p.Cutoff), DueAt: f.DueAt, MaxSubmissions: p.MaxSubmissions, Terminal: f.Terminal},
		Department: f.Department, DepartmentName: f.DepartmentName, State: f.View.State, StateLabel: pdomain.FormLabels[f.View.State],
		Attempt: f.View.Attempt, LimitReached: f.View.LimitReached, CanRequestSupplement: f.View.CanRequestSupplement && f.CanReview,
		CanConfirm: f.View.CanConfirm && f.CanReview, CanReview: f.CanReview, Editable: f.Editable, Overdue: f.Overdue,
		RequestNote: f.RequestNote, Template: templateOf(f.Template), Grid: f.Grid, PlanMissing: f.PlanMissing, Tabs: []tabDTO{},
		Content: f.Content, ContentSource: f.ContentSource, DraftAt: f.DraftAt, Valid: f.Result.Valid(), Issues: f.Result.Issues,
		AdjPeriods: []adjPeriodDTO{}, Submissions: []submissionDTO{}}
	if out.Issues == nil {
		out.Issues = []domain.Issue{}
	}
	if pv := f.PlanVersion; pv != nil {
		out.Plan = &planDTO{Version: pv.Version, DecisionNo: pv.DecisionNo, EffectiveFrom: day(pv.EffectiveFrom), VersionID: pv.VersionID.String()}
		if pv.DecisionDate != nil {
			d := day(*pv.DecisionDate)
			out.Plan.DecisionDate = &d
		}
	}
	for _, t := range f.Tabs {
		td := tabDTO{Key: t.Key, Label: t.Label, Plan: t.Plan, Rows: []rowDTO{}}
		for _, r := range t.Rows {
			rd := rowDTO{EntryRef: r.EntryRef.String(), No: r.No, Code: r.Code, Name: r.Name, Unit: r.Unit, ScaleNote: r.ScaleNote,
				ModuleSourced: r.ModuleSourced, SourceKey: r.SourceKey, Cells: map[string]string{}}
			if r.PlannedQty != nil {
				q := r.PlannedQty.String()
				rd.PlannedQty = &q
			}
			if rr, ok := f.Result.Rows[r.EntryRef]; ok {
				rd.Cells, rd.OverPlan = rr.Cells, rr.OverPlan
			}
			td.Rows = append(td.Rows, rd)
		}
		out.Tabs = append(out.Tabs, td)
	}
	for _, a := range f.AdjPeriods {
		out.AdjPeriods = append(out.AdjPeriods, adjPeriodDTO{ID: a.ID.String(), Name: a.Name, Year: a.Year})
	}
	for _, s := range f.Submissions {
		out.Submissions = append(out.Submissions, submissionDTO{ID: s.ID.String(), Attempt: s.Attempt, SubmittedAt: s.SubmittedAt, Late: s.Late,
			Decision: s.Decision, ReviewNote: s.ReviewNote, ReviewedAt: s.ReviewedAt})
	}
	return out
}

type taskDTO struct {
	PeriodID        string     `json:"period_id"`
	Name            string     `json:"name"`
	ReportType      string     `json:"report_type"`
	Year            int        `json:"year"`
	FromDate        string     `json:"from_date"`
	ToDate          string     `json:"to_date"`
	DueAt           time.Time  `json:"due_at"`
	Overdue         bool       `json:"overdue"`
	State           string     `json:"state"`
	StateLabel      string     `json:"state_label"`
	Attempt         int64      `json:"attempt"`
	MaxSubmissions  int        `json:"max_submissions"`
	LimitReached    bool       `json:"limit_reached"`
	RequestNote     string     `json:"request_note"`
	LastSubmittedAt *time.Time `json:"last_submitted_at"`
	Late            bool       `json:"late"`
	Actionable      bool       `json:"actionable"` // "Cần xử lý": Chưa nhập, Lưu nháp, Trễ hạn, Yêu cầu bổ sung
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) templates(c *gin.Context) {
	items := []templateDTO{}
	for _, t := range append(domain.Seeds(), domain.Generic()) {
		items = append(items, templateOf(t))
	}
	c.JSON(http.StatusOK, gin.H{"column_kinds": domain.ColumnKinds, "items": items})
}

func (h Handler) tasks(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	ts, err := h.Svc.Tasks(c.Request.Context(), c.Query("department"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]taskDTO, 0, len(ts))
	for _, t := range ts {
		st := t.View.State
		items = append(items, taskDTO{PeriodID: t.Period.ID.String(), Name: t.Period.Name, ReportType: t.Period.Type, Year: t.Period.Year,
			FromDate: day(t.Period.From), ToDate: day(t.Period.To), DueAt: t.DueAt, Overdue: t.Overdue, State: st,
			StateLabel: pdomain.FormLabels[st], Attempt: t.View.Attempt, MaxSubmissions: t.Period.MaxSubmissions, LimitReached: t.View.LimitReached,
			RequestNote: t.RequestNote, LastSubmittedAt: t.LastAt, Late: t.Late,
			Actionable: st == pdomain.FormNotStarted || st == pdomain.FormDraft || st == pdomain.FormLate || st == pdomain.FormSupplement})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) get(c *gin.Context) {
	id, dept, ok := path(c)
	if !ok {
		return
	}
	f, err := h.Svc.Get(c.Request.Context(), id, dept)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, formOf(f))
}

type saveReq struct {
	Sheet       domain.Sheet `json:"sheet"`
	BaseAttempt int64        `json:"base_attempt"`
}

func (h Handler) save(c *gin.Context, fn func(uuid.UUID, string, app.SaveInput, string) (app.Form, error)) {
	id, dept, ok := path(c)
	if !ok {
		return
	}
	var r saveReq
	if err := c.ShouldBindJSON(&r); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	f, err := fn(id, dept, app.SaveInput{Sheet: r.Sheet, BaseAttempt: r.BaseAttempt}, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, formOf(f))
}

func (h Handler) draft(c *gin.Context) {
	h.save(c, func(id uuid.UUID, dept string, in app.SaveInput, idem string) (app.Form, error) {
		return h.Svc.SaveDraft(c.Request.Context(), id, dept, in, idem)
	})
}

func (h Handler) submit(c *gin.Context) {
	h.save(c, func(id uuid.UUID, dept string, in app.SaveInput, idem string) (app.Form, error) {
		return h.Svc.Submit(c.Request.Context(), id, dept, in, idem)
	})
}

func (h Handler) review(c *gin.Context) {
	id, dept, ok := path(c)
	if !ok {
		return
	}
	var r struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	f, err := h.Svc.Review(c.Request.Context(), id, dept, r.Decision, r.Note, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, formOf(f))
}
