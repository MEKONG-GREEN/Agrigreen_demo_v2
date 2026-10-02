package httpx

import "github.com/gin-gonic/gin"

// Module là một module nghiệp vụ gắn route vào hai bề mặt API (mục 11):
//
//   - RegisterApp: Application API (api.<domain>) — OIDC + JWT, nhóm /v1.
//   - RegisterAdmin: Admin API (admin.<domain>) — OIDC + MFA bắt buộc, nhóm /v1.
//     Dữ liệu chủ, tham số, phê duyệt, nhật ký.
//
// Cả hai nhóm đã có xác thực và Idempotency-Key cho POST; module chỉ kiểm vai.
type Module interface {
	RegisterApp(rg *gin.RouterGroup)
	RegisterAdmin(rg *gin.RouterGroup)
}

// RequestMiddleware (tùy chọn) là module cấp middleware chạy cho MỌI route của cả hai bề mặt, ngay sau
// xác thực và trước Idempotency-Key — vd ngữ cảnh phiên KN v7.7 (header X-AG-Context) của pf/accounts.
// Gốc lắp ráp gắn middleware trước khi module nào đăng ký route.
type RequestMiddleware interface {
	Middleware() gin.HandlerFunc
}

// Middlewares trả middleware của các module cài RequestMiddleware, theo thứ tự module.
func Middlewares(mods []Module) []gin.HandlerFunc {
	var out []gin.HandlerFunc
	for _, m := range mods {
		if rm, ok := m.(RequestMiddleware); ok {
			if h := rm.Middleware(); h != nil {
				out = append(out, h)
			}
		}
	}
	return out
}
