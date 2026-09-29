// Package handler: HTTP của pf/extension (contracts/openapi/application.yaml, mục
// "# --- pf/extension ---").
package handler

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 đã có xác thực và Idempotency-Key.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension")
	g.POST("/tasks", h.createTask)
	g.GET("/tasks", h.listTasks)
	g.POST("/tasks/bulk-assign", h.bulkAssign)
	g.GET("/tasks/:id", h.getTask)
	g.GET("/support-requests", h.listStaffSupport)
	g.POST("/tasks/:id/assign", h.assignTask)
	g.POST("/tasks/:id/status", h.updateStatus)

	g.POST("/directives", h.issueDirective)
	g.GET("/directives", h.listDirectives)
	g.GET("/directives/:id", h.getDirective)
	g.POST("/directives/:id/ack", h.ackDirective)

	g.POST("/field-reports", h.createReport)
	g.GET("/field-reports", h.listReports)
	g.GET("/field-reports/:id", h.getReport)
	g.POST("/field-reports/:id/corrections", h.correctReport)
	g.POST("/report-periods/close", h.closePeriod)
	g.POST("/program-reports", h.submitProgram)
	g.POST("/program-reports/:id/approve", h.approveProgram)
	g.GET("/program-progress", h.programProgress)

	g.GET("/dashboard", h.dashboard)

	g.POST("/staff", h.upsertStaff)
	g.GET("/roster", h.roster)
	g.GET("/roster/:id/contact", h.rosterContact)
	g.POST("/roster/status", h.setDuty)
	g.POST("/roster/shifts", h.addShift)
	h.registerUnified(rg)
	h.registerSupport(rg)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathID(c *gin.Context, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

// optRef đọc một mã tham chiếu tùy chọn.
func optRef(v *problem.Problem, bad *bool, field string, s *string) (uuid.UUID, *problem.Problem) {
	if s == nil || *s == "" {
		return uuid.Nil, v
	}
	id, err := ids.ParseRef(*s)
	if err != nil {
		*bad = true
		return uuid.Nil, v.WithField(field, "phải là UUID")
	}
	return id, v
}

func limitOf(c *gin.Context) int {
	if n, err := strconv.Atoi(c.Query("limit")); err == nil {
		return n
	}
	return 50
}

func uuidCursor(c *gin.Context) (uuid.UUID, bool) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return after, false
		}
		copy(after[:], b)
	}
	return after, true
}

func nextUUID(n *uuid.UUID) any {
	if n == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(n[:])
}

// ---------------------------------------------------------------------------
// Nhiệm vụ hỗ trợ
// ---------------------------------------------------------------------------

type createTaskReq struct {
	ID             *string `json:"id"`
	CommuneCode    string  `json:"commune_code"`
	CooperativeRef *string `json:"cooperative_ref"`
	HouseholdRef   *string `json:"household_ref"`
	PlotRef        *string `json:"plot_ref"`
	Category       string  `json:"category"`
	Description    string  `json:"description"`
	Priority       string  `json:"priority"`
	AssigneeRef    *string `json:"assignee_ref"`
}

func (h Handler) createTask(c *gin.Context) {
	var req createTaskReq
	if !bind(c, &req) {
		return
	}
	in := app.CreateTaskInput{
		CommuneCode: req.CommuneCode, Category: req.Category, Description: req.Description, Priority: req.Priority, IdemKey: idem(c),
	}
	v, bad := problem.Validation, false
	if req.ID != nil {
		id, err := ids.Parse(*req.ID)
		if err != nil {
			v, bad = v.WithField("id", "phải là UUIDv7"), true
		} else {
			in.ID = &id
		}
	}
	in.CooperativeRef, v = optRef(v, &bad, "cooperative_ref", req.CooperativeRef)
	in.HouseholdRef, v = optRef(v, &bad, "household_ref", req.HouseholdRef)
	in.PlotRef, v = optRef(v, &bad, "plot_ref", req.PlotRef)
	in.AssigneeRef, v = optRef(v, &bad, "assignee_ref", req.AssigneeRef)
	if bad {
		httpx.Fail(c, v)
		return
	}
	res, err := h.Svc.CreateTask(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/extension/tasks/"+res.Task.ID.String())
	c.JSON(status, toTask(res.Task, h.Svc.Clock.Now()))
}

func (h Handler) listTasks(c *gin.Context) {
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	q := app.TaskQuery{
		Status: c.Query("status"), Priority: c.Query("priority"), Mine: c.Query("mine") == "true",
		Overdue: c.Query("overdue") == "true", After: after, Limit: limitOf(c),
	}
	if s := c.Query("assignee_ref"); s != "" {
		a, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("assignee_ref", "phải là UUID"))
			return
		}
		q.Assignee = a
	}
	page, err := h.Svc.ListTasks(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	names := h.Svc.NamesFor(c.Request.Context(), page.Items)
	items := make([]taskDTO, 0, len(page.Items))
	for _, t := range page.Items {
		items = append(items, withNames(toTask(t, now), t, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextUUID(page.Next)})
}

func (h Handler) getTask(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetTask(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	evs := make([]eventDTO, 0, len(d.Events))
	for _, e := range d.Events {
		evs = append(evs, toEvent(e))
	}
	names := h.Svc.NamesFor(c.Request.Context(), []domain.Task{d.Task})
	c.JSON(http.StatusOK, gin.H{"task": withNames(toTask(d.Task, h.Svc.Clock.Now()), d.Task, names), "events": evs})
}

type assignReq struct {
	AssigneeRef string `json:"assignee_ref"`
	Note        string `json:"note"`
}

func (h Handler) assignTask(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	var req assignReq
	if !bind(c, &req) {
		return
	}
	a, err := ids.ParseRef(req.AssigneeRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("assignee_ref", "bắt buộc, phải là UUID"))
		return
	}
	h.writeUpdate(c, app.UpdateInput{TaskID: id, Action: domain.ActionAssigned, AssigneeRef: a, Note: req.Note, IdemKey: idem(c)})
}

type statusReq struct {
	Action    string  `json:"action"`
	Note      string  `json:"note"`
	EventTime *string `json:"event_time"`
}

func (h Handler) updateStatus(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	var req statusReq
	if !bind(c, &req) {
		return
	}
	a, valid := domain.ParseAction(req.Action)
	if !valid || a == domain.ActionAssigned {
		httpx.Fail(c, problem.Validation.WithField("action", "accepted, progress, resolved, escalated hoặc reopened"))
		return
	}
	in := app.UpdateInput{TaskID: id, Action: a, Note: req.Note, IdemKey: idem(c)}
	if req.EventTime != nil {
		t, err := time.Parse(time.RFC3339Nano, *req.EventTime)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("event_time", "phải theo RFC 3339 có múi giờ"))
			return
		}
		in.EventTime = &t
	}
	h.writeUpdate(c, in)
}

func (h Handler) writeUpdate(c *gin.Context, in app.UpdateInput) {
	res, err := h.Svc.UpdateTask(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": toTask(res.Task, h.Svc.Clock.Now()), "event": toEvent(res.Event)})
}

type bulkReq struct {
	TaskIDs      []string `json:"task_ids"`
	AssigneeRefs []string `json:"assignee_refs"`
}

func (h Handler) bulkAssign(c *gin.Context) {
	var req bulkReq
	if !bind(c, &req) {
		return
	}
	in := app.BulkAssignInput{IdemKey: idem(c)}
	for _, s := range req.TaskIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("task_ids", "phải là UUID"))
			return
		}
		in.TaskIDs = append(in.TaskIDs, id)
	}
	for _, s := range req.AssigneeRefs {
		id, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("assignee_refs", "phải là UUID"))
			return
		}
		in.AssigneeRefs = append(in.AssigneeRefs, id)
	}
	res, err := h.Svc.BulkAssign(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	items := make([]taskDTO, 0, len(res))
	for _, r := range res {
		items = append(items, toTask(r.Task, now))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Chỉ đạo khẩn
// ---------------------------------------------------------------------------

type directiveReq struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Severity      string   `json:"severity"`
	AudienceUnits []string `json:"audience_units"`
	ExpiresAt     *string  `json:"expires_at"`
}

func (h Handler) issueDirective(c *gin.Context) {
	var req directiveReq
	if !bind(c, &req) {
		return
	}
	in := app.IssueDirectiveInput{Title: req.Title, Body: req.Body, Severity: req.Severity, AudienceUnits: req.AudienceUnits, IdemKey: idem(c)}
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339Nano, *req.ExpiresAt)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("expires_at", "phải theo RFC 3339 có múi giờ"))
			return
		}
		in.ExpiresAt = &t
	}
	res, err := h.Svc.IssueDirective(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/extension/directives/"+res.Directive.ID.String())
	c.JSON(status, toDirective(res.Directive))
}

func (h Handler) listDirectives(c *gin.Context) {
	var after int64
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 8 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = int64(binary.BigEndian.Uint64(b))
	}
	page, err := h.Svc.ListDirectives(c.Request.Context(), app.DirectiveQuery{Mine: c.Query("mine") == "true", After: after, Limit: limitOf(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]directiveDTO, 0, len(page.Items))
	for _, d := range page.Items {
		items = append(items, toDirective(d))
	}
	var next any
	if page.Next != nil {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(*page.Next))
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) getDirective(c *gin.Context) {
	id, ok := pathID(c, app.ErrDirectiveNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetDirective(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := gin.H{"directive": toDirective(d.Directive), "pending_targets": nil}
	if d.Pending != nil {
		resp["pending_targets"] = toTargets(d.Pending)
	}
	c.JSON(http.StatusOK, resp)
}

func (h Handler) ackDirective(c *gin.Context) {
	id, ok := pathID(c, app.ErrDirectiveNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.AckDirective(c.Request.Context(), id, nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDirective(d))
}

// ---------------------------------------------------------------------------
// Báo cáo hiện trường
// ---------------------------------------------------------------------------

type reportReq struct {
	ID *string `json:"id"`
	app.ReportPayload
	EventTime *string `json:"event_time"`
}

type correctionReq struct {
	app.ReportPayload
	CorrectionReason *string `json:"correction_reason"`
	EventTime        *string `json:"event_time"`
}

func parseEventTime(v *problem.Problem, bad *bool, s *string) (*time.Time, *problem.Problem) {
	if s == nil {
		return nil, v
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		*bad = true
		return nil, v.WithField("event_time", "phải theo RFC 3339 có múi giờ")
	}
	return &t, v
}

func (h Handler) createReport(c *gin.Context) {
	var req reportReq
	if !bind(c, &req) {
		return
	}
	r, err := req.ToReport()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	in := app.ReportInput{Report: r, IdemKey: idem(c)}
	v, bad := problem.Validation, false
	if req.ID != nil {
		id, err := ids.Parse(*req.ID)
		if err != nil {
			v, bad = v.WithField("id", "phải là UUIDv7"), true
		} else {
			in.ID = &id
		}
	}
	in.EventTime, v = parseEventTime(v, &bad, req.EventTime)
	if bad {
		httpx.Fail(c, v)
		return
	}
	h.writeReport(c, in)
}

func (h Handler) correctReport(c *gin.Context) {
	id, ok := pathID(c, app.ErrReportNotFound)
	if !ok {
		return
	}
	var req correctionReq
	if !bind(c, &req) {
		return
	}
	r, err := req.ToReport()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	in := app.ReportInput{Report: r, SupersedesID: &id, CorrectionReason: req.CorrectionReason, IdemKey: idem(c)}
	v, bad := problem.Validation, false
	in.EventTime, v = parseEventTime(v, &bad, req.EventTime)
	if bad {
		httpx.Fail(c, v)
		return
	}
	h.writeReport(c, in)
}

func (h Handler) writeReport(c *gin.Context, in app.ReportInput) {
	res, err := h.Svc.SubmitReport(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/extension/field-reports/"+res.Report.ID.String())
	c.JSON(status, toReport(res.Report))
}

func (h Handler) listReports(c *gin.Context) {
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	page, err := h.Svc.ListReports(c.Request.Context(), app.ReportQuery{
		Period: c.Query("period"), Kind: c.Query("kind"), After: after, Limit: limitOf(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]reportDTO, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, toReport(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextUUID(page.Next)})
}

func (h Handler) getReport(c *gin.Context) {
	id, ok := pathID(c, app.ErrReportNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetReport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	vs := make([]reportDTO, 0, len(d.Versions))
	for _, r := range d.Versions {
		vs = append(vs, toReport(r))
	}
	c.JSON(http.StatusOK, gin.H{"report": toReport(d.Report), "versions": vs})
}

type closeReq struct {
	Period       string `json:"period"`
	ProvinceCode string `json:"province_code"`
	Note         string `json:"note"`
}

func (h Handler) closePeriod(c *gin.Context) {
	var req closeReq
	if !bind(c, &req) {
		return
	}
	closed, err := h.Svc.ClosePeriod(c.Request.Context(), app.ClosePeriodInput{Period: req.Period, ProvinceCode: req.ProvinceCode, Note: req.Note})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if !closed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"period": req.Period, "province_code": req.ProvinceCode, "closed": true, "already_closed": !closed})
}

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

func (h Handler) dashboard(c *gin.Context) {
	d, err := h.Svc.GetDashboard(c.Request.Context(), c.Query("unit"), c.Query("period"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	children := make([]gin.H, 0, len(d.Children))
	for _, ch := range d.Children {
		children = append(children, gin.H{"unit": ch.Unit, "kpi": toKPI(ch.KPI)})
	}
	var unit any
	if d.Unit != "" {
		unit = d.Unit
	}
	c.JSON(http.StatusOK, gin.H{
		"unit": unit, "level": d.Level, "period": d.Period, "as_of": ts(d.AsOf), "total": toKPI(d.Total),
		"period_closed": d.Total.PeriodClosed, "closed_provinces": nonNil(d.Total.ClosedProvinceList), "children": children,
	})
}

// ---------------------------------------------------------------------------
// Danh bạ trực
// ---------------------------------------------------------------------------

type staffReq struct {
	ActorRef    string `json:"actor_ref"`
	UnitCode    string `json:"unit_code"`
	RoleTitle   string `json:"role_title"`
	DisplayName string `json:"display_name"`
	Active      *bool  `json:"active"`
}

func (h Handler) upsertStaff(c *gin.Context) {
	var req staffReq
	if !bind(c, &req) {
		return
	}
	a, err := ids.ParseRef(req.ActorRef)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("actor_ref", "bắt buộc, phải là UUID"))
		return
	}
	st := domain.Staff{ActorRef: a, UnitCode: req.UnitCode, RoleTitle: req.RoleTitle, DisplayName: req.DisplayName, Active: true}
	if req.Active != nil {
		st.Active = *req.Active
	}
	out, err := h.Svc.UpsertStaff(c.Request.Context(), st)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toStaff(out))
}

func (h Handler) roster(c *gin.Context) {
	r, err := h.Svc.GetRoster(c.Request.Context(), c.Query("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var unit any
	if r.Unit != "" {
		unit = r.Unit
	}
	c.JSON(http.StatusOK, gin.H{"unit": unit, "on_duty": r.OnDuty, "items": toRoster(r.Items), "fallback": toRoster(r.Fallback)})
}

// rosterContact: số điện thoại của cán bộ để gọi/nhắn 1 chạm (US-DIR-02). Không lưu đệm.
func (h Handler) rosterContact(c *gin.Context) {
	id, ok := pathID(c, app.ErrStaffNotFound)
	if !ok {
		return
	}
	phone, err := h.Svc.StaffPhone(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"actor_ref": id.String(), "phone": phone})
}

type dutyReq struct {
	Status string `json:"status"`
}

func (h Handler) setDuty(c *gin.Context) {
	var req dutyReq
	if !bind(c, &req) {
		return
	}
	m, err := h.Svc.SetDutyStatus(c.Request.Context(), req.Status)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": m.Status, "set_at": ts(m.SetAt), "valid_until": ts(m.ValidUntil)})
}

type shiftReq struct {
	ActorRef string `json:"actor_ref"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

func (h Handler) addShift(c *gin.Context) {
	var req shiftReq
	if !bind(c, &req) {
		return
	}
	v, bad := problem.Validation, false
	a, err := ids.ParseRef(req.ActorRef)
	if err != nil {
		v, bad = v.WithField("actor_ref", "bắt buộc, phải là UUID"), true
	}
	s, err := time.Parse(time.RFC3339Nano, req.StartsAt)
	if err != nil {
		v, bad = v.WithField("starts_at", "phải theo RFC 3339 có múi giờ"), true
	}
	e, err := time.Parse(time.RFC3339Nano, req.EndsAt)
	if err != nil {
		v, bad = v.WithField("ends_at", "phải theo RFC 3339 có múi giờ"), true
	}
	if bad {
		httpx.Fail(c, v)
		return
	}
	sh, err := h.Svc.AddShift(c.Request.Context(), a, s, e)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"shift_id": sh.ID.String(), "actor_ref": sh.ActorRef.String(), "starts_at": ts(sh.StartsAt), "ends_at": ts(sh.EndsAt)})
}
