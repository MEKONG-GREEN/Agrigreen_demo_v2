// Package app: use case của pf/metrics — ghi sổ giá trị chỉ số theo ngày (job
// metrics_snapshot, backfill), đọc lịch sử, nhật ký lớp GIS + ảnh chụp + phát lại.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md, mục pf/metrics).
var (
	ErrUnknownMetric = problem.New(http.StatusNotFound, "metrics.unknown_metric", "Không có định nghĩa chỉ số hiệu lực")
	ErrNotClosed     = problem.New(http.StatusUnprocessableEntity, "metrics.not_closed", "Ngày nghiệp vụ chưa kết thúc")
	ErrOutOfScope    = problem.New(http.StatusForbidden, "metrics.out_of_scope", "Phạm vi vượt quyền người gọi")
	ErrRangeTooLarge = problem.New(http.StatusUnprocessableEntity, "metrics.range_too_large", "Khoảng ngày quá dài")
	ErrUnknownLayer  = problem.New(http.StatusNotFound, "metrics.unknown_layer", "Không có lớp GIS này")
	ErrDefEdited     = problem.New(http.StatusConflict, "metrics.definition_edited", "Tệp định nghĩa của phiên bản đã dùng bị sửa — thêm phiên bản mới")
	ErrDependency    = problem.New(http.StatusServiceUnavailable, "metrics.dependency_unavailable", "Chưa nối CSDL hoặc nguồn chỉ số")
	ErrInvalidChange = problem.New(http.StatusUnprocessableEntity, "metrics.invalid_layer_change", "Dòng nhật ký lớp không hợp lệ")
	ErrFutureDate    = problem.New(http.StatusUnprocessableEntity, "metrics.future_date", "Ngày ở tương lai")
)

// MaxRangeDays là số ngày tối đa của một truy vấn lịch sử / backfill qua API.
const MaxRangeDays = 400

// Service là use case của module.
type Service struct {
	DB      tenant.Beginner // pool vai metrics_app; nil ở worker khi chỉ chạy consumer trong tx của inbox
	Clock   clock.Clock
	Catalog *domain.Catalog
	// Sources: nguồn theo module chủ dữ liệu (Definition.SourceModule, vd "pf/extension").
	Sources  map[string]port.Source
	Scope    partyport.ScopeResolver
	Geometry port.GeometryResolver
	Log      *slog.Logger
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return ErrDependency
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) readTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return ErrDependency
	}
	return tenant.WithTenantTxOptions(ctx, s.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

// baseViewers luôn xem được mọi chỉ số và nhật ký lớp (trong phạm vi).
var baseViewers = []string{port.RoleMetricsViewer, authz.RolePlatformAdmin}

// gisViewers xem nhật ký lớp, ảnh chụp, phát lại (GIS US-EVL-01, US-REPLAY-01).
var gisViewers = append(slices.Clone(baseViewers), "geo_admin", "plot_viewer", "plot_editor", "kn_national_admin", "kn_province_admin", "cgh_viewer")

func (s *Service) scope(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
	}
	return s.Scope.Scope(ctx)
}

func canView(p authz.Principal, d domain.Def) bool {
	for _, r := range append(slices.Clone(baseViewers), d.ViewerRoles...) {
		if p.Has(r) {
			return true
		}
	}
	return false
}
