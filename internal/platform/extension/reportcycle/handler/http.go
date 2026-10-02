// Package handler: HTTP của vòng đời báo cáo KN v7.7 (contracts/openapi/application.yaml, mục
// "# --- pf/extension: vòng đời báo cáo KN v7.7 (P4) ---"). Mọi route cần header X-AG-Context (ngữ cảnh phiên P1).
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 (đã có xác thực, ngữ cảnh phiên, Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension")
	g.GET("/report-forms", h.reportForms)
	g.POST("/report-batches/preview", h.preview)
	g.POST("/report-batches", h.issue)
	g.GET("/report-batches", h.listBatches)
	g.GET("/report-batches/:id", h.getBatch)
	g.GET("/report-batches/:id/log", h.batchLog)
	g.POST("/report-batches/:id/withdraw", h.withdraw)
	g.POST("/report-batches/:id/deadline", h.deadline)
	g.GET("/report-batches/:id/provincial-values", h.provincial)
	g.GET("/report-batches/:id/status", h.status)
	g.POST("/report-batches/:id/reminders", h.remind)
	g.GET("/report-batches/:id/reminders", h.reminders)

	g.GET("/report-tasks", h.listTasks)
	g.GET("/report-tasks/:id", h.getTask)
	g.POST("/report-tasks/:id/submissions", h.submit)
	g.GET("/report-tasks/:id/history", h.history)
	g.GET("/report-tasks/:id/log", h.taskLog)
	g.POST("/report-tasks/:id/adjustment-request", h.adjust)
	g.POST("/report-tasks/:id/confirm", h.confirm)

	g.GET("/kn-periods", h.listPeriods)
	g.GET("/report-periods/:id", h.getPeriod)
	g.GET("/report-periods/:id/log", h.periodLog)
	g.POST("/report-periods/:id/close", h.closePeriod)
	g.POST("/report-periods/:id/reopen", h.reopen)

	g.GET("/consolidations", h.consolidation)
	g.POST("/consolidations", h.consolidate)
	g.GET("/consolidations/pending-changes", h.pending)
	g.GET("/consolidations/runs", h.runs)
	g.GET("/consolidations/:id/changes", h.changes)

	g.GET("/kn-dashboard", h.dashboard)
	g.GET("/commune-reports", h.communeReports)
	g.GET("/commune-reports/stats", h.communeStats)
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

func optUUID(c *gin.Context, name string) (*uuid.UUID, bool) {
	v := strings.TrimSpace(c.Query(name))
	if v == "" {
		return nil, true
	}
	id, err := uuid.Parse(v)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return nil, false
	}
	return &id, true
}

func cursor(c *gin.Context) (*uuid.UUID, bool) {
	cur := c.Query("cursor")
	if cur == "" {
		return nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil || len(b) != 16 {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return nil, false
	}
	var id uuid.UUID
	copy(id[:], b)
	return &id, true
}

func next(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(id[:])
}

func limit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func uuids(ss []string, field string) ([]uuid.UUID, *problem.Problem) {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, problem.Validation.WithField(field, "phải là UUID")
		}
		out = append(out, id)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Phát hành (FN-TI-02.02)
// ---------------------------------------------------------------------------

func (h Handler) reportForms(c *gin.Context) {
	fs, err := h.Svc.AvailableForms(c.Request.Context(), c.Query("report_type"), c.Query("from_date"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(fs))
	for _, f := range fs {
		var prov []gin.H
		for _, col := range f.Definition.Columns {
			if col.Kind == "provincial" {
				prov = append(prov, gin.H{"letter": col.Letter, "title": col.Title, "unit": col.Unit})
			}
		}
		if prov == nil {
			prov = []gin.H{}
		}
		items = append(items, gin.H{"template_id": f.TemplateID, "version_id": f.VersionID, "code": f.Code, "name": f.Name, "version": f.Version,
			"report_types": f.ReportTypes, "period_block": len(f.Definition.PeriodBlocks) > 0, "completeness": f.Definition.Completeness,
			"provincial_columns": prov})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type deadlineReq struct {
	OffsetDays int    `json:"offset_days"`
	Time       string `json:"time"`
}

type batchReq struct {
	ReportType  string   `json:"report_type"`
	TemplateIDs []string `json:"template_ids"`
	Scope       struct {
		Mode     string   `json:"mode"`
		Regions  []string `json:"regions"`
		Communes []string `json:"communes"`
	} `json:"scope"`
	PeriodType       string                                  `json:"period_type"`
	FromDate         string                                  `json:"from_date"`
	ToDate           string                                  `json:"to_date"`
	SeasonLabel      string                                  `json:"season_label"`
	Deadline         *deadlineReq                            `json:"deadline"`
	CloseAfterDays   *int                                    `json:"close_after_days"`
	Note             string                                  `json:"note"`
	ProvincialValues map[string]map[string]map[string]string `json:"provincial_values"`
	ProvincialSource string                                  `json:"provincial_source"`
	CopyProvincial   []string                                `json:"copy_provincial_from_previous"`
}

func (r batchReq) input() (app.BatchInput, *problem.Problem) {
	tpls, p := uuids(r.TemplateIDs, "template_ids")
	if p != nil {
		return app.BatchInput{}, p
	}
	cp, p := uuids(r.CopyProvincial, "copy_provincial_from_previous")
	if p != nil {
		return app.BatchInput{}, p
	}
	in := app.BatchInput{ReportType: r.ReportType, TemplateIDs: tpls, ScopeMode: r.Scope.Mode, Regions: r.Scope.Regions, Communes: r.Scope.Communes,
		PeriodType: r.PeriodType, From: r.FromDate, To: r.ToDate, SeasonLabel: r.SeasonLabel, CloseAfterDays: r.CloseAfterDays, Note: r.Note,
		ProvincialSource: r.ProvincialSource, CopyProvincial: cp, ProvincialValues: map[uuid.UUID]map[string]map[string]string{}}
	if r.Deadline != nil {
		in.Deadline = &domain.Deadline{OffsetDays: r.Deadline.OffsetDays, Time: r.Deadline.Time}
	}
	for k, v := range r.ProvincialValues {
		id, err := uuid.Parse(k)
		if err != nil {
			return in, problem.Validation.WithField("provincial_values", "khóa phải là template_id")
		}
		in.ProvincialValues[id] = v
	}
	return in, nil
}

func previewDTO(pv app.Preview) gin.H {
	forms := make([]gin.H, 0, len(pv.Forms))
	for _, f := range pv.Forms {
		forms = append(forms, gin.H{"template_id": f.Version.TemplateID, "version_id": f.Version.VersionID, "code": f.Version.Code, "name": f.Version.Name,
			"version": f.Version.Version, "period_block": f.PeriodBlock, "slots": f.Slots})
	}
	periods := make([]gin.H, 0, len(pv.Periods))
	for _, p := range pv.Periods {
		periods = append(periods, gin.H{"slot": p.Slot, "label": p.Label, "range_label": p.RangeLabel, "from_date": date(p.From), "to_date": date(p.To),
			"due_at": tss(p.DueAt), "auto_close_at": tss(p.AutoCloseAt)})
	}
	communes := make([]gin.H, 0, len(pv.Communes))
	for _, c := range pv.Communes {
		communes = append(communes, gin.H{"commune_code": c.CommuneCode, "region_code": c.RegionCode, "region_name": c.RegionName,
			"officer_count": len(pv.Officers[c.CommuneCode])})
	}
	cols := make([]gin.H, 0, len(pv.ProvincialColumns))
	for _, c := range pv.ProvincialColumns {
		cols = append(cols, gin.H{"template_id": c.TemplateID, "form_code": c.FormCode, "letter": c.Letter, "title": c.Title, "unit": c.Unit})
	}
	noOff := pv.CommunesNoOfficer
	if noOff == nil {
		noOff = []string{}
	}
	warn := pv.Warnings
	if warn == nil {
		warn = []string{}
	}
	return gin.H{"report_type": pv.ReportType, "period_type": pv.PeriodType, "period_rule": pv.Rule.Tag(),
		"deadline": gin.H{"offset_days": pv.Deadline.OffsetDays, "time": pv.Deadline.Time}, "close_after_days": pv.CloseAfterDays,
		"forms": forms, "periods": periods, "communes": communes, "communes_without_officer": noOff, "provincial_columns": cols,
		"form_count": len(pv.Forms), "commune_count": len(pv.Communes), "period_count": len(pv.Periods), "task_count": pv.TaskCount, "warnings": warn}
}

func (h Handler) preview(c *gin.Context) {
	var req batchReq
	if !bind(c, &req) {
		return
	}
	in, p := req.input()
	if p != nil {
		httpx.Fail(c, p)
		return
	}
	pv, err := h.Svc.PreviewBatch(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, previewDTO(pv))
}

func (h Handler) issue(c *gin.Context) {
	var req batchReq
	if !bind(c, &req) {
		return
	}
	in, p := req.input()
	if p != nil {
		httpx.Fail(c, p)
		return
	}
	r, err := h.Svc.IssueBatch(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warn, noOff := r.Warnings, r.CommunesNoOfficer
	if warn == nil {
		warn = []string{}
	}
	if noOff == nil {
		noOff = []string{}
	}
	c.JSON(http.StatusCreated, gin.H{"batch": toBatch(r.Batch), "periods": toPeriods(r.Periods), "warnings": warn, "communes_without_officer": noOff})
}

func (h Handler) listBatches(c *gin.Context) {
	cur, ok := cursor(c)
	if !ok {
		return
	}
	bs, nx, err := h.Svc.ListBatches(c.Request.Context(), c.Query("province"), c.Query("report_type"), cur, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]batchDTO, 0, len(bs))
	for _, b := range bs {
		items = append(items, toBatch(b))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(nx)})
}

func (h Handler) getBatch(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetBatch(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch": toBatch(v.Batch), "periods": toPeriods(v.Periods)})
}

func (h Handler) batchLog(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	es, err := h.Svc.BatchLog(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toLog(es), "next_cursor": nil})
}

type reasonReq struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

func (h Handler) withdraw(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	b, err := h.Svc.WithdrawBatch(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toBatch(b))
}

type deadlineChangeReq struct {
	PeriodIDs      []string     `json:"period_ids"`
	DueAt          *time.Time   `json:"due_at"`
	Deadline       *deadlineReq `json:"deadline"`
	CloseAfterDays *int         `json:"close_after_days"`
	Reason         string       `json:"reason"`
}

func (h Handler) deadline(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	var req deadlineChangeReq
	if !bind(c, &req) {
		return
	}
	pids, p := uuids(req.PeriodIDs, "period_ids")
	if p != nil {
		httpx.Fail(c, p)
		return
	}
	in := app.DeadlineInput{PeriodIDs: pids, DueAt: req.DueAt, CloseAfterDays: req.CloseAfterDays, Reason: req.Reason}
	if req.Deadline != nil {
		in.Deadline = &domain.Deadline{OffsetDays: req.Deadline.OffsetDays, Time: req.Deadline.Time}
	}
	chs, err := h.Svc.ChangeDeadline(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(chs))
	for _, ch := range chs {
		items = append(items, gin.H{"period_id": ch.PeriodID, "label": ch.Label, "old_due_at": tss(ch.OldDue), "new_due_at": tss(ch.NewDue),
			"old_auto_close_at": tss(ch.OldClose), "new_auto_close_at": tss(ch.NewClose)})
	}
	c.JSON(http.StatusOK, gin.H{"changes": items})
}

func (h Handler) provincial(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	tpl, ok := optUUID(c, "template_id")
	if !ok {
		return
	}
	vs, err := h.Svc.ProvincialValuesOf(c.Request.Context(), id, tpl)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := []gin.H{}
	for t, m := range vs {
		for commune, cells := range m {
			items = append(items, gin.H{"template_id": t, "commune_code": commune, "cells": cells})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) status(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	pid, ok := optUUID(c, "period_id")
	if !ok {
		return
	}
	tpl, ok := optUUID(c, "template_id")
	if !ok {
		return
	}
	b, err := h.Svc.Status(c.Request.Context(), id, pid, tpl, c.Query("status"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(b.Rows))
	for _, r := range b.Rows {
		officers := make([]gin.H, 0, len(r.Task.Assignees))
		for _, o := range r.Task.Assignees {
			officers = append(officers, gin.H{"actor_ref": o, "display": b.Names[o]})
		}
		rows = append(rows, gin.H{"task_id": r.Task.ID, "commune_code": r.Task.CommuneCode, "region_code": r.Task.RegionCode, "form_code": r.Task.FormCode,
			"template_id": r.Task.TemplateID, "status": r.Display, "status_label": domain.StatusLabel[r.Display], "officers": officers,
			"no_officer": r.NoOfficer, "submitted_at": tsp(r.Task.SubmittedAt), "last_reminded_at": tsp(r.Task.LastRemindedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"period": toPeriod(b.Period), "counts": b.Counts, "items": rows})
}

type remindReq struct {
	PeriodID   string   `json:"period_id"`
	TemplateID *string  `json:"template_id"`
	Communes   []string `json:"communes"`
	AllPending bool     `json:"all_pending"`
	Message    string   `json:"message"`
}

func (h Handler) remind(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	var req remindReq
	if !bind(c, &req) {
		return
	}
	pid, err := uuid.Parse(req.PeriodID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "phải là UUID"))
		return
	}
	in := app.RemindInput{PeriodID: pid, Communes: req.Communes, AllPending: req.AllPending, Message: req.Message}
	if req.TemplateID != nil && *req.TemplateID != "" {
		t, err := uuid.Parse(*req.TemplateID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("template_id", "phải là UUID"))
			return
		}
		in.TemplateID = &t
	}
	r, err := h.Svc.Remind(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	no, done := r.SkippedNoOfficer, r.SkippedDone
	if no == nil {
		no = []string{}
	}
	if done == nil {
		done = []string{}
	}
	c.JSON(http.StatusCreated, gin.H{"reminder": toReminder(r.Reminder, nil), "skipped_no_officer": no, "skipped_done": done})
}

func (h Handler) reminders(c *gin.Context) {
	id, ok := pathID(c, app.ErrBatchNotFound)
	if !ok {
		return
	}
	pid, ok := optUUID(c, "period_id")
	if !ok {
		return
	}
	rs, names, err := h.Svc.RemindersOf(c.Request.Context(), id, pid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]reminderDTO, 0, len(rs))
	for _, r := range rs {
		items = append(items, toReminder(r, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Công việc, bài nộp, rà soát (FN-XA-03.01/03.02, FN-TI-02.04)
// ---------------------------------------------------------------------------

func (h Handler) taskQuery(c *gin.Context) (app.TaskQuery, bool) {
	bid, ok := optUUID(c, "batch_id")
	if !ok {
		return app.TaskQuery{}, false
	}
	pid, ok := optUUID(c, "period_id")
	if !ok {
		return app.TaskQuery{}, false
	}
	return app.TaskQuery{Commune: c.Query("commune"), PeriodType: c.Query("period_type"), Status: c.Query("status"), FormCode: c.Query("form_code"),
		BatchID: bid, PeriodID: pid, Limit: limit(c)}, true
}

func taskListDTO(l app.TaskList) gin.H {
	items := make([]taskDTO, 0, len(l.Items))
	for _, t := range l.Items {
		items = append(items, toTask(t))
	}
	var rem any
	if l.LastReminder != nil {
		rem = toReminder(*l.LastReminder, nil)
	}
	return gin.H{"items": items, "counts": gin.H{"todo": l.Todo, "overdue": l.Overdue, "adjustment_requested": l.Adjustment}, "last_reminder": rem}
}

func (h Handler) listTasks(c *gin.Context) {
	q, ok := h.taskQuery(c)
	if !ok {
		return
	}
	l, err := h.Svc.ListTasks(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, taskListDTO(l))
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
	var latest, submitted any
	if d.Latest != nil {
		latest = toSubmission(*d.Latest, nil)
	}
	if d.LatestSubmit != nil {
		submitted = toSubmission(*d.LatestSubmit, nil)
	}
	slots := d.OpenSlots
	if slots == nil {
		slots = []int{}
	}
	prov := d.Provincial
	if prov == nil {
		prov = map[string]string{}
	}
	carried := d.Carried
	if carried == nil {
		carried = map[string]map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"task": toTask(d.TaskView), "batch": toBatch(*d.Batch), "period": toPeriod(d.Period), "form_name": d.FormName,
		"layout": gin.H{"template_id": d.Task.TemplateID, "version_id": d.Task.VersionID, "period_labels": d.PeriodLabels, "season": d.Batch.SeasonLabel,
			"open_slots": slots},
		"provincial_values": prov, "carried": carried, "latest": latest, "latest_submitted": submitted})
}

type submissionReq struct {
	ID        *string       `json:"id"`
	Kind      string        `json:"kind"`
	Rows      []subRowInput `json:"rows"`
	EventTime *time.Time    `json:"event_time"`
	DeviceID  string        `json:"device_id"`
}

type subRowInput struct {
	Indicator string            `json:"indicator"`
	Input     map[string]string `json:"input"`
}

func (h Handler) submit(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	var req submissionReq
	if !bind(c, &req) {
		return
	}
	in := app.SubmissionInput{Kind: req.Kind, EventTime: req.EventTime, DeviceID: req.DeviceID}
	if req.ID != nil && *req.ID != "" {
		sid, err := uuid.Parse(*req.ID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("id", "phải là UUIDv7"))
			return
		}
		in.ID = &sid
	}
	for _, r := range req.Rows {
		in.Rows = append(in.Rows, store.SubRow{Indicator: r.Indicator, Input: r.Input})
	}
	r, err := h.Svc.Submit(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	st := http.StatusCreated
	if r.Replayed {
		st = http.StatusOK
	}
	c.JSON(st, gin.H{"submission": toSubmission(r.Submission, nil), "task": toTask(r.Task), "issues": issuesOf(r.Issues)})
}

func (h Handler) history(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	subs, names, err := h.Svc.History(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]submissionDTO, 0, len(subs))
	for _, s := range subs {
		items = append(items, toSubmission(s, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) taskLog(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	es, err := h.Svc.TaskLog(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toLog(es), "next_cursor": nil})
}

func (h Handler) adjust(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.RequestAdjustment(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": toTask(r.Task)})
}

func (h Handler) confirm(c *gin.Context) {
	id, ok := pathID(c, app.ErrTaskNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.Confirm(c.Request.Context(), id, req.Note, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": toTask(r.Task)})
}

// ---------------------------------------------------------------------------
// Kỳ (FN-TI-02.04)
// ---------------------------------------------------------------------------

func (h Handler) listPeriods(c *gin.Context) {
	bid, ok := optUUID(c, "batch_id")
	if !ok {
		return
	}
	ps, err := h.Svc.ListPeriods(c.Request.Context(), app.PeriodQuery{Province: c.Query("province"), ReportType: c.Query("report_type"), BatchID: bid, Limit: limit(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toPeriods(ps)})
}

func periodViewDTO(v app.PeriodView) gin.H {
	runs := make([]runDTO, 0, len(v.Runs))
	for _, r := range v.Runs {
		runs = append(runs, toRun(r, nil))
	}
	return gin.H{"period": toPeriod(v.Period), "batch": toBatch(v.Batch), "submitted": v.Submitted, "total": v.Total, "reopened": v.Reopened, "runs": runs}
}

func (h Handler) getPeriod(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetPeriod(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodViewDTO(v))
}

func (h Handler) periodLog(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	es, err := h.Svc.PeriodLog(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": toLog(es), "next_cursor": nil})
}

func (h Handler) closePeriod(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.ClosePeriod(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, periodViewDTO(v))
}

type reopenReq struct {
	Communes    []string  `json:"communes"`
	TemplateIDs []string  `json:"template_ids"`
	Reason      string    `json:"reason"`
	Until       time.Time `json:"until"`
}

func (h Handler) reopen(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodNotFound)
	if !ok {
		return
	}
	var req reopenReq
	if !bind(c, &req) {
		return
	}
	tpls, p := uuids(req.TemplateIDs, "template_ids")
	if p != nil {
		httpx.Fail(c, p)
		return
	}
	r, err := h.Svc.ReopenPeriod(c.Request.Context(), id, app.ReopenInput{Communes: req.Communes, TemplateIDs: tpls, Reason: req.Reason, Until: req.Until}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := periodViewDTO(r.Period)
	out["task_ids"], out["dispatch_changed"] = r.TaskIDs, r.DispatchChanged
	c.JSON(http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// Tổng hợp các xã (FN-TI-02.05, FN-TW-01.02)
// ---------------------------------------------------------------------------

func (h Handler) consolidation(c *gin.Context) {
	pid, err := uuid.Parse(c.Query("period_id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "bắt buộc, UUID"))
		return
	}
	tpl, ok := optUUID(c, "template_id")
	if !ok {
		return
	}
	run, ok := optUUID(c, "run_id")
	if !ok {
		return
	}
	v, err := h.Svc.GetConsolidation(c.Request.Context(), app.ConsolidationQuery{PeriodID: pid, TemplateID: tpl, FormCode: c.Query("form_code"), RunID: run,
		Region: c.Query("region")}, c.Query("compare") == "previous_year")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	marks := make([]gin.H, 0, len(v.Marks))
	for _, m := range v.Marks {
		marks = append(marks, gin.H{"row_key": m.RowKey, "letter": m.Letter, "old": m.Old, "kind": m.Kind, "changed_by": m.ChangedBy,
			"changed_by_display": nameOf(v.Names, m.ChangedBy), "submitted_at": tsp(m.SubmittedAt)})
	}
	missing := make([]gin.H, 0, len(v.Missing))
	for _, m := range v.Missing {
		missing = append(missing, gin.H{"commune_code": m.Commune, "commune_name": m.CommuneName, "region_code": m.Region, "region_name": m.RegionName})
	}
	rows, totals := v.Rows, v.Totals
	if rows == nil {
		rows = []app.SnapRow{}
	}
	if totals == nil {
		totals = []app.SnapTotal{}
	}
	pending := make([]gin.H, 0, len(v.Pending.Items))
	for _, it := range v.Pending.Items {
		pending = append(pending, pendingDTO(it))
	}
	var cmp any
	if v.Compare != nil {
		cmp = gin.H{"label": v.CompareLabel, "total": v.Compare}
	}
	label := "Tạm tính"
	if !v.Run.Provisional {
		label = "Đã chốt"
	}
	c.JSON(http.StatusOK, gin.H{"period": toPeriod(v.Period), "batch": toBatch(v.Batch), "form_name": v.FormName, "run": toRun(v.Run, v.Names),
		"status_label": label, "read_only": true, "rows": rows, "totals": totals, "missing": missing, "changed_cells": marks,
		"pending": gin.H{"changes": v.Pending.Changes, "communes": v.Pending.Communes, "items": pending}, "compare": cmp})
}

func nameOf(m map[uuid.UUID]string, id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return m[*id]
}

func pendingDTO(it app.PendingItem) gin.H {
	return gin.H{"commune_code": it.Commune, "form_code": it.FormCode, "task_id": it.TaskID, "submission_id": it.SubmissionID,
		"submitted_at": tss(it.SubmittedAt), "kind": it.Kind}
}

type consolidateReq struct {
	PeriodID    string   `json:"period_id"`
	TemplateIDs []string `json:"template_ids"`
}

func (h Handler) consolidate(c *gin.Context) {
	var req consolidateReq
	if !bind(c, &req) {
		return
	}
	pid, err := uuid.Parse(req.PeriodID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "bắt buộc, UUID"))
		return
	}
	tpls, p := uuids(req.TemplateIDs, "template_ids")
	if p != nil {
		httpx.Fail(c, p)
		return
	}
	r, err := h.Svc.Consolidate(c.Request.Context(), pid, tpls, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	runs := make([]runDTO, 0, len(r.Runs))
	for _, x := range r.Runs {
		runs = append(runs, toRun(x, nil))
	}
	up := r.UpToDate
	if up == nil {
		up = []string{}
	}
	st := http.StatusCreated
	msg := ""
	if len(r.Runs) == 0 {
		st, msg = http.StatusOK, "Dữ liệu đã mới nhất"
	}
	c.JSON(st, gin.H{"runs": runs, "up_to_date": up, "message": msg})
}

func (h Handler) pending(c *gin.Context) {
	pid, err := uuid.Parse(c.Query("period_id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "bắt buộc, UUID"))
		return
	}
	p, err := h.Svc.PendingChanges(c.Request.Context(), pid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, pendingDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{"changes": p.Changes, "communes": p.Communes, "items": items})
}

func (h Handler) runs(c *gin.Context) {
	pid, err := uuid.Parse(c.Query("period_id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "bắt buộc, UUID"))
		return
	}
	tpl, ok := optUUID(c, "template_id")
	if !ok {
		return
	}
	rs, names, err := h.Svc.RunsOf(c.Request.Context(), pid, tpl)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]runDTO, 0, len(rs))
	for _, r := range rs {
		items = append(items, toRun(r, names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) changes(c *gin.Context) {
	id, ok := pathID(c, app.ErrRunNotFound)
	if !ok {
		return
	}
	cur, ok := cursor(c)
	if !ok {
		return
	}
	lim := limit(c)
	if lim <= 0 || lim > 1000 {
		lim = 500
	}
	run, chs, names, err := h.Svc.RunChanges(c.Request.Context(), id, cur, lim+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var nx *uuid.UUID
	if len(chs) > lim {
		chs = chs[:lim]
		id := chs[len(chs)-1].ID
		nx = &id
	}
	items := make([]changeDTO, 0, len(chs))
	for _, ch := range chs {
		items = append(items, changeDTO{ID: ch.ID, CommuneCode: ch.Commune, RowKey: ch.RowKey, Letter: ch.Letter, Old: ch.Old, New: ch.New, Kind: ch.Kind,
			SubmissionID: ch.Submission, ChangedBy: ch.ChangedBy, ChangedByDisplay: nameOf(names, ch.ChangedBy), OnBehalfOf: ch.OnBehalfOf,
			SubmittedAt: tsp(ch.SubmittedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"run": toRun(run, names), "items": items, "next_cursor": next(nx)})
}

// ---------------------------------------------------------------------------
// Dashboard (FN-TI-02.01, FN-TW-01.01), Báo cáo của xã (FN-XA-03.07)
// ---------------------------------------------------------------------------

func series(ps []app.SeriesPoint) []gin.H {
	out := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		out = append(out, gin.H{"key": p.Key, "label": p.Label, "value": p.Value.String(), "plan": decs(p.Plan), "pct": decs(p.Pct)})
	}
	return out
}

func (h Handler) dashboard(c *gin.Context) {
	pid, ok := optUUID(c, "period_id")
	if !ok {
		return
	}
	bid, ok := optUUID(c, "batch_id")
	if !ok {
		return
	}
	d, err := h.Svc.KNDashboard(c.Request.Context(), app.DashboardQuery{Province: c.Query("province"), Region: c.Query("region"), PeriodID: pid, BatchID: bid})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var period, batch any
	if d.Period != nil {
		period = toPeriod(*d.Period)
	}
	if d.Batch != nil {
		batch = toBatch(*d.Batch)
	}
	pests := make([]gin.H, 0, len(d.PestsTop))
	for _, p := range d.PestsTop {
		pests = append(pests, gin.H{"indicator": p.Indicator, "area": p.Area.String(), "previous": p.Previous.String(), "delta": p.Delta.String()})
	}
	uns := make([]gin.H, 0, len(d.Unsubmitted))
	for _, u := range d.Unsubmitted {
		officers := make([]gin.H, 0, len(u.Officers))
		for _, o := range u.Officers {
			officers = append(officers, gin.H{"actor_ref": o, "display": d.Names[o]})
		}
		uns = append(uns, gin.H{"commune_code": u.Commune, "commune_name": u.Name, "region_code": u.Region, "pending_forms": u.PendingForms,
			"officers": officers, "no_officer": len(u.Officers) == 0})
	}
	by := make([]gin.H, 0, len(d.ByCommune))
	for _, x := range d.ByCommune {
		by = append(by, gin.H{"commune_code": x.Commune, "commune_name": x.Name, "sown_area": x.SownArea.String(), "sown_plan": x.SownPlan.String(),
			"pest_area": x.PestArea.String(), "submitted": x.Submitted})
	}
	alerts := make([]gin.H, 0, len(d.Alerts))
	for _, a := range d.Alerts {
		alerts = append(alerts, gin.H{"level": a.Level, "code": a.Code, "message": a.Message})
	}
	var periods []periodDTO
	if d.Periods != nil {
		periods = toPeriods(d.Periods)
	} else {
		periods = []periodDTO{}
	}
	c.JSON(http.StatusOK, gin.H{"province": d.Province, "region": d.Region, "period": period, "batch": batch, "periods": periods,
		"updated_at": tsp(d.UpdatedAt), "empty": d.Period == nil || d.UpdatedAt == nil,
		"cards": gin.H{
			"sown_area": metricDTO(d.SownArea), "sown_plan": metricDTO(d.SownPlan), "sown_pct": metricDTO(d.SownPct),
			"harvest_area": metricDTO(d.HarvestArea), "harvest_output": metricDTO(d.HarvestOut), "harvest_yield": metricDTO(d.HarvestYield),
			"veg_sown": metricDTO(d.VegSown), "veg_harvest_area": metricDTO(d.VegHarvest), "veg_output": metricDTO(d.VegOutput),
			"pest_area": metricDTO(d.PestArea), "pest_severe": metricDTO(d.PestSevere), "fruit_area": metricDTO(d.FruitArea),
			"submitted": gin.H{"communes_submitted": d.Submitted, "communes_total": d.Communes},
		},
		"sown_by_week": series(d.SownByWeek), "tra": series(d.Tra), "sown_by_region": series(d.SownByRegion), "pests_top": pests,
		"unsubmitted": uns, "by_commune": by, "alerts": alerts, "region_names": d.RegionNames, "versions": d.Versions, "read_only": true})
}

func (h Handler) communeReports(c *gin.Context) {
	q, ok := h.taskQuery(c)
	if !ok {
		return
	}
	l, err := h.Svc.ListTasks(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, taskListDTO(l))
}

func (h Handler) communeStats(c *gin.Context) {
	bid, ok := optUUID(c, "batch_id")
	if !ok {
		return
	}
	commune := c.Query("commune")
	if commune == "" {
		httpx.Fail(c, problem.Validation.WithField("commune", "bắt buộc"))
		return
	}
	st, err := h.Svc.CommuneStatsOf(c.Request.Context(), commune, bid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var batch any
	if st.Batch != nil {
		batch = toBatch(*st.Batch)
	}
	c.JSON(http.StatusOK, gin.H{"commune_code": st.Commune, "batch": batch, "sown_area": metricDTO(st.SownArea), "sown_plan": metricDTO(st.SownPlan),
		"sown_pct": metricDTO(st.SownPct), "pest_area": metricDTO(st.PestArea), "tasks_total": st.TotalTasks, "tasks_submitted": st.Submitted,
		"tasks_due": st.DueTasks, "tasks_on_time": st.OnTime})
}
