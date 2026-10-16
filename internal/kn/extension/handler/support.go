package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerSupport gắn route yêu cầu hỗ trợ của nông dân (HTX US-SUPPORT-01).
func (h Handler) registerSupport(rg *gin.RouterGroup) {
	rg.POST("/support-requests", h.submitSupport)
	rg.GET("/support-requests", h.listSupport)
}

type supportReq struct {
	ID *string `json:"id"` // UUIDv7 sinh trên máy (tùy chọn)
	app.SupportPayload
}

func (h Handler) submitSupport(c *gin.Context) {
	var req supportReq
	if !bind(c, &req) {
		return
	}
	in, err := req.ToInput()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.ID != nil && *req.ID != "" {
		id, err := uuid.Parse(*req.ID)
		if err != nil || id.Version() != 7 {
			httpx.Fail(c, problem.Validation.WithField("id", "phải là UUIDv7"))
			return
		}
		in.ID = &id
	}
	in.IdemKey = idem(c)
	res, err := h.Svc.SubmitSupport(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"support_request": json.RawMessage(app.SupportData(res.Request)), "replayed": res.Replayed})
}

func (h Handler) listSupport(c *gin.Context) {
	var before uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		u, perr := uuid.ParseBytes(b)
		if err != nil || perr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		before = u
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.ListMySupport(c.Request.Context(), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]json.RawMessage, 0, len(pg.Items))
	for _, r := range pg.Items {
		items = append(items, app.SupportData(r))
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if pg.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(pg.Next.String()))
	}
	c.JSON(http.StatusOK, resp)
}
