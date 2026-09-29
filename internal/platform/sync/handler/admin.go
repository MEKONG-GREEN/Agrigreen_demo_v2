package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RegisterAdmin gắn route Admin API (MFA bắt buộc).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/devices", h.listDevices)
	rg.POST("/devices/:id/revoke", h.revoke)
	rg.GET("/sync/conflicts", h.listConflicts)
	rg.GET("/sync/conflicts/:id", h.getConflict)
	rg.POST("/sync/conflicts/:id/resolve", h.resolve)
	rg.GET("/sync/integrations", h.integrations)
	rg.GET("/sync/integrations/log", h.integrationLog)
}

func (h Handler) listDevices(c *gin.Context) {
	var actor *uuid.UUID
	if s := c.Query("actor_ref"); s != "" {
		a, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("actor_ref", "phải là UUID"))
			return
		}
		actor = &a
	}
	after, ok := cursorParam(c)
	if !ok {
		return
	}
	pg, err := h.Svc.ListDevices(c.Request.Context(), actor, after, limitParam(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]deviceDTO, 0, len(pg.Items))
	for _, d := range pg.Items {
		items = append(items, toDeviceDTO(d, true))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) revoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrDeviceNotFound)
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.Svc.RevokeDevice(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDeviceDTO(d, true))
}

func (h Handler) listConflicts(c *gin.Context) {
	q := app.ConflictQuery{Status: c.Query("status"), OverdueOnly: c.Query("overdue") == "true", Limit: limitParam(c),
		Entity: c.Query("entity"), SLA: c.Query("sla")}
	if s := c.Query("owner_org_ref"); s != "" {
		o, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("owner_org_ref", "phải là UUID"))
			return
		}
		q.OwnerOrgRef = &o
	}
	after, ok := cursorParam(c)
	if !ok {
		return
	}
	q.After = after
	pg, err := h.Svc.ListConflicts(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]conflictDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toConflictDTO(it, pg.Now))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) getConflict(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrConflictNotFound)
		return
	}
	d, err := h.Svc.GetConflictDetail(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toConflictDetailDTO(d, h.Svc.Clock.Now()))
}

type resolveReq struct {
	Note             string  `json:"note"`
	ResolvedRecordID *string `json:"resolved_record_id"`
	// Resolution (tùy chọn, US-SYNC-03): keep_version | correction.
	Resolution string `json:"resolution"`
}

func (h Handler) resolve(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrConflictNotFound)
		return
	}
	var req resolveReq
	if !bindJSON(c, &req) {
		return
	}
	var rec *uuid.UUID
	if req.ResolvedRecordID != nil {
		r, err := ids.ParseRef(*req.ResolvedRecordID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("resolved_record_id", "phải là UUID"))
			return
		}
		rec = &r
	}
	cf, err := h.Svc.ResolveConflictAs(c.Request.Context(), id, req.Note, rec, req.Resolution)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toConflictDTO(cf, h.Svc.Clock.Now()))
}
