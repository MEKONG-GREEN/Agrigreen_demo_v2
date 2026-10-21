package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

// ExportAuthorization must be mounted after authentication/session resolution and
// before idempotency.Middleware. The assembly's RequestMiddleware chain does this.
func (h Handler) ExportAuthorization() gin.HandlerFunc {
	if !h.Svc.HasSourceJobPolicies() {
		return nil
	}
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.FullPath() != "/v1/reports/exports" {
			c.Next()
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			httpx.Fail(c, problem.BadRequest)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		var req exportReq
		if err = json.Unmarshal(body, &req); err != nil {
			httpx.Fail(c, problem.BadRequest)
			return
		}
		if err = h.Svc.CheckExportReplay(c.Request.Context(), app.ExportInput{
			TemplateKey: req.TemplateKey, Format: req.Format, Params: req.Params, SourceArgs: req.SourceArgs, IdemKey: c.GetHeader(idempotency.Header),
		}); err != nil {
			httpx.Fail(c, err)
			return
		}
		c.Next()
	}
}
