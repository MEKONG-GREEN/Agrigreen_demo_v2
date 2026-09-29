package handler

import (
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TenantHeader mang tenant của sự kiện job.requested (optimizer đọc từ phong bì).
const TenantHeader = "X-Tenant-ID"

// MaxResultBytes giới hạn phong bì kết quả (đường cong tồn kho 365 ngày × Hub + độ nhạy).
const MaxResultBytes = 8 << 20

// Authorizer quyết định request Internal API có đến từ dịch vụ được phép không.
type Authorizer func(r *http.Request) error

// MTLSAuthorizer chỉ nhận client có chứng thư đã xác minh (mTLS, mục 11) với
// CN hoặc DNS SAN thuộc danh sách (mặc định "optimizer"; AG_INTERNAL_CLIENT_NAMES).
func MTLSAuthorizer(allowed []string) Authorizer {
	return func(r *http.Request) error {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
			return app.ErrInternalUnauthorized
		}
		leaf := r.TLS.VerifiedChains[0][0]
		if slices.Contains(allowed, leaf.Subject.CommonName) {
			return nil
		}
		for _, n := range leaf.DNSNames {
			if slices.Contains(allowed, n) {
				return nil
			}
		}
		return app.ErrInternalUnauthorized
	}
}

// AllowedClientsFromEnv đọc AG_INTERNAL_CLIENT_NAMES (phân tách dấu phẩy), mặc định optimizer.
func AllowedClientsFromEnv() []string {
	v := strings.TrimSpace(os.Getenv("AG_INTERNAL_CLIENT_NAMES"))
	if v == "" {
		return []string{"optimizer"}
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Internal là Internal API cho optimizer (chỉ trong cụm, mTLS — mục 11).
type Internal struct {
	Svc       *app.Service
	Authorize Authorizer
}

// Register gắn route dưới nhóm /internal/v1.
func (h Internal) Register(rg *gin.RouterGroup) {
	g := rg.Group("/simulations", h.guard)
	g.GET("/:id/snapshot", h.snapshot)
	g.POST("/:id/result", h.result)
}

func (h Internal) guard(c *gin.Context) {
	authorize := h.Authorize
	if authorize == nil {
		authorize = MTLSAuthorizer(AllowedClientsFromEnv())
	}
	if err := authorize(c.Request); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Next()
}

func ids(c *gin.Context) (tenantID, simID uuid.UUID, ok bool) {
	tid, err := uuid.Parse(c.GetHeader(TenantHeader))
	if err != nil || tid == uuid.Nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("thiếu hoặc sai header "+TenantHeader))
		return uuid.Nil, uuid.Nil, false
	}
	sid, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrSimulationNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return tid, sid, true
}

// snapshot trả đúng byte ảnh chụp đã băm (optimizer kiểm sha256 với sự kiện).
func (h Internal) snapshot(c *gin.Context) {
	tid, sid, ok := ids(c)
	if !ok {
		return
	}
	raw, sha, err := h.Svc.SnapshotBytes(c.Request.Context(), tid, sid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("X-Content-SHA256", sha)
	c.Data(http.StatusOK, "application/json", raw)
}

// result nhận phong bì kết quả; nộp lặp → 200 với trạng thái đã lưu (idempotent).
func (h Internal) result(c *gin.Context) {
	tid, sid, ok := ids(c)
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxResultBytes+1))
	if err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("không đọc được thân request"))
		return
	}
	if len(raw) > MaxResultBytes {
		httpx.Fail(c, problem.PayloadTooLarge)
		return
	}
	out, err := h.Svc.AcceptResult(c.Request.Context(), tid, sid, raw)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if out.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"simulation_id": sid.String(), "status": out.Status, "replayed": out.Replayed})
}
