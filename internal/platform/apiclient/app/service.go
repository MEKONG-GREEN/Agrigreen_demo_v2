// Package app: use case của pf/apiclient (ADR-0022).
//
//   - Admin API (api-platform, MFA): khách hàng API, đăng ký sản phẩm + scope, gói hạn mức có phiên
//     bản — api_client_admin đề xuất, api_client_approver KHÁC duyệt (R-56, ở đây và CHECK DB); khóa
//     API phát hành / xoay (chồng lấn 7 ngày) / thu hồi, bí mật hiện MỘT lần.
//   - api-public: Authenticator (prefix + HMAC thời gian hằng), Plans (cho pf/metering), việc xuất
//     bất đồng bộ (mẫu mục 12) và worker dựng tệp.
//
// Mọi bản ghi thuộc tenant đơn vị vận hành (người gọi Admin API là tài khoản của tenant đó).
package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là producer của phong bì sự kiện.
const Producer = port.Producer

// Mã lỗi riêng của app (docs/errors.md, mục pf/apiclient) — bổ sung cho mã đóng băng ở port/.
var (
	ErrClientNotFound        = problem.New(http.StatusNotFound, "apiclient.client_not_found", "Không tìm thấy khách hàng API")
	ErrKeyNotFound           = problem.New(http.StatusNotFound, "apiclient.key_not_found", "Không tìm thấy khóa API")
	ErrSubscriptionNotFound  = problem.New(http.StatusNotFound, "apiclient.subscription_not_found", "Không tìm thấy đăng ký sản phẩm")
	ErrPlanNotFound          = problem.New(http.StatusNotFound, "apiclient.plan_not_found", "Không tìm thấy phiên bản gói hạn mức")
	ErrState                 = problem.New(http.StatusConflict, "apiclient.state", "Bản ghi không ở trạng thái cho phép thao tác này")
	ErrNotOperator           = problem.New(http.StatusForbidden, "apiclient.not_operator_tenant", "Chỉ tài khoản của đơn vị vận hành Public API được quản trị khách hàng API")
	ErrDependencyUnavailable = problem.New(http.StatusServiceUnavailable, "apiclient.dependency_unavailable",
		"Phụ thuộc của Public API chưa sẵn sàng (pepper khóa, nguồn sản phẩm, kho đối tượng)")
	ErrJobNotFound = problem.New(http.StatusNotFound, "apiclient.job_not_found", "Không tìm thấy việc xuất")
	ErrJobNotReady = problem.New(http.StatusConflict, "apiclient.job_not_ready", "Việc xuất chưa hoàn thành hoặc đã lỗi — chưa có tệp")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai apiclient_app
	Clock clock.Clock
	// Peppers: pepper HMAC theo phiên bản. Nil → phát hành/xoay khóa và xác thực trả 503.
	Peppers *domain.Peppers
	// Rand: nguồn ngẫu nhiên sinh khóa (nil → crypto/rand).
	Rand io.Reader
	// OperatorTenant: tenant đơn vị vận hành (AG_PUBLIC_OPERATOR_TENANT). Nil ở Admin API → nhận
	// tenant của người gọi; đặt → người gọi phải thuộc đúng tenant này.
	OperatorTenant uuid.UUID
	// Store: kho đối tượng trong lãnh thổ cho tệp xuất (worker ghi, api-public phát luồng).
	Store objstore.Store
	// Collection: nguồn collection_volume (pf/cropcycle) cho worker xuất.
	Collection port.CollectionVolumeSource
	// Aggregates: nguồn ô thô của tập tổng hợp (pf/cropcycle) cho job dựng feed (W5.3); Contributors: tenant
	// chủ dữ liệu đóng góp; Params: ngưỡng k (publicdata.k_min, tenant đơn vị vận hành).
	Aggregates   port.AggregateSource
	Contributors port.Contributors
	Params       params.Params
	Log          *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

// operator kiểm vai và trả tenant đơn vị vận hành của người gọi Admin API.
func (s *Service) operator(ctx context.Context, roles ...string) (authz.Principal, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return p, err
	}
	if s.OperatorTenant != uuid.Nil && p.TenantID != s.OperatorTenant {
		return p, ErrNotOperator
	}
	return p, nil
}

func reasonOK(r string) bool {
	n := len([]rune(r))
	return n >= 10 && n <= 500
}
