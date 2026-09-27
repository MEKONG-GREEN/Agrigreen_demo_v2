// Package authz: danh tính người gọi và phân quyền nghiệp vụ ở backend (R-55).
//
// Gateway chỉ làm TLS, định tuyến, giới hạn tần suất. Mọi kiểm quyền nằm ở
// đây và ở app/ của module — không ở UI.
package authz

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Vai dùng chung. Vai nghiệp vụ của module khai báo trong module.
const (
	RoleParamSetter   = "param_setter"   // R-56
	RoleParamApprover = "param_approver" // R-56
	RolePlatformAdmin = "platform_admin"
)

// Principal là người gọi đã xác thực.
type Principal struct {
	ActorRef uuid.UUID // = sub của token; mã tham chiếu, không phải định danh
	TenantID uuid.UUID // từ claim tenant_id của token (R-16)
	Roles    []string
	MFA      bool      // token có amr chứa yếu tố thứ hai
	AuthTime time.Time // thời điểm đăng nhập
}

// Has cho biết người gọi có vai r.
func (p Principal) Has(r string) bool { return slices.Contains(p.Roles, r) }

type ctxKey struct{}

// WithPrincipal gắn Principal vào context — chỉ middleware xác thực gọi.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext lấy Principal.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Require trả lỗi nếu người gọi không có ít nhất một trong các vai.
func Require(ctx context.Context, roles ...string) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, problem.Unauthenticated
	}
	for _, r := range roles {
		if p.Has(r) {
			return p, nil
		}
	}
	return p, problem.Forbidden
}

// ErrSelfApproval: người phê duyệt trùng người đặt.
var ErrSelfApproval = errors.New("authz: tự phê duyệt")

// EnsureDistinctApprover chặn một người phê duyệt thay đổi do chính mình đặt
// (R-56). Gọi ở app/, không ở UI.
func EnsureDistinctApprover(setter uuid.UUID, approver Principal) error {
	if !approver.Has(RoleParamApprover) {
		return problem.Forbidden.WithDetail("cần vai " + RoleParamApprover)
	}
	if setter == approver.ActorRef {
		return problem.SelfApproval.WithCause(ErrSelfApproval)
	}
	return nil
}
