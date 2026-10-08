// Package app: use case của pf/geo — ranh giới hành chính, kho/Hub và sức chứa, hub MKG qua van, đường bộ,
// log GPS, lớp bản đồ gộp và bản đồ chuẩn hóa. Mở giao dịch bằng tenant.WithTenantTx; kiểm vai (authz.Require)
// và phạm vi (party ScopeResolver). Thửa và ranh giới HTX ở pf/cooperative (W3.3): geo đọc qua
// port.PlotGeometry, port.MapLayers do gốc lắp ráp nối.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của pf/geo (docs/errors.md). Mã geo.* của thửa / ranh giới HTX khai ở pf/cooperative (W3.3).
var (
	ErrAdminBoundaryNotFound = problem.New(http.StatusNotFound, "geo.admin_boundary_not_found", "Chưa có ranh giới hành chính cho đơn vị này")
	ErrVersionConflict       = problem.New(http.StatusConflict, "geo.version_conflict", "Ranh giới đã được sửa bởi người khác — hãy tải phiên bản hiện hành rồi sửa lại")
	ErrOutOfScope            = problem.New(http.StatusForbidden, "geo.out_of_scope", "Bạn không có quyền truy cập lô ruộng này.")
	ErrTooFewPoints          = problem.New(http.StatusUnprocessableEntity, domain.CodeTooFewPoints, "Cần tối thiểu 3 điểm ranh giới không thẳng hàng để khép vùng.")
	ErrInvalidGeometry       = problem.New(http.StatusUnprocessableEntity, domain.CodeInvalidGeometry, "Ranh giới không hợp lệ")
	ErrCRSUnsupported        = problem.New(http.StatusUnprocessableEntity, domain.CodeCRSUnsupported, "Hệ toạ độ không được hỗ trợ — chỉ nhận WGS84 (EPSG:4326)")
	ErrFormatInvalid         = problem.New(http.StatusUnprocessableEntity, domain.CodeFormat, "Tệp không đúng định dạng")
	ErrFormatUnsupported     = problem.New(http.StatusUnprocessableEntity, domain.CodeFormatUnsupported, "Định dạng tệp chưa được hỗ trợ")
	ErrAreaOutOfRange        = problem.New(http.StatusUnprocessableEntity, "geo.area_out_of_range", "Diện tích thửa ngoài giới hạn hợp lý")
	ErrDependency            = problem.New(http.StatusServiceUnavailable, "geo.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// Vai thửa do pf/cooperative khai (cooperativeport.RolePlotEditor/RolePlotViewer, W3.3) — geo (capability) không import
// module domain; tên vai giữ nguyên để người dùng bản đồ GIS vẫn đọc được lớp nền.
const (
	rolePlotEditor = "plot_editor"
	rolePlotViewer = "plot_viewer"
)

// Vai được đọc ranh giới hành chính.
var readRoles = []string{rolePlotViewer, rolePlotEditor, port.RoleGeoAdmin, authz.RolePlatformAdmin}

// Vai của Admin API.
var adminRoles = []string{port.RoleGeoAdmin, authz.RolePlatformAdmin}

// Service là use case của pf/geo.
type Service struct {
	DB     tenant.Beginner // pool; giao dịch chạy dưới r_geo (db.ForModule)
	Clock  clock.Clock
	Scope  partyport.ScopeResolver // bắt buộc — thiếu thì 503
	Params params.Params           // tùy chọn — thiếu thì dùng ngưỡng mặc định
	Units  partyport.AdminUnits    // bắt buộc cho ghi
	// Farmers (tùy chọn): nông hộ của tài khoản nông dân (pf/accounts) — vai farmer đọc log GPS của mình.
	Farmers port.FarmerHouseholds
	// Plots (tùy chọn, W3.3): dữ kiện thửa của pf/cooperative cho log GPS (xã/HTX, khoảng cách tới lô, thửa của
	// nông hộ). Thiếu → log không có khoảng cách; vai farmer nhận 503.
	Plots port.PlotGeometry
	// MapLayers (tùy chọn, W3.3): lớp ranh giới HTX và lớp thửa của pf/cooperative cho bản đồ gộp / chuẩn hóa.
	// Thiếu → yêu cầu lớp đó nhận 503 geo.dependency_unavailable.
	MapLayers port.MapLayers
	// Waterways (tùy chọn): lớp tuyến đường thủy (pf/waterway) cho bản đồ chuẩn hóa (GIS US-INT-02).
	Waterways port.Layer
	// ValveGrants (ADR-0020): tenant bên cấp nhận van hub_facility / hub_capacity của tenant phong
	// bì (pf/party). Thiếu → consumer van từ chối (DLQ, party.valve_share_grant_missing).
	ValveGrants partyport.ValveGrants
	// Egress (G5, ADR-0024): điểm kiểm soát xuất (pf/metering) — consumer van gọi Guard kênh valve
	// sau ValveTargets, mỗi bên cấp một lần. Nil → hành vi cũ.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// access là quyền của người gọi đã tính: vai, phạm vi xã và giới hạn theo HTX.
type access struct {
	p        authz.Principal
	sc       authz.Scope
	coopOnly bool // quản lý HTX: chỉ HTX của mình (OrgRef)
}

func (s *Service) access(ctx context.Context, roles ...string) (access, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return access{}, err
	}
	if s.Scope == nil {
		return access{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
	}
	sc, err := s.Scope.Scope(ctx)
	if err != nil {
		return access{}, err
	}
	return accessFor(p, sc), nil
}

func accessFor(p authz.Principal, sc authz.Scope) access {
	a := access{p: p, sc: sc}
	a.coopOnly = p.Has(port.RoleCoopManager) && !p.Has(authz.RolePlatformAdmin) && !p.Has(port.RoleGeoAdmin)
	if a.coopOnly && p.OrgRef == uuid.Nil {
		a.sc = authz.Scope{}
	}
	return a
}

func (a access) filter() store.Filter {
	return store.Filter{All: a.sc.All, Units: a.sc.Units, CoopOnly: a.coopOnly, Coop: a.p.OrgRef}
}

// violationProblem quy vi phạm của domain về problem 422 theo mã.
func violationProblem(err error) error {
	v, ok := domain.AsViolation(err)
	if !ok {
		return err
	}
	var base *problem.Problem
	switch v.Code {
	case domain.CodeTooFewPoints:
		return ErrTooFewPoints
	case domain.CodeCRSUnsupported:
		base = ErrCRSUnsupported
	case domain.CodeFormat:
		base = ErrFormatInvalid
	case domain.CodeFormatUnsupported:
		base = ErrFormatUnsupported
	default:
		base = ErrInvalidGeometry
	}
	return base.WithDetail(v.Msg)
}

// eventTime chuẩn hóa thời điểm sự việc: rỗng → hiện tại; không được ở tương lai quá 5 phút.
func (s *Service) eventTime(t time.Time) (time.Time, error) {
	now := s.now()
	if t.IsZero() {
		return now, nil
	}
	if t.After(now.Add(5 * time.Minute)) {
		return time.Time{}, problem.Validation.WithField("event_time", "không được ở tương lai")
	}
	return t.UTC(), nil
}

func (s *Service) inTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

var _ port.AdminGeometry = (*Service)(nil)

// OutsideCommunePct thực hiện port.AdminGeometry (pf/cooperative kiểm thửa nằm trong địa giới xã): tính trong giao
// dịch riêng của pf/geo. Tenant lấy từ context.
func (s *Service) OutsideCommunePct(ctx context.Context, geojson, commune string) (*decimal.Decimal, error) {
	var out *decimal.Decimal
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.OutsideCommunePct(ctx, tx, geojson, commune)
		return e
	})
	return out, err
}
