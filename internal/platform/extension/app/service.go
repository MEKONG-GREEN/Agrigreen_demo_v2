// Package app: use case của pf/extension. Giao dịch mở ở đây; kiểm vai bằng
// authz.Require và phạm vi TW → Tỉnh → Xã bằng party/port.ScopeResolver (ADR-0009).
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrTaskNotFound       = problem.New(http.StatusNotFound, "extension.task_not_found", "Không tìm thấy nhiệm vụ")
	ErrTaskState          = problem.New(http.StatusConflict, "extension.task_state_conflict", "Trạng thái nhiệm vụ không cho phép thao tác này")
	ErrNotAssignee        = problem.New(http.StatusForbidden, "extension.not_assignee", "Chỉ cán bộ được giao mới cập nhật được nhiệm vụ")
	ErrAssigneeInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.assignee_invalid", "Cán bộ được giao không phụ trách địa bàn của nhiệm vụ")
	ErrOutOfScope         = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrDirectiveNotFound  = problem.New(http.StatusNotFound, "extension.directive_not_found", "Không tìm thấy chỉ đạo")
	ErrNotTarget          = problem.New(http.StatusForbidden, "extension.not_directive_target", "Bạn không thuộc danh sách nhận chỉ đạo này")
	ErrNoTargets          = problem.New(http.StatusUnprocessableEntity, "extension.no_directive_targets", "Vùng đã chọn chưa có cán bộ nào trong danh bạ để nhận chỉ đạo")
	ErrReportNotFound     = problem.New(http.StatusNotFound, "extension.report_not_found", "Không tìm thấy báo cáo")
	ErrPeriodClosed       = problem.New(http.StatusConflict, "extension.period_closed", "Kỳ báo cáo đã chốt — chỉ được tạo bản ghi bù kèm lý do")
	ErrReasonRequired     = problem.New(http.StatusUnprocessableEntity, "extension.correction_reason_required", "Bản ghi bù phải nêu lý do (≥ 10 ký tự)")
	ErrReportSuperseded   = problem.New(http.StatusConflict, "extension.report_superseded", "Báo cáo này đã có phiên bản mới hơn — sửa trên phiên bản hiện hành")
	ErrCatalogCode        = problem.New(http.StatusUnprocessableEntity, "extension.catalog_code_invalid", "Mã không có hoặc đã ngừng dùng trong danh mục dùng chung")
	ErrCooperativeInvalid = problem.New(http.StatusUnprocessableEntity, "extension.cooperative_invalid", "HTX không tồn tại, đã ngừng hoạt động hoặc không thuộc xã đã chọn")
	ErrUnitInvalid        = problem.New(http.StatusUnprocessableEntity, "extension.unit_invalid", "Mã đơn vị hành chính không có hiệu lực")
	ErrStaffNotFound      = problem.New(http.StatusNotFound, "extension.staff_not_found", "Cán bộ chưa có trong danh bạ")
	ErrDependency         = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrIDReuse            = problem.New(http.StatusConflict, "extension.id_reused", "id đã dùng cho bản ghi khác")
	ErrNoContact          = problem.New(http.StatusNotFound, "extension.contact_unavailable", "Cán bộ chưa khai số điện thoại liên hệ")
)

// Nhóm vai.
var (
	knReaders    = []string{port.RoleKNOfficer, port.RoleKNProvinceAdmin, port.RoleKNNationalAdmin, authz.RolePlatformAdmin}
	taskCreators = []string{port.RoleKNOfficer, port.RoleKNProvinceAdmin, port.RoleCoopManager, authz.RolePlatformAdmin}
	taskReaders  = []string{port.RoleKNOfficer, port.RoleKNProvinceAdmin, port.RoleKNNationalAdmin, port.RoleCoopManager, authz.RolePlatformAdmin}
	supervisors  = []string{port.RoleKNProvinceAdmin, port.RoleKNNationalAdmin, authz.RolePlatformAdmin}
	fieldWriters = []string{port.RoleKNOfficer, port.RoleKNProvinceAdmin}
	rosterReader = []string{port.RoleKNOfficer, port.RoleKNProvinceAdmin, port.RoleKNNationalAdmin, port.RoleCoopManager, authz.RolePlatformAdmin}
)

// SystemActor là actor_ref của job hệ thống (leo thang SLA) trong sổ.
var SystemActor = uuid.MustParse("00000000-0000-0000-0000-00000000e5c1")

// Service là use case của module.
type Service struct {
	DB       tenant.Beginner // pool vai extension_app
	Clock    clock.Clock
	Scope    partyport.ScopeResolver
	Orgs     partyport.Orgs
	Units    partyport.AdminUnits
	Params   params.Params
	Catalogs mdport.Catalogs
	// Plots, Coops (tùy chọn): thống kê thửa và HTX của pf/cooperative cho bảng điều hành gộp (G-02).
	Plots coopport.Plots
	Coops coopport.Cooperatives
	// Contacts (tùy chọn): số điện thoại cán bộ ở pf/accounts cho "gọi/nhắn 1 chạm" (US-DIR-02).
	Contacts accport.StaffContacts
	// Farmers: liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) cho yêu cầu hỗ trợ
	// từ App HTX (HTX US-SUPPORT-01). Nil → route yêu cầu hỗ trợ trả 503.
	Farmers accport.FarmerHouseholds
	// GpsLog (tùy chọn): log GPS của pf/geo cho consumer port.GpsLogConsumer (HTX US-GPS-03).
	GpsLog geoport.GpsLog
	// KNContext, KNDirectory (tùy chọn, P1): ngữ cảnh phiên KN và danh bạ gán vai trò cho lịch công tác (P5).
	// Thiếu → suy từ vai realm + phạm vi đơn vị, danh bạ trực pf_extension.staff.
	KNContext   accport.KNContextResolver
	KNDirectory accport.KNDirectory
	// ContactNames (tùy chọn, W11.23 pf/contact): tên cán bộ theo tài khoản từ Contact — đọc trước danh bạ trực (Q-072).
	ContactNames partyport.ActorNames
	// TablePDF (tùy chọn, pf/reports): "Xuất PDF" lịch công tác. Thiếu → 503.
	TablePDF port.TablePDF
	// Egress (tùy chọn, G5 ADR-0024 + quyết định A5): điểm kiểm soát xuất dùng chung (pf/metering) cho "Xuất
	// Excel/PDF" lịch công tác — xuất NỘI BỘ (Internal): ghi egress_event, đếm, cảnh báo, không bao giờ chặn.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// scope lấy phạm vi của người gọi. Thiếu resolver → lỗi phụ thuộc (mặc định chặn).
func (s *Service) scope(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if sc, ok := ctx.Value(scopeKey{}).(authz.Scope); ok {
		return sc, nil // pf/sync đã tính phạm vi và truyền vào Sink.Accept
	}
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
	}
	return s.Scope.Scope(ctx)
}

type scopeKey struct{}

// withScope gắn phạm vi đã tính sẵn (do pf/sync truyền vào) cho các use case.
func withScope(ctx context.Context, sc authz.Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, sc)
}

// provinceOf trả mã tỉnh của một đơn vị (xã → tỉnh cha; tỉnh → chính nó).
func (s *Service) provinceOf(ctx context.Context, unit, field string) (string, partyport.Level, error) {
	if s.Units == nil {
		return "", "", ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, unit, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return "", "", ErrUnitInvalid.WithField(field, "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return "", "", err
	}
	switch u.Level {
	case partyport.LevelProvince:
		return u.Code, u.Level, nil
	case partyport.LevelCommune:
		return u.ParentCode, u.Level, nil
	}
	return "", "", ErrUnitInvalid.WithField(field, "chỉ nhận đơn vị cấp tỉnh hoặc xã hiện hành")
}

// paramDecimal đọc tham số đã duyệt tại thời điểm at; chưa có → giá trị mặc định.
func (s *Service) paramDecimal(ctx context.Context, key string, at time.Time, def decimal.Decimal) (decimal.Decimal, error) {
	if s.Params == nil {
		return def, nil
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, params.ErrParamNotFound) {
		return def, nil
	}
	if err != nil {
		return decimal.Decimal{}, err
	}
	return v.Value, nil
}

// checkCatalog kiểm mã thuộc danh mục dùng chung và đang dùng (FR-01 BR-01).
func (s *Service) checkCatalog(ctx context.Context, catalog, code, field string) error {
	if s.Catalogs == nil {
		return ErrDependency.WithDetail("chưa nối danh mục dùng chung")
	}
	it, err := s.Catalogs.Get(ctx, catalog, code)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) || (err == nil && !it.Active) {
		return ErrCatalogCode.WithField(field, "không có trong danh mục "+catalog+" hoặc đã ngừng dùng")
	}
	return err
}

// checkCooperative kiểm HTX tồn tại, đang hoạt động và thuộc xã (khi đã nối pf/party).
func (s *Service) checkCooperative(ctx context.Context, ref uuid.UUID, commune string) error {
	if ref == uuid.Nil || s.Orgs == nil {
		return nil
	}
	o, err := s.Orgs.Get(ctx, ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return ErrCooperativeInvalid.WithField("cooperative_ref", "không tồn tại")
	}
	if err != nil {
		return err
	}
	if o.Kind != partyport.OrgCooperative || !o.Active || o.AdminUnitCode != commune {
		return ErrCooperativeInvalid.WithField("cooperative_ref", "không phải HTX đang hoạt động của xã này")
	}
	return nil
}

// provinceOrgOf đi ngược cây tổ chức từ org tới TTKN tỉnh gần nhất (nơi nhận leo thang).
func (s *Service) provinceOrgOf(ctx context.Context, org uuid.UUID) uuid.UUID {
	if s.Orgs == nil {
		return uuid.Nil
	}
	for i := 0; i < 6 && org != uuid.Nil; i++ {
		o, err := s.Orgs.Get(ctx, org)
		if err != nil {
			return uuid.Nil
		}
		if o.Kind == partyport.OrgTTKNProvince {
			return o.Ref
		}
		org = o.ParentRef
	}
	return uuid.Nil
}

// visible cho biết bản ghi gắn xã commune (thuộc tỉnh province) nằm trong phạm vi đọc.
func visible(sc authz.Scope, commune, province string) bool {
	return sc.All || slices.Contains(sc.Units, commune) || slices.Contains(sc.Units, province)
}

// fieldProblem ánh xạ domain.FieldError sang problem 422.
func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}
