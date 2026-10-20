// Package handler: route /v1/extension/cnts/campaigns… (W11.50 — Chiến dịch điều tra mở rộng CNTS, FN-21). Mọi route cần
// header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi cần Idempotency-Key.
package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts/campaigns")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.POST("/:id/draft", h.update)
	g.POST("/:id/publish", h.publish)
	g.POST("/:id/extend", h.extend)
	g.POST("/:id/close", h.close)
	g.POST("/:id/reopen", h.reopen)
	g.POST("/:id/remind", h.remind)
	g.GET("/:id/results", h.results)
	g.GET("/:id/results/xlsx", h.resultsXLSX)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("id", "phải là UUID"))
		return uuid.Nil, false
	}
	return id, true
}

type inputDTO struct {
	Name              string   `json:"name"`
	Goal              string   `json:"goal"`
	Field             string   `json:"field"`
	TemplateCode      string   `json:"template_code"`
	ScopeMode         string   `json:"scope_mode"`
	Regions           []string `json:"regions"`
	Communes          []string `json:"communes"`
	FromDate          string   `json:"from_date"`
	ToDate            string   `json:"to_date"`
	DueAt             string   `json:"due_at"`
	OwnerRef          string   `json:"owner_ref"`
	RemindBeforeHours int      `json:"remind_before_hours"`
}

func parseTime(field, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, problem.Validation.WithField(field, "thời điểm RFC 3339")
	}
	return t, nil
}

func (d inputDTO) input() (app.Input, error) {
	due, err := parseTime("due_at", d.DueAt)
	if err != nil {
		return app.Input{}, err
	}
	in := app.Input{Name: d.Name, Goal: d.Goal, Field: d.Field, TemplateCode: d.TemplateCode, ScopeMode: d.ScopeMode, Regions: d.Regions,
		Communes: d.Communes, From: d.FromDate, To: d.ToDate, DueAt: due, RemindBeforeHours: d.RemindBeforeHours}
	if d.OwnerRef != "" {
		o, err := uuid.Parse(d.OwnerRef)
		if err != nil {
			return app.Input{}, problem.Validation.WithField("owner_ref", "phải là UUID")
		}
		in.OwnerRef = &o
	}
	return in, nil
}

func strs(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (h Handler) view(c *gin.Context, x app.Campaign) gin.H {
	v := x.Version
	refs := []uuid.UUID{v.ActorRef}
	if v.OwnerRef != nil {
		refs = append(refs, *v.OwnerRef)
	}
	for _, e := range x.Events {
		refs = append(refs, e.ActorRef)
	}
	names := h.Svc.DisplayNames(c.Request.Context(), refs)
	out := gin.H{
		"id": v.CampaignID.String(), "version": v.Seq, "name": v.Name, "goal": v.Goal, "field": v.Field, "template_code": v.TemplateCode,
		"scope_mode": v.ScopeMode, "regions": strs(v.Regions), "communes": strs(v.Units), "tasks": len(v.Units),
		"from_date": v.From.Format(time.DateOnly), "to_date": v.To.Format(time.DateOnly), "due_at": ts(x.DueAt),
		"remind_before_hours": v.RemindBeforeHours, "state": x.State, "province_code": v.Province,
		"updated_by": gin.H{"ref": v.ActorRef.String(), "display": names[v.ActorRef]}, "updated_at": ts(v.RecordedAt),
		"owner": nil, "forms_period_id": nil, "period_key": nil, "template_version": nil, "progress": nil,
	}
	if v.OwnerRef != nil {
		out["owner"] = gin.H{"ref": v.OwnerRef.String(), "display": names[*v.OwnerRef]}
	}
	if p := x.Period; p != nil {
		out["forms_period_id"], out["period_key"], out["template_version"] = p.FormsPeriodRef.String(), p.PeriodKey, p.TemplateVersion
	}
	evs := make([]gin.H, 0, len(x.Events))
	for _, e := range x.Events {
		ev := gin.H{"action": e.Action, "at": ts(e.RecordedAt), "actor": gin.H{"ref": e.ActorRef.String(), "display": names[e.ActorRef]},
			"due_at": nil, "reason": e.Reason, "reminded": e.Reminded}
		if e.DueAt != nil {
			ev["due_at"] = ts(*e.DueAt)
		}
		evs = append(evs, ev)
	}
	out["events"] = evs
	if pr := x.Progress; pr != nil {
		regions := make([]gin.H, 0, len(pr.Regions))
		for _, r := range pr.Regions {
			regions = append(regions, gin.H{"code": r.Code, "name": r.Name, "total": r.Total, "submitted": r.Submitted, "pending": r.Pending,
				"overdue": r.Overdue, "missed": r.Missed})
		}
		units := make([]gin.H, 0, len(pr.Units))
		for _, u := range pr.Units {
			var last any
			if u.LastSubmittedAt != nil {
				last = ts(*u.LastSubmittedAt)
			}
			units = append(units, gin.H{"commune": u.Commune, "commune_name": u.CommuneName, "region_code": u.Region, "region_name": u.RegionName,
				"status": u.Status, "versions": u.Versions, "late": u.Late, "last_submitted_at": last})
		}
		out["progress"] = gin.H{"period_status": pr.PeriodStatus, "total": pr.Total, "submitted": pr.Submitted, "pending": pr.Pending,
			"overdue": pr.Overdue, "missed": pr.Missed, "returned": pr.Returned, "regions": regions, "units": units}
	}
	return out
}

func (h Handler) list(c *gin.Context) {
	xs, err := h.Svc.List(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		items = append(items, h.view(c, x))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

func (h Handler) create(c *gin.Context) {
	var d inputDTO
	if !bind(c, &d) {
		return
	}
	in, err := d.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	x, replayed, err := h.Svc.Create(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, h.view(c, x))
}

func (h Handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var d inputDTO
	if !bind(c, &d) {
		return
	}
	in, err := d.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	x, err := h.Svc.Update(c.Request.Context(), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

func (h Handler) publish(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, err := h.Svc.Publish(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

type dueDTO struct {
	DueAt  string `json:"due_at"`
	Reason string `json:"reason"`
}

func (h Handler) extend(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var d dueDTO
	if !bind(c, &d) {
		return
	}
	due, err := parseTime("due_at", d.DueAt)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	x, err := h.Svc.Extend(c.Request.Context(), id, due, d.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

func (h Handler) close(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, err := h.Svc.Close(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

func (h Handler) reopen(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var d dueDTO
	if !bind(c, &d) {
		return
	}
	due, err := parseTime("due_at", d.DueAt)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	x, err := h.Svc.Reopen(c.Request.Context(), id, due, d.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.view(c, x))
}

func (h Handler) remind(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	x, n, err := h.Svc.Remind(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaign": h.view(c, x), "reminded": n})
}

func (h Handler) results(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, err := h.Svc.Results(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	fields := make([]gin.H, 0, len(r.Fields))
	for _, f := range r.Fields {
		fields = append(fields, gin.H{"key": f.Key, "label": f.Label, "unit": f.Unit})
	}
	rows := make([]gin.H, 0, len(r.Rows))
	for _, x := range r.Rows {
		rows = append(rows, gin.H{"commune": x.Commune, "commune_name": x.CommuneName, "region_name": x.RegionName, "values": x.Values,
			"nil_report": x.NilReport})
	}
	c.JSON(http.StatusOK, gin.H{"campaign_id": r.Campaign.Version.CampaignID.String(), "basis": r.Basis, "fields": fields, "rows": rows,
		"total": r.Total, "units": r.Units, "reported": r.Reported, "separate_from_cumulative": true,
		"period_kind": domain.PeriodKind})
}

func (h Handler) resultsXLSX(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	b, name, err := h.Svc.ResultsXLSX(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+name+"\"; filename*=UTF-8''"+url.PathEscape(name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
}
