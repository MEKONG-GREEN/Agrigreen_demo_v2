// Package handler: HTTP của pf/reports (contracts/openapi/application.yaml và
// admin.yaml, mục "# --- pf/reports ---").
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/reports")
	g.GET("/templates", h.templates)
	g.POST("/exports", h.requestExport)
	g.GET("/exports/:id", h.export)
	g.GET("/exports/:id/download", h.download)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	g := rg.Group("/reports")
	g.GET("/templates", h.adminTemplates)
	g.POST("/templates", h.saveTemplate)
	g.GET("/templates/:key/versions", h.versions)
	g.POST("/templates/:key/retire", h.retire)
	g.GET("/schedules", h.schedules)
	g.POST("/schedules", h.createSchedule)
	g.POST("/schedules/:id/deactivate", h.deactivate)
	g.GET("/schedules/:id/runs", h.runs)
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

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

// ---------------------------------------------------------------------------
// Mẫu biểu
// ---------------------------------------------------------------------------

type templateDTO struct {
	Key       string            `json:"key"`
	Version   int               `json:"version"`
	Builtin   bool              `json:"builtin"`
	Status    string            `json:"status"`
	Def       domain.Definition `json:"definition"`
	Reason    *string           `json:"reason"`
	CreatedBy *string           `json:"created_by"`
	CreatedAt *string           `json:"created_at"`
}

func toTemplate(t domain.Template) templateDTO {
	d := templateDTO{Key: t.Key, Version: t.Version, Builtin: t.Builtin(), Status: t.Status, Def: t.Def, Reason: t.Reason}
	if !t.Builtin() {
		by, at := t.CreatedBy.String(), ts(t.CreatedAt)
		d.CreatedBy, d.CreatedAt = &by, &at
	}
	if d.Def.Columns == nil {
		d.Def.Columns = []domain.Column{}
	}
	return d
}

func templateList(c *gin.Context, ts []domain.Template, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]templateDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTemplate(t))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) templates(c *gin.Context) {
	ts, err := h.Svc.ListTemplates(c.Request.Context())
	templateList(c, ts, err)
}

func (h Handler) adminTemplates(c *gin.Context) {
	ts, err := h.Svc.AdminTemplates(c.Request.Context())
	templateList(c, ts, err)
}

func (h Handler) versions(c *gin.Context) {
	ts, err := h.Svc.TemplateVersions(c.Request.Context(), c.Param("key"))
	templateList(c, ts, err)
}

type saveTemplateReq struct {
	Key        string            `json:"key"`
	Definition domain.Definition `json:"definition"`
}

func (h Handler) saveTemplate(c *gin.Context) {
	var req saveTemplateReq
	if !bind(c, &req) {
		return
	}
	t, err := h.Svc.SaveTemplate(c.Request.Context(), req.Key, req.Definition)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toTemplate(t))
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) retire(c *gin.Context) {
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	t, err := h.Svc.RetireTemplate(c.Request.Context(), c.Param("key"), req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toTemplate(t))
}

// ---------------------------------------------------------------------------
// Việc xuất
// ---------------------------------------------------------------------------

type exportReq struct {
	TemplateKey string        `json:"template_key"`
	Format      string        `json:"format"`
	Params      domain.Params `json:"params"`
}

type exportDTO struct {
	ID              string        `json:"id"`
	TemplateKey     string        `json:"template_key"`
	TemplateVersion int           `json:"template_version"`
	Title           string        `json:"title"`
	Format          string        `json:"format"`
	Params          domain.Params `json:"params"`
	Status          string        `json:"status"`
	Trigger         string        `json:"trigger"`
	ScheduleID      *string       `json:"schedule_id"`
	RequestedBy     string        `json:"requested_by"`
	RequestedAt     string        `json:"requested_at"`
	RowCount        *int          `json:"row_count"`
	Provisional     *bool         `json:"provisional"`
	SizeBytes       *int64        `json:"size_bytes"`
	SHA256          *string       `json:"sha256"`
	ErrorCode       *string       `json:"error_code"`
	CompletedAt     *string       `json:"completed_at"`
}

func toExport(j domain.Job) exportDTO {
	d := exportDTO{
		ID: j.ID.String(), TemplateKey: j.TemplateKey, TemplateVersion: j.TemplateVersion, Title: j.Def.Title, Format: j.Format,
		Params: j.Params, Status: string(j.Status), Trigger: j.Trigger, RequestedBy: j.RequestedBy.String(), RequestedAt: ts(j.RequestedAt),
		RowCount: j.RowCount, Provisional: j.Provisional, SizeBytes: j.SizeBytes, SHA256: j.SHA256, ErrorCode: j.ErrorCode,
		CompletedAt: tsp(j.CompletedAt),
	}
	if j.ScheduleID != nil {
		s := j.ScheduleID.String()
		d.ScheduleID = &s
	}
	return d
}

func (h Handler) requestExport(c *gin.Context) {
	var req exportReq
	if !bind(c, &req) {
		return
	}
	j, _, err := h.Svc.RequestExport(c.Request.Context(), app.ExportInput{
		TemplateKey: req.TemplateKey, Format: req.Format, Params: req.Params, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/reports/exports/"+j.ID.String())
	c.JSON(http.StatusAccepted, toExport(j))
}

func (h Handler) export(c *gin.Context) {
	id, ok := pathID(c, app.ErrExportNotFound)
	if !ok {
		return
	}
	j, err := h.Svc.GetExport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toExport(j))
}

func (h Handler) download(c *gin.Context) {
	id, ok := pathID(c, app.ErrExportNotFound)
	if !ok {
		return
	}
	l, err := h.Svc.Download(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"export_id": l.Job.ID.String(), "url": l.URL, "expires_at": ts(l.ExpiresAt), "file_name": l.FileName,
		"content_type": domain.ContentType(l.Job.Format), "sha256": l.Job.SHA256,
	})
}

// ---------------------------------------------------------------------------
// Lịch gửi
// ---------------------------------------------------------------------------

type scheduleReq struct {
	TemplateKey string        `json:"template_key"`
	Format      string        `json:"format"`
	UnitCode    string        `json:"unit_code"`
	Params      domain.Params `json:"params"`
	Frequency   string        `json:"frequency"`
	Weekday     int           `json:"weekday"`
	DayOfMonth  int           `json:"day_of_month"`
	Hour        int           `json:"hour"`
	Recipients  []string      `json:"recipients"`
}

type scheduleDTO struct {
	ID                string        `json:"id"`
	TemplateKey       string        `json:"template_key"`
	Format            string        `json:"format"`
	UnitCode          *string       `json:"unit_code"`
	Params            domain.Params `json:"params"`
	Frequency         string        `json:"frequency"`
	Weekday           *int          `json:"weekday"`
	DayOfMonth        *int          `json:"day_of_month"`
	Hour              int           `json:"hour"`
	Recipients        []string      `json:"recipients"`
	OwnerRef          string        `json:"owner_ref"`
	Active            bool          `json:"active"`
	DeactivatedReason *string       `json:"deactivated_reason"`
	CreatedAt         string        `json:"created_at"`
	UpdatedAt         string        `json:"updated_at"`
}

func intp(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func toSchedule(s domain.Schedule) scheduleDTO {
	d := scheduleDTO{
		ID: s.ID.String(), TemplateKey: s.TemplateKey, Format: s.Format, Params: s.Params, Frequency: s.Frequency,
		Weekday: intp(s.Weekday), DayOfMonth: intp(s.DayOfMonth), Hour: s.Hour, OwnerRef: s.OwnerRef.String(), Active: s.Active,
		DeactivatedReason: s.DeactivatedReason, CreatedAt: ts(s.CreatedAt), UpdatedAt: ts(s.UpdatedAt), Recipients: []string{},
	}
	if s.UnitCode != "" {
		u := s.UnitCode
		d.UnitCode = &u
	}
	for _, r := range s.Recipients {
		d.Recipients = append(d.Recipients, r.String())
	}
	return d
}

func (h Handler) createSchedule(c *gin.Context) {
	var req scheduleReq
	if !bind(c, &req) {
		return
	}
	s := domain.Schedule{
		TemplateKey: req.TemplateKey, Format: req.Format, UnitCode: req.UnitCode, Params: req.Params, Frequency: req.Frequency,
		Weekday: req.Weekday, DayOfMonth: req.DayOfMonth, Hour: req.Hour,
	}
	for i, r := range req.Recipients {
		id, err := ids.ParseRef(r)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("recipients", "phần tử "+strconv.Itoa(i)+" phải là actor_ref (UUID)"))
			return
		}
		s.Recipients = append(s.Recipients, id)
	}
	out, replayed, err := h.Svc.CreateSchedule(c.Request.Context(), app.ScheduleInput{Schedule: s, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/reports/schedules/"+out.ID.String())
	c.JSON(status, toSchedule(out))
}

func (h Handler) schedules(c *gin.Context) {
	list, err := h.Svc.ListSchedules(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]scheduleDTO, 0, len(list))
	for _, s := range list {
		out = append(out, toSchedule(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) deactivate(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	s, err := h.Svc.DeactivateSchedule(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toSchedule(s))
}

func toRun(r store.Run) gin.H {
	var job any
	if r.JobID != nil {
		job = r.JobID.String()
	}
	return gin.H{
		"slot_key": r.SlotKey, "slot_at": ts(r.SlotAt), "status": r.Status, "skip_reason": r.SkipReason, "job_id": job,
		"fired_at": ts(r.FiredAt), "delivery_status": r.DeliveryStatus, "delivery_note": r.DeliveryNote, "sent_count": r.SentCount,
		"delivered_at": tsp(r.DeliveredAt),
	}
}

func (h Handler) runs(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	list, err := h.Svc.ScheduleRuns(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		out = append(out, toRun(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
