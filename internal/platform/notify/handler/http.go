// Package handler: HTTP của pf/notify (contracts/openapi/application.yaml và
// admin.yaml, mục "pf/notify").
package handler

import (
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (xác thực + idempotency).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/notifications", h.list)
	rg.GET("/notifications/unread-count", h.unread)
	rg.POST("/notifications/read-all", h.readAll)
	rg.POST("/notifications/:id/read", h.read)
	rg.POST("/devices/:id/push-token", h.pushToken)
	rg.POST("/sync/nudge", h.nudge)
}

// RegisterAdmin gắn route Admin API (MFA): đồng bộ ngay từ web quản trị; tình trạng kênh gửi.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/sync/nudge", h.nudge)
	rg.GET("/notify/delivery-health", h.deliveryHealth)
}

// deliveryHealth: tình trạng kênh email/đẩy của tenant (P0-NOTIFY-FAILCLOSED) — kênh tắt có cảnh báo.
func (h Handler) deliveryHealth(c *gin.Context) {
	res, err := h.Svc.DeliveryHealth(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, problem.PayloadTooLarge)
			return false
		}
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// notificationDTO là một thông báo trả cho client. Không có audience (chi tiết định tuyến nội bộ).
type notificationDTO struct {
	app.NotificationData
	Topic bool `json:"topic"`
}

func toDTO(n domain.Notification) notificationDTO {
	return notificationDTO{NotificationData: app.ToData(n), Topic: n.Topic()}
}

func (h Handler) list(c *gin.Context) {
	var before uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		copy(before[:], b)
	}
	limit := 0
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 200 {
			httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên 1–200"))
			return
		}
		limit = n
	}
	pg, err := h.Svc.List(c.Request.Context(), before, c.Query("unread") == "true", limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]notificationDTO, 0, len(pg.Items))
	for _, n := range pg.Items {
		items = append(items, toDTO(n))
	}
	var next any
	if pg.Next != nil {
		next = base64.RawURLEncoding.EncodeToString(pg.Next[:])
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) unread(c *gin.Context) {
	n, err := h.Svc.UnreadCount(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread": n})
}

func (h Handler) read(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotificationNotFound)
		return
	}
	n, err := h.Svc.MarkRead(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(n))
}

func (h Handler) readAll(c *gin.Context) {
	n, err := h.Svc.ReadAll(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"marked": n})
}

type pushTokenReq struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

type pushTokenDTO struct {
	DeviceID     string `json:"device_id"`
	Provider     string `json:"provider"`
	Platform     string `json:"platform"`
	Active       bool   `json:"active"`
	RegisteredAt string `json:"registered_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (h Handler) pushToken(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrDeviceUnregistered)
		return
	}
	var req pushTokenReq
	if !bindJSON(c, &req) {
		return
	}
	t, err := h.Svc.RegisterPushToken(c.Request.Context(), id, req.Provider, req.Token)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pushTokenDTO{DeviceID: t.DeviceID.String(), Provider: t.Provider, Platform: t.Platform,
		Active: t.DisabledAt == nil, RegisteredAt: t.RegisteredAt.UTC().Format(time.RFC3339), UpdatedAt: t.UpdatedAt.UTC().Format(time.RFC3339)})
}

type nudgeReq struct {
	DeviceID *string  `json:"device_id"`
	Unit     string   `json:"unit"`
	Entities []string `json:"entities"`
}

func (h Handler) nudge(c *gin.Context) {
	var req nudgeReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.NudgeInput{Unit: req.Unit, Entities: req.Entities}
	if req.DeviceID != nil {
		id, err := uuid.Parse(*req.DeviceID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("device_id", "phải là UUID"))
			return
		}
		in.DeviceID = &id
	}
	res, limited, err := h.Svc.Nudge(c.Request.Context(), in)
	if limited != nil {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(limited.RetryAfter.Seconds()))))
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"nudge_id": res.ID.String(), "target_kind": res.Kind, "target": res.Target, "devices": res.Devices,
		"entities": res.Entities})
}
