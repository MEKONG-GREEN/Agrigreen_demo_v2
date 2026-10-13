// Package handler: route /v1/extension/cnts/alerts…, /v1/extension/cnts/alert-config… (W11.43 — cảnh báo CNTS, vòng đời
// cảnh báo, cấu hình nhắc nhở theo biểu, ngưỡng). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi cần
// Idempotency-Key.
package handler

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntsalert/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/alerts", h.list)
	g.GET("/alerts/:ref", h.get)
	g.POST("/alerts/:ref/assign", h.assign)
	g.POST("/alerts/:ref/start", h.start)
	g.POST("/alerts/:ref/close", h.close)
	g.GET("/alert-assignees", h.assignees)
	g.GET("/alert-config", h.config)
	g.PUT("/alert-config/forms/:code", h.putForm)
	g.PUT("/alert-config/kinds/:kind", h.putKind)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func refsOut(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func actor(a app.Actor) gin.H { return gin.H{"ref": a.Ref.String(), "display": a.Display} }

func optActor(a *app.Actor) any {
	if a == nil {
		return nil
	}
	return actor(*a)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("ref", "phải là UUID"))
		return uuid.Nil, false
	}
	return ref, true
}

func kindOut(code string) gin.H {
	k, _ := domain.KindOf(code)
	return gin.H{"code": k.Code, "no": k.No, "label": k.Label, "default_level": k.Level, "level_configurable": k.LevelConfigurable,
		"recipients": k.Recipients, "source": k.Source}
}

func alertOut(v app.AlertView) gin.H {
	a := v.Alert
	detail := a.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	k, _ := domain.KindOf(a.Kind)
	return gin.H{
		"alert_ref": a.Ref.String(), "kind": a.Kind, "kind_label": k.Label, "kind_no": k.No, "level": a.Level, "status": a.Status,
		"overdue": v.Overdue, "commune_code": optStr(a.CommuneCode), "province_code": optStr(a.ProvinceCode), "subject_kind": a.SubjectKind,
		"subject_ref": a.SubjectRef, "template_code": optStr(a.TemplateCode), "period_key": optStr(a.PeriodKey), "period_ref": optRef(a.PeriodRef),
		"source_ref": optRef(a.SourceRef), "title": a.Title, "detail": detail, "recipient_refs": refsOut(a.RecipientRefs),
		"assignee": optActor(v.Assignee), "raised_at": ts(a.RaisedAt), "due_at": ts(a.DueAt), "overdue_at": optTS(a.OverdueAt),
		"closed_at": optTS(a.ClosedAt), "auto_closed": a.AutoClosed, "result": optStr(a.Result), "seq": a.LastSeq, "updated_at": ts(a.UpdatedAt),
		"can_assign": v.CanAssign, "can_start": v.CanStart, "can_close": v.CanClose,
	}
}

func detailOut(d app.Detail) gin.H {
	out := alertOut(d.AlertView)
	hist := make([]gin.H, 0, len(d.History))
	for _, h := range d.History {
		hist = append(hist, gin.H{"seq": h.Seq, "action": h.Action, "status_after": h.StatusAfter, "actor": actor(h.Actor),
			"assignee": optActor(h.Assignee), "due_at": optTS(h.DueAt), "note": optStr(h.Note), "event_time": ts(h.EventTime),
			"support_reason": h.SupportReason})
	}
	out["history"] = hist
	return out
}

func (h Handler) list(c *gin.Context) {
	f := app.ListFilter{Status: c.Query("status"), Kind: c.Query("kind"), Level: c.Query("level"), Template: c.Query("template_code"),
		Mine: c.Query("mine") == "true"}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên"))
			return
		}
		f.Limit = n
	}
	vs, err := h.Svc.List(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	counts := gin.H{"open": 0, "urgent_open": 0, "overdue": 0}
	for _, v := range vs {
		items = append(items, alertOut(v))
		if v.Status != domain.StatusClosed {
			counts["open"] = counts["open"].(int) + 1
			if v.Level == domain.LevelUrgent {
				counts["urgent_open"] = counts["urgent_open"].(int) + 1
			}
			if v.Overdue {
				counts["overdue"] = counts["overdue"].(int) + 1
			}
		}
	}
	c.JSON(200, gin.H{"items": items, "counts": counts})
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	d, err := h.Svc.Get(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(200, detailOut(d))
}

type actionBody struct {
	BaseSeq     int64      `json:"base_seq"`
	AssigneeRef string     `json:"assignee_ref"`
	DueAt       *time.Time `json:"due_at"`
	Note        string     `json:"note"`
	Result      string     `json:"result"`
}

func (h Handler) action(c *gin.Context, run func(ref uuid.UUID, in app.ActionInput) (app.Detail, error)) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var b actionBody
	if !bind(c, &b) {
		return
	}
	in := app.ActionInput{BaseSeq: b.BaseSeq, DueAt: b.DueAt, Note: b.Note}
	if b.Result != "" {
		in.Note = b.Result
	}
	if b.AssigneeRef != "" {
		u, err := uuid.Parse(b.AssigneeRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("assignee_ref", "phải là UUID"))
			return
		}
		in.Assignee = u
	}
	d, err := run(ref, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(200, detailOut(d))
}

func (h Handler) assign(c *gin.Context) {
	h.action(c, func(ref uuid.UUID, in app.ActionInput) (app.Detail, error) {
		return h.Svc.Assign(c.Request.Context(), ref, in, idem(c))
	})
}

func (h Handler) start(c *gin.Context) {
	h.action(c, func(ref uuid.UUID, in app.ActionInput) (app.Detail, error) {
		return h.Svc.Start(c.Request.Context(), ref, in, idem(c))
	})
}

func (h Handler) close(c *gin.Context) {
	h.action(c, func(ref uuid.UUID, in app.ActionInput) (app.Detail, error) {
		return h.Svc.Close(c.Request.Context(), ref, in, idem(c))
	})
}

func (h Handler) assignees(c *gin.Context) {
	os, err := h.Svc.Assignees(c.Request.Context(), c.Query("commune_code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(os))
	for _, o := range os {
		communes := o.Communes
		if communes == nil {
			communes = []string{}
		}
		items = append(items, gin.H{"actor": actor(o.Actor), "level": o.Level, "communes": communes, "manager": o.Manager})
	}
	c.JSON(200, gin.H{"items": items})
}

func reminderOut(r domain.FormReminder) gin.H {
	ch := r.Channels
	if ch == nil {
		ch = []string{}
	}
	return gin.H{"before_due_hours": r.BeforeDueHours, "on_overdue": r.OnOverdue, "repeat_daily": r.RepeatDaily, "repeat_at": r.RepeatAt,
		"late_streak": r.LateStreak, "channels": ch, "message": r.Message}
}

func configOut(v app.ConfigView) gin.H {
	forms := make([]gin.H, 0, len(v.Forms))
	for _, f := range v.Forms {
		forms = append(forms, gin.H{"code": f.Code, "label": f.Label, "reminder": reminderOut(f.Reminder), "custom": f.Custom, "version": f.Version,
			"updated_at": optTS(f.UpdatedAt), "updated_by": optActor(f.UpdatedBy), "sent_today": f.SentToday})
	}
	kinds := make([]gin.H, 0, len(v.Alerts))
	for _, a := range v.Alerts {
		kinds = append(kinds, gin.H{"kind": kindOut(a.Kind.Code), "sla_hours": a.Setting.SLAHours, "urgent": a.Setting.Urgent, "level": a.Level,
			"custom": a.Custom, "version": a.Version})
	}
	ths := make([]gin.H, 0, len(v.Thresholds))
	for _, t := range v.Thresholds {
		var val any
		if t.Value != nil {
			val = t.Value.String()
		}
		var ef any
		if t.EffectiveFrom != nil {
			ef = t.EffectiveFrom.Format(time.DateOnly)
		}
		ths = append(ths, gin.H{"key": t.Key, "label": t.Label, "unit": t.Unit, "value": val, "version": t.Version, "effective_from": ef})
	}
	return gin.H{"forms": forms, "kinds": kinds, "thresholds": ths}
}

func (h Handler) config(c *gin.Context) {
	v, err := h.Svc.Config(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(200, configOut(v))
}

func (h Handler) putForm(c *gin.Context) {
	var r domain.FormReminder
	if !bind(c, &r) {
		return
	}
	v, err := h.Svc.PutFormReminder(c.Request.Context(), c.Param("code"), r, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(200, configOut(v))
}

func (h Handler) putKind(c *gin.Context) {
	var a domain.AlertSetting
	if !bind(c, &a) {
		return
	}
	v, err := h.Svc.PutAlertSetting(c.Request.Context(), c.Param("kind"), a, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(200, configOut(v))
}
