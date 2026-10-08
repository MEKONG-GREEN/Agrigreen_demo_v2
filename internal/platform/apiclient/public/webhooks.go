package public

// Webhook mỏng (W5.7 — R-INT-07, R-INT-09; ADR-0033 §4): GET /v1/webhooks — đối tác xem webhook của CHÍNH mình,
// trạng thái ngắt mạch và cursor đã báo. Đăng ký / xoay bí mật / tắt / mở lại mạch ở Admin API (api-platform, MFA).
// Thân webhook chỉ là {feed, cursor, occurred_at} (contracts/openapi/public.yaml, mục webhooks).

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/webhook"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
)

func (s *Server) registerWebhooks(v1 *gin.RouterGroup) {
	v1.GET("/webhooks", s.handle(spec{kind: meter.CallMeta}, s.webhooks))
	v1.GET("/feeds/:feed/checksums", s.handle(spec{kind: meter.CallMeta}, s.checksums))
}

func (s *Server) webhooks(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	if s.d.Svc == nil {
		return nil, app.ErrDependencyUnavailable
	}
	svc := webhook.Service{DB: s.d.Svc.DB, Clock: s.d.Clock, Log: s.d.Log}
	vs, err := svc.ListForClient(c.Request.Context(), s.d.OperatorTenant, caller.Client.ID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		items = append(items, webhook.DTO(v))
	}
	r, err := jsonResult(http.StatusOK, map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	r.header.Set("Cache-Control", "no-store")
	return r, nil
}
