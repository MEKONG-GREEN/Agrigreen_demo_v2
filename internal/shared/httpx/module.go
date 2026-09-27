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
