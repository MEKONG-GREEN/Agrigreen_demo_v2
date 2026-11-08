package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

// CNTSWriteAuthorization runs after accounts resolves the current selected
// session and before the generic HTTP success cache, including on retries.
func (h Handler) CNTSWriteAuthorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		switch c.FullPath() {
		case "/v1/extension/cnts/form-designs", "/v1/extension/cnts/form-designs/:code/versions", "/v1/extension/cnts/period-series":
		default:
			c.Next()
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			httpx.Fail(c, problem.BadRequest)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		switch c.FullPath() {
		case "/v1/extension/cnts/form-designs":
			var req cntsDesignCreateReq
			if json.Unmarshal(body, &req) != nil {
				httpx.Fail(c, problem.BadRequest)
				return
			}
			code, ok := domain.SurveyCode(req.CodeSuffix)
			if !ok {
				httpx.Fail(c, problem.Validation.WithField("code_suffix", "mã biểu không hợp lệ"))
				return
			}
			err = h.Svc.CheckCNTSDefinition(c.Request.Context(), "create", code, app.CNTSDesignCreate{CodeSuffix: req.CodeSuffix, Title: req.Title, SourceCode: req.SourceCode, SourceVersion: req.SourceVersion, Fields: req.Fields}, c.GetHeader(idempotency.Header))
		case "/v1/extension/cnts/form-designs/:code/versions":
			var req cntsDesignSaveReq
			if json.Unmarshal(body, &req) != nil {
				httpx.Fail(c, problem.BadRequest)
				return
			}
			if req.BaseVersion == nil {
				httpx.Fail(c, problem.Validation.WithField("base_version", "bắt buộc — phiên bản đã đọc"))
				return
			}
			err = h.Svc.CheckCNTSDefinition(c.Request.Context(), "save", c.Param("code"), app.CNTSDesignSave{BaseVersion: *req.BaseVersion, Title: req.Title, Fields: req.Fields}, c.GetHeader(idempotency.Header))
		case "/v1/extension/cnts/period-series":
			var req cntsSeriesReq
			if json.Unmarshal(body, &req) != nil {
				httpx.Fail(c, problem.BadRequest)
				return
			}
			err = h.Svc.CheckCNTSPublication(c.Request.Context(), req.input(), c.GetHeader(idempotency.Header))
		}
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.Next()
	}
}
