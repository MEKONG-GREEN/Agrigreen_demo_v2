// Package app: use case của pf/reports — mẫu biểu, xuất báo cáo bất đồng bộ (mục
// 12), điểm kiểm soát xuất dữ liệu (mục 18), lịch gửi định kỳ (US-DASH-03).
package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/extensionsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/mechanizationsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
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

// Native capability roles. Application roles are registered through Policy.
var (
	cghRoles      = []string{mechport.RoleCGHViewer, mechport.RoleCGHAdmin, authz.RolePlatformAdmin}
	templateAdmin = []string{port.RoleReportsAdmin, port.RoleMasterdataAdmin, authz.RolePlatformAdmin}
)

func hasAny(p authz.Principal, roles []string) bool {
	return slices.ContainsFunc(roles, p.Has)
}

// Service là use case của module.
type Service struct {
	Builders       *SourceBuilderRegistry
	Catalog        *CatalogRegistry
	Policies       *PolicyRegistry
	ResolveSession port.SessionResolver
	DB             tenant.Beginner // pool vai reports_app
	Clock          clock.Clock
	Log            *slog.Logger

	Scope    partyport.ScopeResolver
	Orgs     partyport.Orgs
	Units    partyport.AdminUnits
	Params   params.Params
	Catalogs mdport.Catalogs
	Store    objstore.Store

	Extension     extensionsrc.ReportData
	Mechanization mechanizationsrc.ReportData
	CropCycle     cropcyclesrc.ReportData // HTX US-DASH-02: sản lượng HTX

	Directory  accountsport.MailDirectory      // nil → bỏ gửi email kèm cảnh báo
	Recipients accountsport.RecipientDirectory // nil → màn lập lịch của quản lý HTX trả 503
	Mailer     port.Mailer                     // nil → bỏ gửi email kèm cảnh báo
	LinkBase   string                          // URL gốc web-ops: <LinkBase>/reports/exports/<id>
	// LinkDownloadBase (A4): URL gốc công khai của api-platform cho liên kết tải Chi cục
	// <LinkDownloadBase>/v1/public/report-links/<token> (AG_REPORT_LINK_BASE). Rỗng → gửi kiểu liên kết thất bại.
	LinkDownloadBase string

	// KN v7.7 P4 — báo cáo Chi cục (dispatch.go): lần tổng hợp của pf/extension, cờ + bộ chuyển Google Sheet (TẮT mặc định).
	Consolidations extensionsrc.Consolidations
	GSheet         dispatch.GSheetConfig
	GSheetPub      dispatch.GSheetPublisher
	// Egress (G5, ADR-0024 mục 3): điểm kiểm soát xuất DÙNG CHUNG (pf/metering, kênh report_export),
	// gọi NGAY SAU điểm kiểm soát cục bộ egress ở cả ba lối. Nil → hành vi cũ.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
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
		case errors.Is(err, params.ErrParamNotFound):
		case err != nil:
			return 0, err
		default:
			v = pv.Value
		}
	}
	return int(v.IntPart()), nil
}

// unitLevel trả cấp của đơn vị (tỉnh/xã) hiện hành.
func (s *Service) unitLevel(ctx context.Context, code string) (partyport.AdminUnit, error) {
	if s.Units == nil {
		return partyport.AdminUnit{}, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) {
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
