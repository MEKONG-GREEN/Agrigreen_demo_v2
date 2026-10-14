package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi của sổ tổ chức, HTX (docs/errors.md). Mã lỗi nông hộ (party.household_*) do pf/cooperative trả (W3.3).
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
)

// Vai được đọc sổ tổ chức và HTX.
var readerRoles = []string{
	port.RoleRegistryAdmin, port.RoleKNNationalAdmin, port.RoleKNProvinceAdmin, port.RoleKNOfficer,
	port.RoleCoopManager, port.RoleCGHViewer, authz.RolePlatformAdmin,
}

// Vai quản trị dữ liệu chủ trên Admin API.
var adminRoles = []string{port.RoleRegistryAdmin, port.RoleKNProvinceAdmin, authz.RolePlatformAdmin}

// orgNodeRoles: vai được TẠO Nút hành chính / TTKN (mọi loại trừ HTX). Quyết định PO 14/10/2026 (gói ORGUI,
// docs/kn/gia-dinh/ORGUI.md): xã là đơn vị hành chính, chỉ Admin nền tảng tạo trên trang "Cơ cấu tổ chức";
// quản trị KN (kn_province_admin…) không tạo. Di chuyển/đổi tên/vô hiệu hóa Nút vẫn theo adminRoles + phạm vi.
var orgNodeRoles = []string{port.RoleRegistryAdmin, authz.RolePlatformAdmin}

// requireNodeCreator chặn vai KN tạo Nút không phải HTX (403 forbidden).
func requireNodeCreator(p authz.Principal, kind port.OrgKind) error {
	if kind == port.OrgCooperative {
		return nil
	}
	for _, r := range orgNodeRoles {
		if p.Has(r) {
			return nil
		}
	}
	return problem.Forbidden.WithDetail("chỉ Admin nền tảng (platform_admin, registry_admin) được tạo Nút hành chính / TTKN")
}

// Registry là use case sổ tổ chức và HTX (Nút). Nông hộ/xã viên và hồ sơ HTX ở pf/cooperative (W3.3).
type Registry struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	Units port.AdminUnits // có thể nil khi chưa nối — thao tác cần nó trả ErrDependency
	Vault client.Client   // có thể nil khi chưa nối
	// Plots (tùy chọn): thống kê thửa đã số hoá của pf/geo cho danh sách HTX (G-03).
	Plots port.PlotStats
	// Activity (tùy chọn): vụ chưa xong / nhật ký chờ duyệt (pf/cropcycle) cho màn cảnh báo
	// trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1).
	Activity port.SeasonActivity
	// Machines (tùy chọn): tổng số máy khai báo của HTX (pf/mechanization, KN v7.7 FN-XA-03.04) cho
	// cảnh báo vô hiệu hóa HTX (FN-XA-03.03).
	Machines port.CoopMachineCounts
	// Profiles: hồ sơ HTX và số nông hộ thành viên (pf/cooperative, W3.3). Thiếu → thao tác cần hồ sơ HTX trả
	// 503 party.dependency_unavailable.
	Profiles port.CoopProfiles
}

var (
	_ port.Orgs          = (*Registry)(nil)
	_ port.OrgCodes      = (*Registry)(nil)
	_ port.ScopeResolver = (*Registry)(nil)
	_ port.OrgDirectory  = (*Registry)(nil)
	_ port.ConsentStates = (*Registry)(nil)
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
		if errors.Is(err, port.ErrUnitNotFound) {
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

// OrgsByRefs thực hiện port.OrgDirectory. Tenant lấy từ context.
func (r *Registry) OrgsByRefs(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]port.Org, error) {
	out := make(map[uuid.UUID]port.Org, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	var rows []port.Org
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.OrgsByRefs(ctx, tx, refs)
		return e
	})
	if err != nil {
		return nil, err
	}
	for _, o := range rows {
		out[o.Ref] = o
	}
	return out, nil
}

// OrgsOfKind thực hiện port.OrgDirectory. Tenant lấy từ context.
func (r *Registry) OrgsOfKind(ctx context.Context, kind port.OrgKind, units []string, activeOnly bool) ([]port.Org, error) {
	if units != nil && len(units) == 0 {
		return []port.Org{}, nil
	}
	var out []port.Org
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.OrgsOfKind(ctx, tx, string(kind), units, activeOnly)
		return e
	})
	return out, err
}
