package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi của sổ tổ chức, HTX, nông hộ (docs/errors.md).
var (
	ErrOrgNotFound      = problem.New(http.StatusNotFound, "party.org_not_found", "Không tìm thấy tổ chức")
	ErrOrgCodeTaken     = problem.New(http.StatusConflict, "party.org_code_taken", "Mã tổ chức đã được dùng")
	ErrTaxCodeTaken     = problem.New(http.StatusConflict, "party.tax_code_taken", "Mã số thuế đã gắn với HTX khác")
	ErrInvalidHierarchy = problem.New(http.StatusUnprocessableEntity, "party.invalid_hierarchy", "Vị trí trong cây phân cấp không hợp lệ")
	ErrOrgState         = problem.New(http.StatusConflict, "party.org_state_conflict", "Trạng thái tổ chức không cho phép thao tác này")
	ErrActiveChildren   = problem.New(http.StatusConflict, "party.org_has_active_children", "Tổ chức còn tổ chức con đang hoạt động")
	ErrOutOfScope       = problem.New(http.StatusForbidden, "party.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrDependency       = problem.New(http.StatusServiceUnavailable, "party.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrImportInvalid    = problem.New(http.StatusUnprocessableEntity, "party.import_invalid", "Tệp nhập không đúng mẫu quy định")
	// ErrCoopNameTaken: HTX US-HTX-03 AC-3 (và nhập hàng loạt US-HTX-02) — tên HTX đã có trong tenant.
	ErrCoopNameTaken = problem.New(http.StatusConflict, "party.cooperative_name_taken", "Tên HTX này đã tồn tại, vui lòng chọn tên khác.")
	// ErrPhoneTaken: sao chép nông hộ với SĐT đã có (HTX US-HH-02 AC-4, US-HH-01 AC-3).
	ErrPhoneTaken = problem.New(http.StatusConflict, "party.household_phone_taken", "SĐT đã được sử dụng bởi nông hộ khác")
	// ErrNoContact: chủ hộ chưa có SĐT hợp lệ trong kho định danh (HTX US-HH-03).
	ErrNoContact = problem.New(http.StatusNotFound, "party.household_contact_unavailable", "Nông hộ chưa có số điện thoại liên hệ")
)

// Vai được đọc sổ tổ chức và HTX.
var readerRoles = []string{
	port.RoleRegistryAdmin, port.RoleKNNationalAdmin, port.RoleKNProvinceAdmin, port.RoleKNOfficer,
	port.RoleCoopManager, port.RoleCGHViewer, authz.RolePlatformAdmin,
}

// Vai được xem nông hộ (nhãn hiển thị đã che, mục 10.4). Cục KTHT không xem nông hộ.
var householdReaderRoles = []string{
	port.RoleRegistryAdmin, port.RoleKNNationalAdmin, port.RoleKNProvinceAdmin, port.RoleKNOfficer,
	port.RoleCoopManager, authz.RolePlatformAdmin,
}

// Vai được tạo nông hộ (TTKN QG chỉ đọc — KN US-HTX-01).
var householdWriterRoles = []string{port.RoleRegistryAdmin, port.RoleKNProvinceAdmin, port.RoleKNOfficer, port.RoleCoopManager}

// Vai quản trị dữ liệu chủ trên Admin API.
var adminRoles = []string{port.RoleRegistryAdmin, port.RoleKNProvinceAdmin, authz.RolePlatformAdmin}

// Registry là use case sổ tổ chức, HTX và nông hộ.
type Registry struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	Units mdport.AdminUnits // có thể nil khi chưa nối — thao tác cần nó trả ErrDependency
	Vault client.Client     // có thể nil khi chưa nối
	// Plots (tùy chọn): thống kê thửa đã số hoá của pf/geo cho danh sách HTX (G-03).
	Plots geoport.PlotStats
	// Activity (tùy chọn): vụ chưa xong / nhật ký chờ duyệt (pf/cropcycle) cho màn cảnh báo
	// trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1).
	Activity port.SeasonActivity
	// Machines (tùy chọn): tổng số máy khai báo của HTX (pf/mechanization, KN v7.7 FN-XA-03.04) cho
	// cảnh báo vô hiệu hóa HTX (FN-XA-03.03).
	Machines port.CoopMachineCounts
}

var (
	_ port.Orgs          = (*Registry)(nil)
	_ port.OrgCodes      = (*Registry)(nil)
	_ port.ScopeResolver = (*Registry)(nil)
)

// fieldProblem ánh xạ domain.FieldError sang problem 422 theo trường.
func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

// Get thực hiện port.Orgs cho module khác. Tenant lấy từ context.
func (r *Registry) Get(ctx context.Context, ref uuid.UUID) (port.Org, error) {
	var s *store.OrgState
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		s, e = store.OrgByRef(ctx, tx, ref, false)
		return e
	})
	if err != nil {
		return port.Org{}, err
	}
	if s == nil {
		return port.Org{}, port.ErrOrgNotFound
	}
	return s.Org, nil
}

// ByCode thực hiện port.OrgCodes cho module khác. Tenant lấy từ context.
func (r *Registry) ByCode(ctx context.Context, code string) (port.Org, error) {
	var o *port.Org
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		o, e = store.OrgByCode(ctx, tx, code)
		return e
	})
	if err != nil {
		return port.Org{}, err
	}
	if o == nil {
		return port.Org{}, port.ErrOrgNotFound
	}
	return *o, nil
}

// Scope thực hiện port.ScopeResolver (ADR-0009).
func (r *Registry) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	return r.scopeFor(ctx, p)
}

func (r *Registry) scopeFor(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	if p.OrgRef == uuid.Nil {
		return authz.Scope{}, nil // không có đơn vị công tác → không thấy gì
	}
	var s *store.OrgState
	err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		s, e = store.OrgByRef(ctx, tx, p.OrgRef, false)
		return e
	})
	if err != nil {
		return authz.Scope{}, err
	}
	// Tổ chức không tồn tại hoặc đã bị vô hiệu hóa → mặc định chặn (US-HTXSTATUS-01).
	if s == nil || !s.Active {
		return authz.Scope{}, nil
	}
	switch s.Kind {
	case port.OrgTTKNNational, port.OrgDCRD, port.OrgMKG:
		return authz.Scope{All: true}, nil
	case port.OrgTTKNProvince:
		if s.AdminUnitCode == "" {
			return authz.Scope{}, nil
		}
		if r.Units == nil {
			return authz.Scope{}, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
		}
		codes, err := r.Units.Descendants(ctx, s.AdminUnitCode, r.Clock.Now())
		if errors.Is(err, mdport.ErrUnitNotFound) {
			return authz.Scope{}, nil
		}
		if err != nil {
			return authz.Scope{}, err
		}
		return authz.Scope{Units: codes}, nil
	case port.OrgExtensionTeam, port.OrgCooperative:
		if s.AdminUnitCode == "" {
			return authz.Scope{}, nil
		}
		return authz.Scope{Units: []string{s.AdminUnitCode}}, nil
	}
	return authz.Scope{}, nil
}
