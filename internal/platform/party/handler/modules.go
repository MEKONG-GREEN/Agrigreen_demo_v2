package handler

// HTTP bật module theo đơn vị (W1.4, R-MOD-08).
// Admin API (MFA): GET /v1/modules, POST /v1/modules/{zone}/{name}/enablement, GET /v1/modules/{zone}/{name}/log.
// Application API: GET /v1/me/modules (trạng thái bật của tenant người gọi — menu, ẩn màn).
// ModuleGate là middleware gốc lắp ráp gắn vào route của module per_tenant.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Modules gói app.Modules cho HTTP.
type Modules struct{ Svc *app.Modules }

// RegisterAdmin gắn route Admin API.
func (h Modules) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/modules", h.list(true))
	rg.POST("/modules/:zone/:name/enablement", h.set)
	rg.GET("/modules/:zone/:name/log", h.log)
}

// RegisterApp gắn route Application API.
func (h Modules) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/me/modules", h.list(false))
}

type moduleDTO struct {
	Module    string  `json:"module"`
	Kind      string  `json:"kind"`
	Enable    string  `json:"enable"`
	Enabled   bool    `json:"enabled"`
	Reason    *string `json:"reason"`
	UpdatedBy *string `json:"updated_by"`
	UpdatedAt *string `json:"updated_at"`
}

func toModuleDTO(v app.ModuleView) moduleDTO {
	out := moduleDTO{Module: v.Module, Kind: v.Kind, Enable: v.Enable, Enabled: v.Enabled, Reason: v.Reason}
	if v.UpdatedBy != nil {
		s := v.UpdatedBy.String()
		out.UpdatedBy = &s
	}
	if v.UpdatedAt != nil {
		s := v.UpdatedAt.UTC().Format(time.RFC3339Nano)
		out.UpdatedAt = &s
	}
	return out
}

func (h Modules) list(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		vs, err := h.Svc.List(c.Request.Context(), admin)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		items := make([]moduleDTO, 0, len(vs))
		for _, v := range vs {
			items = append(items, toModuleDTO(v))
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func moduleParam(c *gin.Context) string { return c.Param("zone") + "/" + c.Param("name") }

type moduleSetReq struct {
	Enabled *bool  `json:"enabled"`
	Reason  string `json:"reason"`
}

func (h Modules) set(c *gin.Context) {
	var req moduleSetReq
	if !bindJSON(c, &req) {
		return
	}
	if req.Enabled == nil {
		httpx.Fail(c, problem.Validation.WithField("enabled", "bắt buộc (true | false)"))
		return
	}
	v, replayed, err := h.Svc.SetEnabled(c.Request.Context(), moduleParam(c), *req.Enabled, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, toModuleDTO(v))
}

func (h Modules) log(c *gin.Context) {
	after, ok := afterSeq(c)
	if !ok {
		return
	}
	es, err := h.Svc.Log(c.Request.Context(), moduleParam(c), after, limitOf(c))
	writeLog(c, es, err)
}

// ModuleGate trả middleware chặn route của module khi module đã tắt cho tenant người gọi: 403
// module_not_enabled (RFC 9457, thân có "module"). Module luôn bật (enable: always) hoặc không có trong
// danh mục → nil (không gắn middleware: lõi không bao giờ bị chặn). Người gọi không có tenant đi tiếp
// (xác thực đã chặn trước đó).
func ModuleGate(en port.ModuleEnablement, module string) gin.HandlerFunc {
	if en == nil || !modreg.PerTenant(module) {
		return nil
	}
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		p, ok := authz.FromContext(ctx)
		if !ok || p.TenantID == uuid.Nil {
			c.Next()
			return
		}
		on, err := en.ModuleEnabled(ctx, p.TenantID, module)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if !on {
			httpx.Fail(c, problem.ModuleNotEnabled.With("module", module))
			return
		}
		c.Next()
	}
}
