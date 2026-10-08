package handler

// Webhook mỏng (W5.7 — R-INT-07, R-INT-09; ADR-0033 §4) trên Admin API (MFA): đăng ký, xoay bí mật, tắt/bật, đóng
// ngắt mạch thủ công, xem trạng thái ngắt mạch + cursor đã báo + lần giao gần nhất. Bí mật hiện MỘT lần
// (Cache-Control: no-store; bản lưu cho Idempotency-Key không có bí mật).

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/webhook"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Webhooks gói webhook.Service cho Admin API.
type Webhooks struct{ Svc *webhook.Service }

// Register gắn route webhook.
func (h Webhooks) Register(rg *gin.RouterGroup) {
	rg.GET("/api-clients/:id/webhooks", h.list)
	rg.POST("/api-clients/:id/webhooks", h.register)
	rg.POST("/api-clients/:id/webhooks/:webhook_id/rotate-secret", h.rotate)
	rg.POST("/api-clients/:id/webhooks/:webhook_id/disable", h.change("disable"))
	rg.POST("/api-clients/:id/webhooks/:webhook_id/enable", h.change("enable"))
	rg.POST("/api-clients/:id/webhooks/:webhook_id/reset-breaker", h.change("reset_breaker"))
}

func (h Webhooks) list(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	vs, err := h.Svc.List(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		items = append(items, webhook.DTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type registerWebhookReq struct {
	URL              string   `json:"url"`
	Feeds            []string `json:"feeds"`
	FailureThreshold int      `json:"failure_threshold"`
}

// writeSecret trả webhook + bí mật MỘT lần; bản lưu phát lại Idempotency-Key không có bí mật.
func writeSecret(c *gin.Context, status int, out webhook.Issued) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	body := webhook.DTO(webhook.View{Registration: out.Registration})
	if b, err := json.Marshal(body); err == nil {
		idempotency.SetStoredBody(c, b)
	}
	if out.Secret != "" {
		body["secret"] = out.Secret
	}
	c.Header("Location", "/v1/api-clients/"+out.ClientID.String()+"/webhooks")
	c.JSON(status, body)
}

func (h Webhooks) register(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	var r registerWebhookReq
	if !bind(c, &r) {
		return
	}
	out, err := h.Svc.Register(c.Request.Context(), id, webhook.RegisterInput{URL: r.URL, Feeds: r.Feeds, Threshold: r.FailureThreshold,
		IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeSecret(c, http.StatusCreated, out)
}

func (h Webhooks) rotate(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	wid, ok := pathID(c, "webhook_id", webhook.ErrNotFound)
	if !ok {
		return
	}
	out, err := h.Svc.RotateSecret(c.Request.Context(), id, wid, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeSecret(c, http.StatusOK, out)
}

func (h Webhooks) change(op string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, "id", app.ErrClientNotFound)
		if !ok {
			return
		}
		wid, ok := pathID(c, "webhook_id", webhook.ErrNotFound)
		if !ok {
			return
		}
		reason, ok := bindReason(c)
		if !ok {
			return
		}
		out, err := h.Svc.Change(c.Request.Context(), id, wid, op, reason, idempotency.KeyFromContext(c.Request.Context()))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, webhook.DTO(webhook.View{Registration: out}))
	}
}
