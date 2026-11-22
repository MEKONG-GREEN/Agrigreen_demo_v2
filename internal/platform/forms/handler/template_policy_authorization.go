package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

// TemplateDefinitionAuthorization runs after session resolution and before the
// shared idempotency cache. Cached success must not bypass a newly denied owner
// policy. It preserves upstream account/session authorization scope bindings.
func (h Handler) TemplateDefinitionAuthorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		switch c.FullPath() {
		case "/v1/forms/templates":
			body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
			if err != nil || len(body) > 1<<20 {
				httpx.Fail(c, problem.BadRequest)
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var req defineReq
			if err := json.Unmarshal(body, &req); err != nil {
				httpx.Fail(c, problem.BadRequest)
				return
			}
			if err := h.Svc.AuthorizeTemplateDefinition(c.Request.Context(), app.TemplateInput{Profile: req.Profile, Code: req.Code, Title: req.Title, Fields: req.Fields, Fixture: req.Fixture}, req.BaseVersion); err != nil {
				httpx.Fail(c, err)
				return
			}
		case "/v1/forms/templates/fixtures":
			// A fixture batch checks every definition; neither a saved batch response nor
			// the first allowed fixture can grant access to another locked code.
			for _, f := range domain.Fixtures() {
				if err := h.Svc.AuthorizeTemplateDefinition(c.Request.Context(), app.TemplateInput{Profile: c.Query("profile"), Code: f.Code, Title: f.Title, Fields: f.Fields, Fixture: true}, nil); err != nil {
					httpx.Fail(c, err)
					return
				}
			}
		}
		c.Next()
	}
}
