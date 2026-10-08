// Package app: use case nông hộ, xã viên HTX và hồ sơ HTX của pf/cooperative (W3.3 — chuyển từ pf/party; mã
// lỗi party.* giữ nguyên, hợp đồng HTTP không đổi). Giao dịch mở ở đây (tenant.WithTenantTx) trên pool của
// module (vai r_cooperative). Thuộc tính Nút HTX (mã, tên, đơn vị hành chính, trạng thái) đọc qua
// partyport.Orgs/OrgDirectory — không JOIN chéo schema; phạm vi qua partyport.ScopeResolver (ADR-0009) hoặc
// ngữ cảnh phiên KN (authz.RequireKN).
package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là module ghi outbox.
const Producer = "pf/cooperative"

// Mã lỗi (docs/errors.md, mục pf/party — mã giữ nguyên khi chuyển module).
var (
	ErrOrgNotFound   = problem.New(http.StatusNotFound, "party.org_not_found", "Không tìm thấy tổ chức")
	ErrOrgState      = problem.New(http.StatusConflict, "party.org_state_conflict", "Trạng thái tổ chức không cho phép thao tác này")
	ErrOutOfScope    = problem.New(http.StatusForbidden, "party.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrDependency    = problem.New(http.StatusServiceUnavailable, "party.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrImportInvalid = problem.New(http.StatusUnprocessableEntity, "party.import_invalid", "Tệp nhập không đúng mẫu quy định")
	// ErrPhoneTaken: sao chép nông hộ với SĐT đã có (HTX US-HH-02 AC-4, US-HH-01 AC-3).
	ErrPhoneTaken = problem.New(http.StatusConflict, "party.household_phone_taken", "SĐT đã được sử dụng bởi nông hộ khác")
	// ErrNoContact: chủ hộ chưa có SĐT hợp lệ trong kho định danh (HTX US-HH-03).
	ErrNoContact = problem.New(http.StatusNotFound, "party.household_contact_unavailable", "Nông hộ chưa có số điện thoại liên hệ")
)

// Vai được xem nông hộ (nhãn hiển thị đã che, mục 10.4). Cục KTHT không xem nông hộ.
var householdReaderRoles = []string{
	partyport.RoleRegistryAdmin, partyport.RoleKNNationalAdmin, partyport.RoleKNProvinceAdmin, partyport.RoleKNOfficer,
	partyport.RoleCoopManager, authz.RolePlatformAdmin,
}

// Vai được tạo nông hộ (TTKN QG chỉ đọc — KN US-HTX-01).
var householdWriterRoles = []string{partyport.RoleRegistryAdmin, partyport.RoleKNProvinceAdmin, partyport.RoleKNOfficer, partyport.RoleCoopManager}

// Service là use case nông hộ/xã viên/hồ sơ HTX. Trường port có thể nối dần; thiếu port mà thao tác cần →
// 503 party.dependency_unavailable.
type Service struct {
	DB    tenant.Beginner // pool của pf/cooperative (db.ForModule → r_cooperative)
	Clock clock.Clock
	// Orgs, Codes, Dir: Nút tổ chức của pf/party (HTX = Nút kind=cooperative).
	Orgs  partyport.Orgs
	Codes partyport.OrgCodes
	Dir   partyport.OrgDirectory
	// Scope: phạm vi theo cây phân cấp của người gọi (ADR-0009).
	Scope partyport.ScopeResolver
	// Consents: trạng thái đồng ý của nông hộ trong danh sách (G-04).
	Consents partyport.ConsentStates
	// Vault: kho định danh — chủ hộ, nhãn hiển thị đã che (cổng hẹp; bộ điều hợp cooperative/port/farmerid).
	Vault port.HouseholdIdentities
	// Names (tùy chọn): tên người thao tác cho khung Log (FN-XA-03.03).
	Names partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// fieldProblem ánh xạ domain.FieldError sang problem 422 theo trường.
func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

// vaultError phân loại lỗi của kho định danh: kho từ chối dữ liệu hoặc quyền (4xx,
// vd SĐT/CCCD sai dạng, thiếu vai vault_registrar) là lỗi của người gọi — trả
// nguyên problem của kho để client sửa, không thử lại; lỗi hệ thống hoặc không
// phản hồi → 503 party.dependency_unavailable.
func vaultError(err error) error {
	var p *problem.Problem
	if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
		return p
	}
	return ErrDependency.WithDetail("kho định danh từ chối hoặc không phản hồi").WithCause(err)
}

// scopeFor tính phạm vi đọc/ghi của người gọi qua pf/party (ScopeResolver đọc Principal từ ctx).
func (s *Service) scopeFor(ctx context.Context) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối phạm vi của pf/party")
	}
	return s.Scope.Scope(ctx)
}

// coopOrg đọc Nút HTX (kind=cooperative) của tenant tid qua pf/party; nil nếu không có hoặc không phải HTX.
func (s *Service) coopOrg(ctx context.Context, tid, ref uuid.UUID) (*partyport.Org, error) {
	if s.Orgs == nil {
		return nil, ErrDependency.WithDetail("chưa nối sổ tổ chức của pf/party")
	}
	o, err := s.Orgs.Get(tenant.WithID(ctx, tid), ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if o.Kind != partyport.OrgCooperative {
		return nil, nil
	}
	return &o, nil
}

// visible cho biết tổ chức/nông hộ gắn đơn vị unit nằm trong phạm vi đọc.
func visible(sc authz.Scope, unit string) bool {
	if sc.All {
		return true
	}
	return unit != "" && sc.Allows(unit)
}

// isCoopManagerOnly: vai quản lý HTX, không kèm vai cán bộ/quản trị có phạm vi rộng hơn.
func isCoopManagerOnly(p authz.Principal) bool {
	return p.Has(partyport.RoleCoopManager) && !p.Has(partyport.RoleRegistryAdmin) && !p.Has(partyport.RoleKNProvinceAdmin) &&
		!p.Has(partyport.RoleKNOfficer) && !p.Has(partyport.RoleKNNationalAdmin) && !p.Has(authz.RolePlatformAdmin)
}

// ConsentStates đọc trạng thái đồng ý hiện tại theo mục đích của các nông hộ (G-04) qua pf/party.
func (s *Service) ConsentStates(ctx context.Context, subjects []uuid.UUID) (map[uuid.UUID][]partyport.ConsentState, error) {
	if len(subjects) == 0 {
		return map[uuid.UUID][]partyport.ConsentState{}, nil
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if s.Consents == nil {
		return nil, ErrDependency.WithDetail("chưa nối đồng ý của pf/party")
	}
	return s.Consents.ConsentStates(tenant.WithID(ctx, p.TenantID), subjects)
}
