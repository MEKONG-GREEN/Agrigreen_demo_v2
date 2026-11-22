package assembly

// W1.4 (R-MOD-08): cổng bật module theo đơn vị. Gốc lắp ráp biết route nào thuộc module nào vì chính nó
// gọi RegisterApp/RegisterAdmin của từng module: route của module per_tenant được đăng ký trong một nhóm
// con mang middleware của pf/party (ModuleGate) — chỉ route của module đó đi qua cổng, module luôn bật
// (lõi) không có middleware. Route công khai (/v1/public, không tenant) không qua cổng.

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// NamedModule là module kèm tên repo (module.yaml).
type NamedModule struct {
	Name   string
	Module httpx.Module
}

// gated bọc một module: route App/Admin qua cổng; middleware phiên và route công khai chuyển nguyên.
type gated struct {
	inner httpx.Module
	gate  gin.HandlerFunc
}

// gateExempt là module có route đã xác thực phải phục vụ cả khi module tắt cho tenant (KNSPLIT-2 #222: menu theo cấu
// hình tự lọc mục của module đã tắt, R-MOD-08 — trước khi chuyển sang chủ KN, menu ở pf/party luôn bật). Mẫu đường dẫn
// theo gin (FullPath, gồm tiền tố /v1).
type gateExempt interface{ GateExemptRoutes() []string }

// Gated trả m với route App/Admin đi qua gate (trừ route m khai miễn cổng); gate nil → m nguyên vẹn.
func Gated(m httpx.Module, gate gin.HandlerFunc) httpx.Module {
	if gate == nil {
		return m
	}
	if e, ok := m.(gateExempt); ok {
		exempt := map[string]bool{}
		for _, p := range e.GateExemptRoutes() {
			exempt[p] = true
		}
		inner := gate
		gate = func(c *gin.Context) {
			if exempt[c.FullPath()] {
				c.Next()
				return
			}
			inner(c)
		}
	}
	return gated{inner: m, gate: gate}
}

func (g gated) RegisterApp(rg *gin.RouterGroup)   { g.inner.RegisterApp(rg.Group("", g.gate)) }
func (g gated) RegisterAdmin(rg *gin.RouterGroup) { g.inner.RegisterAdmin(rg.Group("", g.gate)) }

// Middleware chuyển middleware phiên của module (httpx.RequestMiddleware) — chạy cho MỌI route, không qua cổng.
func (g gated) Middleware() gin.HandlerFunc {
	if rm, ok := g.inner.(httpx.RequestMiddleware); ok {
		return rm.Middleware()
	}
	return nil
}

// RegisterPublic chuyển route công khai (không tenant, không qua cổng).
func (g gated) RegisterPublic(rg *gin.RouterGroup) {
	if p, ok := g.inner.(publicRoutes); ok {
		p.RegisterPublic(rg)
	}
}
