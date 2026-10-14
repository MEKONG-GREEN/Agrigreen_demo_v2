// Package assembly là gốc lắp ráp của api-platform, tách khỏi package main để cmd/api-platform,
// kiểm thử đầu–cuối (tests/e2e) và cmd/seed-synthetic dùng CHUNG một cách nối module —
// không còn bản sao có thể lệch nhau.
package assembly

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Deps là phụ thuộc của api-platform. Mỗi module có pool theo vai DB riêng (R-05)
// và được dựng bằng Build (modules.go).
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier
	// Ops là pool vai đăng nhập đơn vị (app_platform); idempotency chạy SET LOCAL ROLE r_ops — chỉ bảng vận hành schema shared (W2.6).
	Ops     *pgxpool.Pool
	Modules []httpx.Module
	// Pools liệt kê mọi pool để /readyz kiểm.
	Pools []*pgxpool.Pool
	// CORSOrigins là origin trình duyệt được gọi chéo (AG_CORS_ORIGINS, khớp chính xác).
	// Rỗng → không origin nào; app di động và server-to-server không bị ảnh hưởng.
	CORSOrigins []string
	// PartnerLegacy: header ngừng dùng của /v1/partner/* (ADR-0033 §7.2; PartnerLegacyFromEnv). 0 = không Sunset.
	PartnerLegacy PartnerLegacy
	// BuildSHA là commit của image (AG_BUILD_SHA); /healthz trả về để biết máy đang chạy bản nào. Rỗng → bỏ trường.
	BuildSHA string
}

// maxBody giới hạn thân request (tải tệp đi qua đường riêng có giới hạn riêng).
const maxBody = 1 << 20

// Giới hạn route công khai /v1/public theo IP (mỗi bản sao): người nhận Chi cục mở vài liên kết.
const (
	publicPerMinute = 30
	publicBurst     = 20
)

func health(d Deps) func(r *gin.Engine) {
	return func(r *gin.Engine) {
		r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, healthBody(d.BuildSHA)) })
		r.GET("/readyz", func(c *gin.Context) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			for _, p := range append([]*pgxpool.Pool{d.Ops}, d.Pools...) {
				if err := p.Ping(ctx); err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
					return
				}
			}
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
		})
	}
}

// healthBody: /healthz kèm commit của image (AG_BUILD_SHA, Dockerfile) khi có — để biết máy demo đang chạy bản nào.
// Chỉ nhận chuỗi hex 7–40 ký tự; giá trị khác (hoặc rỗng) thì bỏ trường, không phản chiếu nội dung biến môi trường.
func healthBody(sha string) gin.H {
	b := gin.H{"status": "ok"}
	if shaRe.MatchString(sha) {
		b["commit"] = sha
	}
	return b
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// NewRouter dựng Application API (api.<domain>). Public API tắt ở ĐTK 1 (mục 11).
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.Use(httpx.CORS(d.CORSOrigins))                                      // trước mọi route: preflight đi qua cả nhánh 404/405
	r.Use(partnerLegacyMiddleware(d.PartnerLegacy, otelPartnerCounter())) // trước xác thực: header có cả trên 401/403
	health(d)(r)
	// Route công khai (không đăng nhập, không middleware phiên): mã thông báo trong đường dẫn là quyền truy cập —
	// liên kết tải tệp Chi cục của pf/reports (quyết định A4). Đăng ký trước nhóm /v1 có xác thực.
	// Giới hạn thô theo IP: mỗi mã (kể cả mã đoán bừa) tốn một giao dịch DB.
	pub := r.Group("/v1/public", httpx.RateLimitByIP(clock.System{}, publicPerMinute, publicBurst))
	for _, m := range d.Modules {
		if p, ok := m.(publicRoutes); ok {
			p.RegisterPublic(pub)
		}
	}
	v1 := r.Group("/v1", chain(d, httpx.Authenticate(d.Verifier))...)
	for _, m := range d.Modules {
		m.RegisterApp(v1)
	}
	return r
}

// publicRoutes (tùy chọn) là module có route công khai trên Application API.
type publicRoutes interface {
	RegisterPublic(rg *gin.RouterGroup)
}

// chain: xác thực (+ MFA) → middleware của module (KN v7.7: ngữ cảnh phiên X-AG-Context của
// pf/accounts) → Idempotency-Key.
func chain(d Deps, auth ...gin.HandlerFunc) []gin.HandlerFunc {
	out := append(auth, httpx.Middlewares(d.Modules)...)
	return append(out, idempotency.Middleware(db.ForModule(d.Ops, db.Ops)))
}

// NewAdminRouter dựng Admin API (admin.<domain>): MFA bắt buộc cho mọi route (mục 11).
func NewAdminRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.Use(httpx.CORS(d.CORSOrigins)) // trước mọi route: preflight đi qua cả nhánh 404/405
	health(d)(r)
	v1 := r.Group("/v1", chain(d, httpx.Authenticate(d.Verifier), httpx.RequireMFA())...)
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
