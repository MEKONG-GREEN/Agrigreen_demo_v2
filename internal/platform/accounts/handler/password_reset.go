package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// Giới hạn theo IP riêng cho yêu cầu liên kết đặt lại (ngoài giới hạn chung của /v1/public): dồn 5, sau đó 1/phút.
// Trong bộ nhớ từng bản sao; giới hạn 5 yêu cầu/giờ/tài khoản nằm ở DB (ADR-0057).
const (
	resetPerMinute = 1
	resetBurst     = 5
)

// RegisterPublic gắn route CÔNG KHAI (không đăng nhập) của tự đặt lại mật khẩu (#255, BRD 2.0 FN-CH-00.01).
func (h Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.POST("/password-reset", httpx.RateLimitByIP(h.Svc.Clock, resetPerMinute, resetBurst), h.requestPasswordReset)
	rg.POST("/password-reset/confirm", h.confirmPasswordReset)
}

type passwordResetReq struct {
	Email string `json:"email" pii:"true"`
}

// requestPasswordReset luôn trả 202 cùng một thân cho email đúng cú pháp (không lộ email có tồn tại hay không).
func (h Handler) requestPasswordReset(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req passwordResetReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.Svc.RequestPasswordReset(c.Request.Context(), req.Email); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

type passwordResetConfirmReq struct {
	Token    string `json:"token"`
	Password string `json:"password" pii:"true"`
}

func (h Handler) confirmPasswordReset(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	var req passwordResetConfirmReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.Svc.ConfirmPasswordReset(c.Request.Context(), req.Token, req.Password); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
