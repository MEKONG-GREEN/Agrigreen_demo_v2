package handler

// KN v7.7 P5 — lịch công tác tuần (contracts/openapi/application.yaml, "/v1/extension/work-schedules*").

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerSchedule(rg *gin.RouterGroup) {
	g := rg.Group("/extension/work-schedules")
	g.POST("", h.saveSchedule)
	g.GET("", h.listSchedules)
	g.GET("/mine", h.mySchedule)
	g.GET("/counts", h.scheduleCounts)
	g.GET("/unregistered", h.unregistered)
	g.GET("/settings", h.scheduleSettings)
	g.GET("/export", h.exportSchedules)
	g.POST("/copy-previous", h.copyPrevious)
	g.POST("/bulk-approve", h.bulkApprove)
	g.GET("/:id", h.getSchedule)
	g.GET("/:id/log", h.scheduleLog)
	g.POST("/:id/approve", h.approveSchedule)
	g.POST("/:id/request-changes", h.requestChanges)
	g.POST("/:id/items/:item_id/checkin", h.checkin) // W11.46 (CNTS CV-07)
}

// placeReq là địa điểm công tác trên bản đồ (W11.46, CV-06).
type placeReq struct {
	Lat    *float64 `json:"lat"`
	Lon    *float64 `json:"lon"`
	Source *string  `json:"source"`
}

type scheduleItemReq struct {
	ID             *string   `json:"id"`
	Day            string    `json:"day"`
	Session        string    `json:"session"`
	StartTime      *string   `json:"start_time"`
	EndTime        *string   `json:"end_time"`
	Content        string    `json:"content"`
	CommuneCode    *string   `json:"commune_code"`
	CooperativeRef *string   `json:"cooperative_ref"`
	Location       *string   `json:"location"`
	Participants   *string   `json:"participants"`
	Note           *string   `json:"note"`
	Place          *placeReq `json:"place"`
}

type scheduleSaveReq struct {
	ID          *string           `json:"id"`
	WeekStart   string            `json:"week_start"`
	Action      string            `json:"action"`
	BaseVersion *int              `json:"base_version"`
	Items       []scheduleItemReq `json:"items"`
	DeviceID    *string           `json:"device_id"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r scheduleSaveReq) toInput(key string) (app.ScheduleSaveInput, error) {
	v, bad := problem.Validation, false
	in := app.ScheduleSaveInput{Action: r.Action, BaseVersion: r.BaseVersion, DeviceID: r.DeviceID, IdemKey: key}
	if r.ID != nil && *r.ID != "" {
		id, err := ids.Parse(*r.ID)
		if err != nil {
			v, bad = v.WithField("id", "phải là UUIDv7"), true
		} else {
			in.ID = &id
		}
	}
	w, err := domain.ParseDate(r.WeekStart)
	if err != nil {
		v, bad = v.WithField("week_start", "định dạng YYYY-MM-DD"), true
	}
	in.WeekStart = w
	if len(r.Items) > domain.MaxScheduleItems {
		return in, v.WithField("items", domain.ErrTooManyItems.Error())
	}
	for i, it := range r.Items {
		f := "items[" + strconv.Itoa(i) + "]."
		item := domain.ScheduleItem{Session: it.Session, Start: deref(it.StartTime), End: deref(it.EndTime), Content: it.Content,
			CommuneCode: deref(it.CommuneCode), Location: deref(it.Location), Participants: deref(it.Participants), Note: deref(it.Note)}
		if it.ID != nil && *it.ID != "" {
			id, err := uuid.Parse(*it.ID)
			if err != nil {
				v, bad = v.WithField(f+"id", "phải là UUID"), true
			}
			item.ItemID = id
		}
		d, err := domain.ParseDate(it.Day)
		if err != nil {
			v, bad = v.WithField(f+"day", "định dạng YYYY-MM-DD"), true
		}
		item.Day = d
		item.CooperativeRef, v = optRef(v, &bad, f+"cooperative_ref", it.CooperativeRef)
		if p := it.Place; p != nil {
			if p.Lat == nil || p.Lon == nil {
				v, bad = v.WithField(f+"place", "cần lat và lon"), true
			} else {
				item.Place = &domain.GeoPoint{Lat: *p.Lat, Lon: *p.Lon, Source: deref(p.Source)}
			}
		}
		in.Items = append(in.Items, item)
	}
	if bad {
		return in, v
	}
	return in, nil
}

func (h Handler) saveSchedule(c *gin.Context) {
	var req scheduleSaveReq
	if !bind(c, &req) {
		return
	}
	in, err := req.toInput(idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.SaveSchedule(c.Request.Context(), in)
	h.writeDetail(c, d, err, http.StatusCreated)
}

func (h Handler) writeDetail(c *gin.Context, d app.ScheduleDetail, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := created
	if d.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/extension/work-schedules/"+d.Schedule.ID.String())
	if len(d.Checkins) > 0 {
		// Duyệt/lưu lại lịch sau check-in cũng không giữ tọa độ trong cache 48 giờ.
		metadata := d
		metadata.Checkins = maps.Clone(d.Checkins)
		for id, ch := range metadata.Checkins {
			ch.Point = nil
			metadata.Checkins[id] = ch
		}
		cached, err := json.Marshal(toScheduleDetail(metadata))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		idempotency.SetStoredBody(c, cached)
	}
	c.JSON(status, toScheduleDetail(d))
}

func (h Handler) getSchedule(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	version, _ := strconv.Atoi(c.Query("version"))
	d, err := h.Svc.GetSchedule(c.Request.Context(), id, version)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toScheduleDetail(d))
}

func (h Handler) listSchedules(c *gin.Context) {
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.ListSchedules(c.Request.Context(), app.ScheduleQuery{Week: c.Query("week_start"), Status: c.Query("status"),
		Commune: c.Query("commune_code"), OwnerLevel: c.Query("owner_level"), After: after, Limit: limitOf(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, d := range pg.Items {
		d.Names = pg.Names
		m := toSchedule(d)
		m["items"] = toItems(d.Items, d.Changed, d.Conflicts)
		addCheckins(m, d)
		items = append(items, m)
	}
	c.JSON(http.StatusOK, gin.H{"week_start": pg.Week.Format(time.DateOnly), "items": items, "next_cursor": nextUUID(pg.Next)})
}

func (h Handler) mySchedule(c *gin.Context) {
	m, err := h.Svc.GetMySchedule(c.Request.Context(), c.Query("week_start"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var sched any
	if m.Detail != nil {
		sched = toScheduleDetail(*m.Detail)
	}
	c.JSON(http.StatusOK, gin.H{
		"week_start": m.Week.Format(time.DateOnly), "week_end": m.Week.AddDate(0, 0, 6).Format(time.DateOnly),
		"submit_due_at": m.SubmitDue.UTC().Format(time.RFC3339), "review_due_at": m.ReviewDue.UTC().Format(time.RFC3339),
		"locked": m.Locked, "late_now": m.LateNow, "schedule": sched,
		"previous_week_start": m.PreviousWeek.Format(time.DateOnly), "previous_available": m.PreviousAvailable,
		"checkin_enabled": m.CheckinEnabled,
	})
}

func (h Handler) scheduleCounts(c *gin.Context) {
	n, err := h.Svc.CountSchedules(c.Request.Context(), c.Query("week_start"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"week_start": n.Week.Format(time.DateOnly), "draft": n.Draft, "pending": n.Pending,
		"approved": n.Approved, "changes_requested": n.ChangesRequested, "unregistered": n.Unregistered,
		"pending_open_weeks": n.PendingOpenWeeks})
}

func (h Handler) unregistered(c *gin.Context) {
	week, list, names, err := h.Svc.ListUnregistered(c.Request.Context(), c.Query("week_start"), c.Query("commune_code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, u := range list {
		var sid any
		if u.ScheduleID != uuid.Nil {
			sid = u.ScheduleID.String()
		}
		communes := u.Officer.Communes
		if communes == nil {
			communes = []string{}
		}
		items = append(items, gin.H{"actor_ref": u.Officer.ActorRef, "actor_display": nameOrNil(names, u.Officer.ActorRef),
			"owner_level": u.Officer.Level, "province_code": u.Officer.ProvinceCode, "commune_codes": communes,
			"status": u.Status, "schedule_id": sid})
	}
	c.JSON(http.StatusOK, gin.H{"week_start": week.Format(time.DateOnly), "items": items})
}

func (h Handler) scheduleSettings(c *gin.Context) {
	st, err := h.Svc.GetScheduleSettings(c.Request.Context(), c.Query("province_code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"province_code": st.ProvinceCode, "submit_due_offset_hours": st.SubmitDueOffset,
		"review_due_offset_hours": st.ReviewDueOffset, "remind_before_hours": st.RemindBefore,
		"approver_levels": st.ApproverLevels, "province_approval": st.ProvinceApproval, "provisional": st.Provisional,
		"department_code": st.Department, "approver_rule": st.ApproverRule, "checkin_enabled": st.Checkin,
		"checkin_far_m": st.CheckinFarM, "checkin_store_point": st.StorePoint, "checkin_point_retention_days": st.RetentionDays})
}

func (h Handler) exportSchedules(c *gin.Context) {
	f, err := h.Svc.ExportSchedules(c.Request.Context(), app.ScheduleQuery{Week: c.Query("week_start"), Status: c.Query("status"),
		Commune: c.Query("commune_code")}, c.Query("format"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(f.FileName))
	c.Data(http.StatusOK, f.ContentType, f.Body)
}

func (h Handler) copyPrevious(c *gin.Context) {
	var req struct {
		WeekStart string `json:"week_start"`
	}
	if !bind(c, &req) {
		return
	}
	d, err := h.Svc.CopyPreviousWeek(c.Request.Context(), req.WeekStart, idem(c))
	h.writeDetail(c, d, err, http.StatusCreated)
}

func (h Handler) bulkApprove(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !bind(c, &req) {
		return
	}
	list := make([]uuid.UUID, 0, len(req.IDs))
	for i, s := range req.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("ids["+strconv.Itoa(i)+"]", "phải là UUID"))
			return
		}
		list = append(list, id)
	}
	res, err := h.Svc.BulkApprove(c.Request.Context(), list, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	skipped := make([]gin.H, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		skipped = append(skipped, gin.H{"id": s.ID, "code": s.Code})
	}
	c.JSON(http.StatusOK, gin.H{"approved": res.Approved, "skipped": skipped})
}

type reviewReq struct {
	Version int    `json:"version"`
	Reason  string `json:"reason"`
	Note    string `json:"note"`
}

func (h Handler) approveSchedule(c *gin.Context) { h.review(c, port.ScheduleDecisionApproved) }
func (h Handler) requestChanges(c *gin.Context)  { h.review(c, port.ScheduleDecisionChangeRequested) }

func (h Handler) review(c *gin.Context, decision string) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	var req reviewReq
	if !bind(c, &req) {
		return
	}
	if req.Version < 1 {
		httpx.Fail(c, problem.Validation.WithField("version", "bắt buộc, ≥ 1"))
		return
	}
	d, err := h.Svc.ReviewSchedule(c.Request.Context(), app.ReviewInput{ID: id, Decision: decision, Version: req.Version,
		Reason: req.Reason, Note: req.Note, IdemKey: idem(c)})
	h.writeDetail(c, d, err, http.StatusOK)
}

func (h Handler) scheduleLog(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	var before int64
	if cur := c.Query("cursor"); cur != "" {
		n, err := strconv.ParseInt(cur, 10, 64)
		if err != nil || n < 1 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		before = n
	}
	page, next, names, err := h.Svc.ScheduleLog(c.Request.Context(), id, before, limitOf(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page))
	for _, le := range page {
		changes := make([]gin.H, 0, len(le.Changes))
		for _, ch := range le.Changes {
			changes = append(changes, gin.H{"field": ch.Field, "old": ch.Old, "new": ch.New})
		}
		var reason any
		if le.Reason != "" {
			reason = le.Reason
		}
		items = append(items, gin.H{"seq": le.Seq, "event": le.Event, "actor_ref": le.ActorRef,
			"actor_display": nameOrNil(names, le.ActorRef), "at": le.At.UTC().Format(time.RFC3339),
			"recorded_at": le.RecordedAt.UTC().Format(time.RFC3339), "changes": changes, "reason": reason})
	}
	var nx any
	if next != nil {
		nx = *next
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nx})
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

func nameOrNil(m map[uuid.UUID]string, ref uuid.UUID) any {
	if ref == uuid.Nil {
		return nil
	}
	if n, ok := m[ref]; ok && n != "" {
		return n
	}
	return nil
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func toSchedule(d app.ScheduleDetail) gin.H {
	sc := d.Schedule
	var approved, reviewer any
	if sc.ApprovedVersion > 0 {
		approved = sc.ApprovedVersion
	}
	if sc.ReviewerRef != uuid.Nil {
		reviewer = sc.ReviewerRef
	}
	communes := sc.Communes
	if communes == nil {
		communes = []string{}
	}
	return gin.H{
		"id": sc.ID, "owner_ref": sc.OwnerRef, "owner_display": nameOrNil(d.Names, sc.OwnerRef), "owner_level": sc.OwnerLevel,
		"department_code": sc.Department, "province_code": sc.ProvinceCode, "commune_codes": communes,
		"week_start": sc.WeekStart.Format(time.DateOnly), "week_end": sc.WeekStart.AddDate(0, 0, 6).Format(time.DateOnly),
		"status": sc.Status, "version": sc.Version, "approved_version": approved, "amendment": sc.Amendment, "late": sc.Late,
		"locked": d.Locked, "submit_due_at": d.SubmitDue.UTC().Format(time.RFC3339),
		"review_due_at": d.ReviewDue.UTC().Format(time.RFC3339), "review_overdue": d.ReviewOverdue,
		"submitted_at": timeOrNil(sc.SubmittedAt), "reviewed_at": timeOrNil(sc.ReviewedAt), "reviewer_ref": reviewer,
		"reviewer_display": nameOrNil(d.Names, sc.ReviewerRef), "last_reason": strOrNil(sc.LastReason),
		"item_count": sc.ItemCount, "conflict_count": len(d.Conflicts), "created_at": sc.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": sc.UpdatedAt.UTC().Format(time.RFC3339), "updated_by": sc.UpdatedBy,
	}
}

func toItems(items []domain.ScheduleItem, changed, conflicts map[uuid.UUID]bool) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		var coop any
		if it.CooperativeRef != uuid.Nil {
			coop = it.CooperativeRef
		}
		out = append(out, gin.H{
			"id": it.ItemID, "day": it.DayString(), "session": it.Session, "start_time": strOrNil(it.Start),
			"end_time": strOrNil(it.End), "content": it.Content, "commune_code": strOrNil(it.CommuneCode),
			"cooperative_ref": coop, "location": strOrNil(it.Location), "participants": strOrNil(it.Participants),
			"note": strOrNil(it.Note), "changed": changed[it.ItemID], "conflict": conflicts[it.ItemID],
			"place": placeDTO(it.Place),
		})
	}
	return out
}

func toScheduleDetail(d app.ScheduleDetail) gin.H {
	warnings := make([]gin.H, 0, len(d.Warnings))
	for _, w := range d.Warnings {
		idl := w.ItemIDs
		if idl == nil {
			idl = []uuid.UUID{}
		}
		warnings = append(warnings, gin.H{"code": w.Code, "message": w.Message, "item_ids": idl})
	}
	versions := make([]gin.H, 0, len(d.Versions))
	for _, v := range d.Versions {
		versions = append(versions, gin.H{"version": v.Version, "action": v.Action, "at": v.At.UTC().Format(time.RFC3339),
			"actor_ref": v.ActorRef, "actor_display": nameOrNil(d.Names, v.ActorRef), "item_count": v.ItemCount})
	}
	approvedConf := domain.Conflicts(d.ApprovedItems)
	out := gin.H{
		"schedule": toSchedule(d), "viewing_version": d.ViewingVersion,
		"items":          toItems(d.Items, d.Changed, d.Conflicts),
		"approved_items": toItems(d.ApprovedItems, nil, approvedConf),
		"warnings":       warnings, "versions": versions,
		"can": gin.H{"edit": d.Can.Edit, "submit": d.Can.Submit, "approve": d.Can.Approve, "request_changes": d.Can.RequestChanges},
	}
	addCheckins(out, d)
	return out
}
