// Package handler: route Kỳ KHTC /v1/extension/khtc/* (W11.62 — BRD KHTC v2.2 FN-TI-02.03). Ngữ cảnh phiên KN (header
// X-AG-Context); mở kỳ, sửa hạn, đóng, hủy chỉ Cấp Tỉnh Phòng KHTC. Phiếu của các phòng nộp/rà soát ở /v1/forms
// (profile kn_khtc).
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gắn route Kỳ KHTC.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 (đã có xác thực, ngữ cảnh phiên, Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc")
	g.GET("/weeks", h.weeks)
	g.GET("/form-template", h.template)
	g.POST("/form-template/install", h.install)
	g.GET("/periods", h.list)
	g.POST("/periods/preview", h.preview)
	g.POST("/periods", h.open)
	g.GET("/periods/:id", h.get)
	g.POST("/periods/:id/deadline", h.deadline)
	g.POST("/periods/:id/close", h.closePeriod)
	g.POST("/periods/:id/cancel", h.cancel)
	// W11.11: "Báo cáo cho KHTC" phía phòng (FN-PN-03.01) — phiếu của phòng theo kỳ KHTC.
	g.GET("/department-forms", h.departmentForms)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

func parseDay(field, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "ngày dạng YYYY-MM-DD")
	}
	return &t, nil
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type openReq struct {
	ReportType  string     `json:"report_type"`
	Year        int        `json:"year"`
	No          int        `json:"no"`
	Scope       string     `json:"scope"`
	AsOfDate    string     `json:"as_of_date"` // Mẫu 5: "Đến ngày"
	Name        string     `json:"name"`       // phát sinh
	FromDate    string     `json:"from_date"`  // phát sinh
	ToDate      string     `json:"to_date"`    // phát sinh
	CutoffDate  string     `json:"cutoff_date"`
	DueAt       *time.Time `json:"due_at"`
	ExportDueAt *time.Time `json:"export_due_at"`
	Departments []string   `json:"departments"`
}

func (r openReq) input() (app.OpenInput, error) {
	in := app.OpenInput{Request: domain.Request{Type: r.ReportType, Year: r.Year, No: r.No, Scope: r.Scope, Name: r.Name},
		Overrides: domain.Overrides{DueAt: r.DueAt, ExportDueAt: r.ExportDueAt}, Departments: r.Departments}
	var err error
	if in.Request.ToDate, err = parseDay("as_of_date", r.AsOfDate); err != nil {
		return in, err
	}
	if in.Request.From, err = parseDay("from_date", r.FromDate); err != nil {
		return in, err
	}
	if in.Request.To, err = parseDay("to_date", r.ToDate); err != nil {
		return in, err
	}
	in.Overrides.Cutoff, err = parseDay("cutoff_date", r.CutoffDate)
	return in, err
}

type deptDTO struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type sourceDTO struct {
	Department      string     `json:"department"`
	Source          string     `json:"source"`
	CalendarID      string     `json:"calendar_id"`
	CalendarVersion string     `json:"calendar_version"`
	NativeKey       string     `json:"native_key"`
	Label           string     `json:"label"`
	Known           bool       `json:"known"`
	Closed          bool       `json:"closed"`
	ClosedAt        *time.Time `json:"closed_at"`
}

type periodHead struct {
	ReportType     string    `json:"report_type"`
	Year           int       `json:"year"`
	No             *int      `json:"no"`
	Scope          *string   `json:"scope"`
	PeriodKey      *string   `json:"period_key"`
	Name           string    `json:"name"`
	FromDate       string    `json:"from_date"`
	ToDate         string    `json:"to_date"`
	CutoffDate     string    `json:"cutoff_date"`
	DueAt          time.Time `json:"due_at"`
	ExportDueAt    time.Time `json:"export_due_at"`
	MaxSubmissions int       `json:"max_submissions"`
	Departments    []deptDTO `json:"departments"`
}

type draftDTO struct {
	periodHead
	DueRequired       bool        `json:"due_required"`
	PlanDepartments   bool        `json:"plan_departments"`
	DepartmentsLoaded bool        `json:"departments_loaded"`
	Sources           []sourceDTO `json:"sources"`
	SourcesLinked     bool        `json:"sources_linked"`
	Duplicate         *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"duplicate"`
	TemplateVersion int64 `json:"template_version"`
}

func ptrIf[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func draftOf(d app.Draft) draftDTO {
	p := d.Plan
	out := draftDTO{periodHead: periodHead{ReportType: p.Type, Year: p.Year, No: ptrIf(p.No), Scope: ptrIf(p.Scope), PeriodKey: ptrIf(p.Key),
		Name: p.Name, FromDate: day(p.From), ToDate: day(p.To), CutoffDate: day(p.Cutoff), DueAt: p.DueAt, ExportDueAt: p.ExportDueAt,
		MaxSubmissions: d.MaxSubmissions, Departments: []deptDTO{}},
		DueRequired: d.DueRequired, PlanDepartments: d.PlanDepartments, DepartmentsLoaded: d.DepartmentsLoaded, Sources: []sourceDTO{},
		SourcesLinked: d.SourcesLinked, TemplateVersion: d.TemplateVersion}
	if p.Type == domain.TypeYear || p.Type == domain.TypeProgram || p.Type == domain.TypeAdhoc {
		out.No = nil
	}
	for _, dp := range d.Departments {
		out.Departments = append(out.Departments, deptDTO{Code: dp.Code, Name: dp.Name})
	}
	for _, s := range d.Sources {
		out.Sources = append(out.Sources, sourceDTO{Department: s.Department, Source: s.Source, CalendarID: s.CalendarID,
			CalendarVersion: s.CalendarVersion, NativeKey: s.NativeKey, Label: s.Label})
	}
	if d.Duplicate != nil {
		out.Duplicate = &struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}{d.Duplicate.ID, d.Duplicate.Name}
	}
	return out
}

type formDTO struct {
	Department           string     `json:"department"`
	DepartmentName       string     `json:"department_name"`
	State                string     `json:"state"`
	StateLabel           string     `json:"state_label"`
	Attempt              int64      `json:"attempt"`
	LimitReached         bool       `json:"limit_reached"`
	CanRequestSupplement bool       `json:"can_request_supplement"`
	CanConfirm           bool       `json:"can_confirm"`
	LastSubmittedAt      *time.Time `json:"last_submitted_at"`
	ReviewedAt           *time.Time `json:"reviewed_at"`
	ReviewNote           string     `json:"review_note"`
	Late                 bool       `json:"late"`
}

type eventDTO struct {
	ID          uuid.UUID  `json:"id"`
	Action      string     `json:"action"`
	Forced      bool       `json:"forced"`
	Reason      *string    `json:"reason"`
	DueAt       *time.Time `json:"due_at"`
	ExportDueAt *time.Time `json:"export_due_at"`
	State       *string    `json:"state"`
	ActorRef    uuid.UUID  `json:"actor_ref"`
	At          time.Time  `json:"at"`
}

type countsDTO struct {
	Forms         int `json:"forms"`
	FormsSent     int `json:"forms_sent"`
	Sources       int `json:"sources"`
	SourcesClosed int `json:"sources_closed"`
}

type periodDTO struct {
	ID uuid.UUID `json:"id"`
	periodHead
	FormsPeriodID uuid.UUID   `json:"forms_period_id"`
	FormsStatus   string      `json:"forms_status"`
	TemplateCode  string      `json:"template_code"`
	PeriodRule    string      `json:"period_rule"`
	State         string      `json:"state"`
	StateLabel    string      `json:"state_label"`
	Overdue       bool        `json:"overdue"`
	Counts        countsDTO   `json:"counts"`
	Forms         []formDTO   `json:"forms"`
	Sources       []sourceDTO `json:"sources"`
	Events        []eventDTO  `json:"events"`
	CanClose      bool        `json:"can_close"`
	CloseForced   bool        `json:"close_forced"`
	CanCancel     bool        `json:"can_cancel"`
	CloseLateDays int         `json:"close_late_days"`
	OpenedAt      time.Time   `json:"opened_at"`
	OpenedBy      uuid.UUID   `json:"opened_by"`
}

func periodOf(d app.Detail) periodDTO {
	p := d.Period
	out := periodDTO{ID: p.ID, periodHead: periodHead{ReportType: p.Type, Year: p.Year, No: p.No, Scope: p.Scope, PeriodKey: p.Key, Name: p.Name,
		FromDate: day(p.From), ToDate: day(p.To), CutoffDate: day(p.Cutoff), DueAt: d.DueAt, ExportDueAt: d.ExportDueAt,
		MaxSubmissions: p.MaxSubmissions, Departments: []deptDTO{}},
		FormsPeriodID: p.FormsPeriodRef, FormsStatus: d.FormsStatus, TemplateCode: p.TemplateCode, PeriodRule: p.Rule, State: d.State,
		StateLabel: domain.PeriodLabels[d.State], Overdue: d.Overdue, Counts: countsDTO(d.Counts), Forms: []formDTO{}, Sources: []sourceDTO{},
		Events: []eventDTO{}, CanClose: d.CanClose, CloseForced: d.CloseForced, CanCancel: d.CanCancel, CloseLateDays: d.CloseLateDays,
		OpenedAt: p.EventTime, OpenedBy: p.ActorRef}
	for _, f := range d.Forms {
		out.Departments = append(out.Departments, deptDTO{Code: f.Department, Name: f.DepartmentName})
		out.Forms = append(out.Forms, formDTO{Department: f.Department, DepartmentName: f.DepartmentName, State: f.View.State,
			StateLabel: domain.FormLabels[f.View.State], Attempt: f.View.Attempt, LimitReached: f.View.LimitReached,
			CanRequestSupplement: f.View.CanRequestSupplement, CanConfirm: f.View.CanConfirm, LastSubmittedAt: f.LastSubmittedAt,
			ReviewedAt: f.ReviewedAt, ReviewNote: f.ReviewNote, Late: f.Late})
	}
	for _, s := range d.Sources {
		out.Sources = append(out.Sources, sourceDTO{Department: s.Ref.Department, Source: s.Ref.Source, CalendarID: s.Ref.CalendarID,
			CalendarVersion: s.Ref.CalendarVersion, NativeKey: s.Ref.NativeKey, Label: s.Ref.Label, Known: s.Known, Closed: s.Closed, ClosedAt: s.At})
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, eventOf(e))
	}
	return out
}

func eventOf(e store.Event) eventDTO {
	return eventDTO{ID: e.ID, Action: e.Action, Forced: e.Forced, Reason: e.Reason, DueAt: e.DueAt, ExportDueAt: e.ExportDueAt, State: e.State,
		ActorRef: e.ActorRef, At: e.EventTime}
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) weeks(c *gin.Context) {
	y, _ := strconv.Atoi(c.Query("year"))
	ws, err := h.Svc.Weeks(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ws))
	for _, w := range ws {
		items = append(items, gin.H{"no": w.No, "from_date": day(w.From), "to_date": day(w.To)})
	}
	c.JSON(http.StatusOK, gin.H{"year": y, "items": items})
}

func templateOf(t app.TemplateState) gin.H {
	return gin.H{"code": t.Code, "version": t.Version, "title": t.Title, "installed": t.Version > 0}
}

func (h Handler) template(c *gin.Context) {
	t, err := h.Svc.Template(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, templateOf(t))
}

func (h Handler) install(c *gin.Context) {
	t, err := h.Svc.InstallTemplate(c.Request.Context(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, templateOf(t))
}

func (h Handler) list(c *gin.Context) {
	y, _ := strconv.Atoi(c.Query("year"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	ds, err := h.Svc.List(c.Request.Context(), y, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]periodDTO, 0, len(ds))
	for _, d := range ds {
		items = append(items, periodOf(d))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) preview(c *gin.Context) {
	var req openReq
	if !bind(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.Preview(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, draftOf(d))
}

func (h Handler) open(c *gin.Context) {
	var req openReq
	if !bind(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.Open(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, periodOf(d))
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	d, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodOf(d))
}

func (h Handler) deadline(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		DueAt       time.Time  `json:"due_at"`
		ExportDueAt *time.Time `json:"export_due_at"`
		Reason      string     `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	in := app.DeadlineInput{DueAt: req.DueAt, Reason: req.Reason}
	if req.ExportDueAt != nil {
		in.ExportDueAt = *req.ExportDueAt
	}
	d, err := h.Svc.ChangeDeadline(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodOf(d))
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) closePeriod(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	d, err := h.Svc.Close(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodOf(d))
}

func (h Handler) cancel(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	d, err := h.Svc.Cancel(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodOf(d))
}

// ---------------------------------------------------------------------------
// W11.11 — "Báo cáo cho KHTC" phía phòng (FN-PN-03.01)
// ---------------------------------------------------------------------------

type inboxRowDTO struct {
	PeriodID        uuid.UUID  `json:"period_id"`
	FormsPeriodID   uuid.UUID  `json:"forms_period_id"`
	ReportType      string     `json:"report_type"`
	ReportTypeLabel string     `json:"report_type_label"`
	Year            int        `json:"year"`
	Name            string     `json:"name"`
	FromDate        string     `json:"from_date"`
	ToDate          string     `json:"to_date"`
	DueAt           time.Time  `json:"due_at"`
	PeriodState     string     `json:"period_state"`
	PeriodLabel     string     `json:"period_state_label"`
	State           string     `json:"state"`
	StateLabel      string     `json:"state_label"`
	Attempt         int64      `json:"attempt"`
	MaxSubmissions  int        `json:"max_submissions"`
	LimitReached    bool       `json:"limit_reached"`
	Late            bool       `json:"late"`
	LastSubmittedAt *time.Time `json:"last_submitted_at"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	SupplementNote  *string    `json:"supplement_note"`
	NeedsAction     bool       `json:"needs_action"`
	Action          string     `json:"action"`
	ActionLabel     string     `json:"action_label"`
	EntryRoute      string     `json:"entry_route"`
}

func (h Handler) departmentForms(c *gin.Context) {
	y, _ := strconv.Atoi(c.Query("year"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	in, err := h.Svc.DepartmentForms(c.Request.Context(), app.InboxQuery{View: c.Query("view"), Department: c.Query("department"), Year: y,
		Limit: limit})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]inboxRowDTO, 0, len(in.Rows))
	for _, r := range in.Rows {
		p := r.Period
		items = append(items, inboxRowDTO{PeriodID: p.ID, FormsPeriodID: p.FormsPeriodRef, ReportType: p.Type,
			ReportTypeLabel: app.TypeLabels[p.Type], Year: p.Year, Name: p.Name, FromDate: day(p.From), ToDate: day(p.To), DueAt: r.DueAt,
			PeriodState: r.PeriodState, PeriodLabel: domain.PeriodLabels[r.PeriodState], State: r.Form.State,
			StateLabel: domain.FormLabels[r.Form.State], Attempt: r.Form.Attempt, MaxSubmissions: p.MaxSubmissions,
			LimitReached: r.Form.LimitReached, Late: r.Late, LastSubmittedAt: r.LastSubmittedAt, ReviewedAt: r.ReviewedAt,
			SupplementNote: ptrIf(r.SupplementNote), NeedsAction: r.NeedsAction, Action: r.Action, ActionLabel: app.ActionLabels[r.Action],
			EntryRoute: r.EntryRoute})
	}
	c.JSON(http.StatusOK, gin.H{"department": in.Department, "department_name": in.DepartmentName, "view": in.View,
		"read_only": in.ReadOnly, "counts": gin.H{"total": in.Counts.Total, "needs_action": in.Counts.NeedsAction,
			"supplement": in.Counts.Supplement, "late": in.Counts.Late, "sent": in.Counts.Sent}, "items": items})
}
