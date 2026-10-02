// Package app: use case của pf/geo — thửa có ranh giới theo phiên bản, chồng
// lấn, nhập tệp, ranh giới hành chính và đồng bộ ngoại tuyến. Mở giao dịch bằng
// tenant.WithTenantTx; kiểm vai (authz.Require) và phạm vi (party ScopeResolver).
package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi của pf/geo (docs/errors.md).
var (
	ErrPlotNotFound          = problem.New(http.StatusNotFound, "geo.plot_not_found", "Không tìm thấy thửa")
	ErrConflictNotFound      = problem.New(http.StatusNotFound, "geo.conflict_not_found", "Không tìm thấy xung đột ranh giới")
	ErrAdminBoundaryNotFound = problem.New(http.StatusNotFound, "geo.admin_boundary_not_found", "Chưa có ranh giới hành chính cho đơn vị này")
	ErrOverlapCrossCoop      = problem.New(http.StatusConflict, "geo.overlap_cross_coop", "Ranh giới bị chồng lấn với lô đã đăng ký của HTX/nông hộ khác, vui lòng kiểm tra lại.")
	ErrOverlapNeedsConfirm   = problem.New(http.StatusConflict, "geo.overlap_needs_confirm", "Ranh giới bị chồng lấn với lô đã đăng ký, vui lòng kiểm tra lại hoặc xác nhận giữ nguyên.")
	ErrAreaDeviation         = problem.New(http.StatusConflict, "geo.area_deviation_needs_confirm", "Diện tích đo lệch quá ngưỡng so với diện tích khai báo")
	ErrVersionConflict       = problem.New(http.StatusConflict, "geo.version_conflict", "Ranh giới đã được sửa bởi người khác — hãy tải phiên bản hiện hành rồi sửa lại")
	ErrPlotState             = problem.New(http.StatusConflict, "geo.plot_state_conflict", "Trạng thái thửa không cho phép thao tác này")
	ErrConflictState         = problem.New(http.StatusConflict, "geo.conflict_state", "Xung đột đã được xử lý")
	ErrPlotExists            = problem.New(http.StatusConflict, "geo.plot_exists", "Mã tham chiếu thửa đã được dùng")
	ErrOutOfScope            = problem.New(http.StatusForbidden, "geo.out_of_scope", "Bạn không có quyền truy cập lô ruộng này.")
	ErrTooFewPoints          = problem.New(http.StatusUnprocessableEntity, domain.CodeTooFewPoints, "Cần tối thiểu 4 điểm ranh giới không thẳng hàng để khép vùng.")
	ErrInvalidGeometry       = problem.New(http.StatusUnprocessableEntity, domain.CodeInvalidGeometry, "Ranh giới không hợp lệ")
	ErrCRSUnsupported        = problem.New(http.StatusUnprocessableEntity, domain.CodeCRSUnsupported, "Hệ toạ độ không được hỗ trợ — chỉ nhận WGS84 (EPSG:4326)")
	ErrFormatInvalid         = problem.New(http.StatusUnprocessableEntity, domain.CodeFormat, "Tệp không đúng định dạng")
	ErrFormatUnsupported     = problem.New(http.StatusUnprocessableEntity, domain.CodeFormatUnsupported, "Định dạng tệp chưa được hỗ trợ")
	ErrAreaOutOfRange        = problem.New(http.StatusUnprocessableEntity, "geo.area_out_of_range", "Diện tích thửa ngoài giới hạn hợp lý")
	ErrOutsideCommune        = problem.New(http.StatusUnprocessableEntity, "geo.outside_commune", "Ranh giới nằm ngoài địa giới xã")
	ErrHouseholdInvalid      = problem.New(http.StatusUnprocessableEntity, "geo.household_invalid", "Nông hộ không tồn tại, đã ngừng hoạt động hoặc không là thành viên của HTX được gán thửa")
	ErrCooperativeInvalid    = problem.New(http.StatusUnprocessableEntity, "geo.cooperative_invalid", "HTX không tồn tại, đã ngừng hoạt động hoặc ngoài phạm vi")
	ErrDependency            = problem.New(http.StatusServiceUnavailable, "geo.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// Vai được đọc thửa.
var readRoles = []string{port.RolePlotViewer, port.RolePlotEditor, port.RoleGeoAdmin, authz.RolePlatformAdmin}

// Vai được ghi thửa (Application API và đồng bộ).
var writeRoles = []string{port.RolePlotEditor, authz.RolePlatformAdmin}

// syncWriteRoles: ranh giới đẩy lên qua pf/sync (entity plot_boundary). Thêm quản lý HTX
// (App HTX, HTX US-PLOT-01/02) — không cần vai plot_editor; phạm vi vẫn giới hạn ở HTX
// của mình (access.coopOnly): thửa HTX mình, thửa nháp chưa gán mà họ vẽ nhận
// cooperative_ref = HTX của họ; thửa HTX khác → rejected geo.out_of_scope.

// Vai của Admin API.
var adminRoles = []string{port.RoleGeoAdmin, authz.RolePlatformAdmin}

var syncWriteRoles = []string{port.RolePlotEditor, port.RoleCoopManager, authz.RolePlatformAdmin}

// Service là use case của pf/geo.
type Service struct {
	DB     tenant.Beginner // pool vai geo_app
	Clock  clock.Clock
	Scope  partyport.ScopeResolver // bắt buộc — thiếu thì 503
	Params mdport.Params           // tùy chọn — thiếu thì dùng ngưỡng mặc định
	Units  mdport.AdminUnits       // bắt buộc cho ghi
	Orgs   partyport.Orgs          // tùy chọn — có thì kiểm HTX khi gán
	// Households (tùy chọn): có thì kiểm nông hộ thuộc đúng HTX của thửa khi gán.
	Households partyport.Households
	// Markers (tùy chọn): điểm pf_party.cooperative.location làm marker/dự phòng của lớp HTX.
	Markers port.CoopMarkers
	// Farmers (tùy chọn): nông hộ của tài khoản nông dân (pf/accounts) — vai farmer đọc log GPS của mình.
	Farmers port.FarmerHouseholds
	// Waterways (tùy chọn): lớp tuyến đường thủy (pf/waterway) cho bản đồ chuẩn hóa (GIS US-INT-02).
	Waterways wwport.Layer
	// Sharing (ADR-0020): kiểm cấp quyền chia sẻ + sổ đọc cho bề mặt đối tác /v1/partner/plots
	// (pf/party). Thiếu → 403 party.share_grant_missing (mặc định từ chối).
	Sharing partyport.PartnerAccess
	// ValveGrants (ADR-0020): tenant bên cấp nhận van hub_facility / hub_capacity của tenant phong
	// bì (pf/party). Thiếu → consumer van từ chối (DLQ, party.valve_share_grant_missing).
	ValveGrants partyport.ValveGrants
	// Vault (tùy chọn, KN v7.7 FN-XA-03.05): tên nông dân của thửa → chủ thể "người" (farmer_ref); nhãn che khi đọc.
	// Thiếu → ghi có farmer_name trả 503 geo.dependency_unavailable; đọc trả farmer = null.
	Vault client.Client
	// Names (tùy chọn): tên người thao tác trong khung Log của thửa KN.
	Names partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// access là quyền của người gọi đã tính: vai, phạm vi xã và giới hạn theo HTX.
type access struct {
	p        authz.Principal
	sc       authz.Scope
	coopOnly bool // quản lý HTX: chỉ HTX của mình (OrgRef) và thửa nháp chưa gán
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

// sees cho biết người gọi thấy thửa (xã trong phạm vi; quản lý HTX: HTX mình hoặc thửa nháp).
func (a access) sees(commune string, coop uuid.UUID) bool {
	if !a.sc.Allows(commune) {
		return false
	}
	return !a.coopOnly || coop == uuid.Nil || coop == a.p.OrgRef
}

// canWrite kiểm quyền ghi vào thửa thuộc xã commune, HTX coop.
func (a access) canWrite(commune string, coop uuid.UUID) error {
	if !a.sc.Allows(commune) {
		return ErrOutOfScope
	}
	if a.coopOnly && coop != uuid.Nil && coop != a.p.OrgRef {
		return ErrOutOfScope
	}
	return nil
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

// tolerance đọc ngưỡng chồng lấn có hiệu lực tại thời điểm sự việc (mục 16).
func (s *Service) tolerance(ctx context.Context, at time.Time) (decimal.Decimal, error) {
	if s.Params == nil {
		return domain.DefaultOverlapTolerancePct, nil
	}
	v, err := s.Params.At(ctx, port.ParamOverlapTolerancePct, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return domain.DefaultOverlapTolerancePct, nil
	}
	if err != nil {
		return decimal.Zero, ErrDependency.WithDetail("không đọc được tham số ngưỡng chồng lấn").WithCause(err)
	}
	if v.Value.IsNegative() {
		return domain.DefaultOverlapTolerancePct, nil
	}
	return v.Value, nil
}

// thresholds đọc ngưỡng chồng lấn và ngưỡng lệch diện tích tại thời điểm sự việc.
func (s *Service) thresholds(ctx context.Context, at time.Time) (tol, maxDev decimal.Decimal, err error) {
	if tol, err = s.tolerance(ctx, at); err != nil {
		return
	}
	maxDev = domain.DefaultAreaDeviationMaxPct
	if s.Params == nil {
		return
	}
	v, perr := s.Params.At(ctx, port.ParamAreaDeviationMaxPct, at)
	switch {
	case errors.Is(perr, mdport.ErrParamNotFound):
	case perr != nil:
		err = ErrDependency.WithDetail("không đọc được tham số ngưỡng lệch diện tích").WithCause(perr)
	case v.Value.IsPositive():
		maxDev = v.Value
	}
	return
}

// checkCommune kiểm mã xã còn hiệu lực và đúng cấp xã.
func (s *Service) checkCommune(ctx context.Context, code string) error {
	if code == "" {
		return problem.Validation.WithField("commune_code", "bắt buộc")
	}
	if s.Units == nil {
		return ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, mdport.ErrUnitNotFound) {
		return problem.Validation.WithField("commune_code", "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
	}
	if u.Level != mdport.LevelCommune {
		return problem.Validation.WithField("commune_code", "phải là mã cấp xã")
	}
	return nil
}

// checkCooperative kiểm HTX khi đã nối pf/party: tồn tại, đúng loại, đang hoạt động, trong phạm vi.
func (s *Service) checkCooperative(ctx context.Context, a access, coop uuid.UUID) error {
	if coop == uuid.Nil || s.Orgs == nil {
		return nil
	}
	o, err := s.Orgs.Get(ctx, coop)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return ErrCooperativeInvalid
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || !o.Active || (!a.sc.All && !a.sc.Allows(o.AdminUnitCode)) {
		return ErrCooperativeInvalid
	}
	return nil
}

// checkHousehold kiểm nông hộ là thành viên ĐANG HOẠT ĐỘNG của đúng HTX coop
// (mỗi thửa đúng một HTX và nông hộ thuộc HTX đó — HTX US-PLOT-01, BRD).
func (s *Service) checkHousehold(ctx context.Context, household, coop uuid.UUID) error {
	if household == uuid.Nil || s.Households == nil {
		return nil
	}
	m, err := s.Households.Membership(ctx, household)
	if errors.Is(err, partyport.ErrHouseholdNotFound) {
		return ErrHouseholdInvalid
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được nông hộ").WithCause(err)
	}
	if !m.Active || m.CooperativeRef != coop {
		return ErrHouseholdInvalid
	}
	return nil
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

var _ port.Plots = (*Service)(nil)

// Get thực hiện port.Plots cho module khác. Tenant lấy từ context.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	var p *store.Plot
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		p, e = store.PlotByRef(ctx, tx, ref, false)
		return e
	})
	if err != nil {
		return port.Plot{}, err
	}
	if p == nil {
		return port.Plot{}, port.ErrPlotNotFound
	}
	return port.Plot{
		Ref: p.Ref, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef,
		Status: p.Status, Version: p.CurrentVersion, AreaHa: p.AreaHa,
	}, nil
}

// DistanceM thực hiện port.Plots: khoảng cách trắc địa từ điểm tới thửa (HTX US-GPS-01).
func (s *Service) DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) {
	var d *decimal.Decimal
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		d, e = store.DistanceM(ctx, tx, ref, lon.String(), lat.String())
		return e
	})
	if err != nil {
		return decimal.Zero, err
	}
	if d == nil {
		return decimal.Zero, port.ErrPlotNotFound
	}
	return *d, nil
}
