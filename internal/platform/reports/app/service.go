// Package app: use case của pf/reports — mẫu biểu, xuất báo cáo bất đồng bộ (mục
// 12), điểm kiểm soát xuất dữ liệu (mục 18), lịch gửi định kỳ (US-DASH-03).
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrTemplateNotFound = problem.New(http.StatusNotFound, "reports.template_not_found", "Không tìm thấy mẫu báo cáo")
	ErrTemplateRetired  = problem.New(http.StatusConflict, "reports.template_retired", "Mẫu báo cáo đã ngừng dùng")
	ErrFormat           = problem.New(http.StatusUnprocessableEntity, "reports.format_unsupported", "Mẫu này không hỗ trợ định dạng đã chọn")
	ErrExportNotFound   = problem.New(http.StatusNotFound, "reports.export_not_found", "Không tìm thấy việc xuất báo cáo")
	ErrExportNotReady   = problem.New(http.StatusConflict, "reports.export_not_ready", "Tệp báo cáo chưa sẵn sàng")
	ErrQuotaExceeded    = problem.New(http.StatusTooManyRequests, "reports.export_quota_exceeded", "Đã vượt hạn mức xuất báo cáo trong ngày")
	ErrOutOfScope       = problem.New(http.StatusForbidden, "reports.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrScheduleNotFound = problem.New(http.StatusNotFound, "reports.schedule_not_found", "Không tìm thấy lịch gửi báo cáo")
	ErrUnitInvalid      = problem.New(http.StatusUnprocessableEntity, "reports.unit_invalid", "Mã đơn vị hành chính không có hiệu lực")
	ErrDependency       = problem.New(http.StatusServiceUnavailable, "reports.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// Mặc định của tham số DLP khi chưa đặt ở pf/masterdata.
var (
	DefaultDailyQuota       = decimal.NewFromInt(50)
	DefaultAnomalyThreshold = decimal.NewFromInt(30)
)

// DownloadTTL là thời hạn URL tải tệp (≤ objstore.MaxPresignTTL).
const DownloadTTL = 10 * time.Minute

// Nhóm vai.
var (
	knRoles       = []string{extport.RoleKNOfficer, extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin, authz.RolePlatformAdmin}
	cghRoles      = []string{mechport.RoleCGHViewer, mechport.RoleCGHAdmin, authz.RolePlatformAdmin}
	allReaders    = []string{extport.RoleKNOfficer, extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin, mechport.RoleCGHViewer, mechport.RoleCGHAdmin, port.RoleReportsAdmin, port.RoleMasterdataAdmin, authz.RolePlatformAdmin}
	templateAdmin = []string{port.RoleReportsAdmin, port.RoleMasterdataAdmin, authz.RolePlatformAdmin}
	// Lập lịch: cán bộ quản lý (TTKN QG/tỉnh — US-DASH-03) và Admin CGH.
	schedulers = []string{extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin, mechport.RoleCGHAdmin, authz.RolePlatformAdmin}
)

// rolesFor trả vai được xuất từ một nguồn dữ liệu.
func rolesFor(source string) []string {
	if domain.Sources[source].Module == "mechanization" {
		return cghRoles
	}
	return knRoles
}

func hasAny(p authz.Principal, roles []string) bool {
	return slices.ContainsFunc(roles, p.Has)
}

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai reports_app
	Clock clock.Clock
	Log   *slog.Logger

	Scope    partyport.ScopeResolver
	Orgs     partyport.Orgs
	Units    mdport.AdminUnits
	Params   mdport.Params
	Catalogs mdport.Catalogs
	Store    objstore.Store

	Extension     extport.ReportData
	Mechanization mechport.ReportData

	Directory port.MailDirectory // nil → bỏ gửi email kèm cảnh báo
	Mailer    port.Mailer        // nil → bỏ gửi email kèm cảnh báo
	LinkBase  string             // URL gốc web-ops: <LinkBase>/reports/exports/<id>
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// scope lấy phạm vi của người gọi (platform_admin → toàn quốc). Thiếu resolver → 503.
func (s *Service) scope(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
	}
	return s.Scope.Scope(ctx)
}

// paramInt đọc tham số số nguyên đã duyệt tại at; chưa có → mặc định.
func (s *Service) paramInt(ctx context.Context, key string, at time.Time, def decimal.Decimal) (int, error) {
	v := def
	if s.Params != nil {
		pv, err := s.Params.At(ctx, key, at)
		switch {
		case errors.Is(err, mdport.ErrParamNotFound):
		case err != nil:
			return 0, err
		default:
			v = pv.Value
		}
	}
	return int(v.IntPart()), nil
}

// unitLevel trả cấp của đơn vị (tỉnh/xã) hiện hành.
func (s *Service) unitLevel(ctx context.Context, code string) (mdport.AdminUnit, error) {
	if s.Units == nil {
		return mdport.AdminUnit{}, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, mdport.ErrUnitNotFound) {
		return u, ErrUnitInvalid.WithField("unit_code", "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	return u, err
}

// fieldProblem ánh xạ domain.FieldError sang 422.
func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Msg)
	}
	return err
}
