package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h Handler) audit(c *gin.Context) {
	known := map[string]bool{"from": true, "to": true, "actor_ref": true, "department": true, "module": true, "action": true, "limit": true, "cursor": true}
	bad := func(field string) {
		httpx.Fail(c, problem.Validation.WithField(field, "bộ lọc nhật ký không hợp lệ"))
	}
	for key, values := range c.Request.URL.Query() {
		if !known[key] || len(values) != 1 {
			bad(key)
			return
		}
	}
	from, err := time.Parse(time.RFC3339Nano, c.Query("from"))
	if err != nil {
		bad("from")
		return
	}
	to, err := time.Parse(time.RFC3339Nano, c.Query("to"))
	if err != nil {
		bad("to")
		return
	}
	in := app.AuditInput{From: from, To: to, Department: c.Query("department"), Module: c.Query("module"), Action: c.Query("action"), Cursor: c.Query("cursor")}
	if raw := c.Query("actor_ref"); raw != "" {
		in.ActorRef, err = uuid.Parse(raw)
		if err != nil || in.ActorRef == uuid.Nil {
			bad("actor_ref")
			return
		}
	}
	if raw := c.Query("limit"); raw != "" {
		in.Limit, err = strconv.Atoi(raw)
		if err != nil || in.Limit < 1 {
			bad("limit")
			return
		}
	}
	page, err := h.Svc.Audit(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}
