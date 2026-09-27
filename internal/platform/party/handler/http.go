// Package handler: HTTP của pf/party (contracts/openapi/application.yaml).
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// Register gắn route vào nhóm /v1 đã có xác thực và idempotency.
func (h Handler) Register(rg *gin.RouterGroup) {
	rg.POST("/consents", h.record)
	rg.GET("/consents", h.list)
	rg.GET("/consents/:id", h.get)
}

type recordReq struct {
	ID          *string `json:"id"`
	SubjectRef  string  `json:"subject_ref"`
	Purpose     string  `json:"purpose"`
	TextVersion string  `json:"text_version"`
	Status      string  `json:"status"`
	Channel     string  `json:"channel"`
	EventTime   string  `json:"event_time"`
	DeviceID    *string `json:"device_id"`
}

type consentDTO struct {
	ID          string  `json:"id"`
	SubjectRef  string  `json:"subject_ref"`
	Purpose     string  `json:"purpose"`
	TextVersion string  `json:"text_version"`
	Status      string  `json:"status"`
	Channel     string  `json:"channel"`
	EventTime   string  `json:"event_time"`
	RecordedAt  string  `json:"recorded_at"`
	Seq         int64   `json:"seq"`
	DeviceID    *string `json:"device_id,omitempty"`
}

func toDTO(c domain.Consent) consentDTO {
	return consentDTO{
		ID: c.ID.String(), SubjectRef: c.SubjectRef.String(), Purpose: c.Purpose, TextVersion: c.TextVersion,
		Status: string(c.Status), Channel: string(c.Channel), EventTime: c.EventTime.UTC().Format(time.RFC3339Nano),
		RecordedAt: c.RecordedAt.UTC().Format(time.RFC3339Nano), Seq: c.Seq, DeviceID: c.DeviceID,
	}
}

func (h Handler) record(c *gin.Context) {
	var req recordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	in := app.RecordInput{
		Purpose: req.Purpose, TextVersion: req.TextVersion, Status: domain.Status(req.Status),
		Channel: domain.Channel(req.Channel), DeviceID: req.DeviceID, IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	v := problem.Validation
	bad := false
	if req.ID != nil {
		id, err := ids.Parse(*req.ID)
		if err != nil {
			v, bad = v.WithField("id", "phải là UUIDv7"), true
		} else {
			in.ID = &id
		}
	}
	if s, err := ids.ParseRef(req.SubjectRef); err != nil {
		v, bad = v.WithField("subject_ref", "phải là UUID"), true
	} else {
		in.SubjectRef = s
	}
	if t, err := time.Parse(time.RFC3339Nano, req.EventTime); err != nil {
		v, bad = v.WithField("event_time", "phải theo RFC 3339 có múi giờ"), true
	} else {
		in.EventTime = t
	}
	if bad {
		httpx.Fail(c, v)
		return
	}
	out, replayed, err := h.Svc.Record(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/consents/"+out.ID.String())
	c.JSON(status, toDTO(out))
}

func (h Handler) get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return
	}
	out, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(out))
}

func (h Handler) list(c *gin.Context) {
	subject, err := ids.ParseRef(c.Query("subject_ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("subject_ref", "bắt buộc, phải là UUID"))
		return
	}
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		copy(after[:], b)
	}
	limit := 50
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	page, err := h.Svc.List(c.Request.Context(), subject, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]consentDTO, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, toDTO(it))
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if page.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString(page.Next[:])
	}
	c.JSON(http.StatusOK, resp)
}
