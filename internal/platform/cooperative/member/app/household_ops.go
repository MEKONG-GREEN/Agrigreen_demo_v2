package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxCopyHouseholds giới hạn số hồ sơ của một lần sao chép hàng loạt (mỗi hồ sơ một
// lần gọi kho định danh — giữ request dưới vài giây, R-52).
const MaxCopyHouseholds = 50

// Trạng thái từng hồ sơ sao chép (HTX US-HH-02 AC-3).
const (
	CopyCreated  = "created"
	CopyRejected = "rejected"
)

// Lý do loại một hồ sơ sao chép (HTX US-HH-02 AC-4). Không chứa dữ liệu cá nhân.
const (
	ReasonMissingName  = "Thiếu họ tên"
	ReasonMissingPhone = "Thiếu SĐT"
	ReasonPhoneTaken   = "SĐT đã tồn tại"
)

// ErrManagerOtherCoop: quản lý HTX thao tác nông hộ của HTX khác (HTX US-HH-01 AC-3).
var errManagerOtherCoop = ErrOutOfScope.WithDetail("Bạn không có quyền truy cập nông hộ này.")

// CopyHouseholdItem là một hồ sơ mới sao chép từ nông hộ nguồn: họ tên, SĐT (và tùy
// chọn CCCD, địa chỉ) do người dùng sửa riêng cho từng hồ sơ — định danh của hộ nguồn
// KHÔNG được đọc hay chép (R-04). Trường chung chép từ hộ nguồn: HTX, xã, diện tích canh tác.
type CopyHouseholdItem struct {
	SourceRef uuid.UUID
	Identity  FarmerIdentity
}

// CopyHouseholdsInput là yêu cầu sao chép hàng loạt.
type CopyHouseholdsInput struct {
	// TargetCooperativeRef: HTX của hồ sơ mới; Nil = HTX hiện tại của từng hộ nguồn.
	TargetCooperativeRef uuid.UUID
	Items                []CopyHouseholdItem
}

// CopyRow là kết quả một hồ sơ (Index bắt đầu từ 1 theo thứ tự gửi).
type CopyRow struct {
	Index        int
	SourceRef    uuid.UUID
	Status       string
	Reason       string
	HouseholdRef uuid.UUID
}

// CopyResult là kết quả cả lần sao chép.
type CopyResult struct {
	Created, Rejected int
	Rows              []CopyRow
}

// CopyHouseholds sao chép hàng loạt nông hộ (HTX US-HH-02 AC-2..4): mỗi hồ sơ hợp lệ
// thành một nông hộ mới (chủ hộ mới trong kho định danh); hồ sơ lỗi bị loại kèm lý do
// "Thiếu họ tên" / "Thiếu SĐT" / "SĐT đã tồn tại", không chặn hồ sơ khác. Gọi lặp
// cùng Idempotency-Key được middleware phát lại đúng phản hồi lần đầu (R-51).
func (s *Service) CopyHouseholds(ctx context.Context, in CopyHouseholdsInput) (CopyResult, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return CopyResult{}, err
	}
	if len(in.Items) == 0 || len(in.Items) > MaxCopyHouseholds {
		return CopyResult{}, problem.Validation.WithField("items", "1–50 hồ sơ")
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return CopyResult{}, err
	}
	var res CopyResult
	for i, it := range in.Items {
		row := CopyRow{Index: i + 1, SourceRef: it.SourceRef}
		reason := ""
		switch {
		case strings.TrimSpace(it.Identity.FullName) == "":
			reason = ReasonMissingName
		case strings.TrimSpace(it.Identity.Phone) == "":
			reason = ReasonMissingPhone
		}
		if reason == "" {
			src, err := s.visibleHousehold(ctx, p, sc, it.SourceRef)
			if err != nil {
				var pr *problem.Problem
				if !errors.As(err, &pr) || pr.Status >= 500 {
					return CopyResult{}, err
				}
				reason = copyReason(pr)
			} else {
				target := in.TargetCooperativeRef
				if target == uuid.Nil {
					target = src.CooperativeRef
				}
				commune := src.CommuneCode
				if target != src.CooperativeRef {
					commune = "" // HTX khác: nông hộ lấy xã của HTX đích
				}
				out, err := s.createHousehold(ctx, p, sc, CreateHouseholdInput{
					Identity: it.Identity, CooperativeRef: target, CommuneCode: commune,
					CultivatedAreaHa: src.CultivatedAreaHa, RejectDuplicate: true,
				})
				if err != nil {
					var pr *problem.Problem
					if !errors.As(err, &pr) || pr.Status >= 500 {
						return CopyResult{}, err // lỗi hệ thống: dừng, client gọi lại
					}
					reason = copyReason(pr)
				} else {
					row.Status, row.HouseholdRef = CopyCreated, out.Household.Ref
				}
			}
		}
		if reason != "" {
			row.Status, row.Reason = CopyRejected, reason
			res.Rejected++
		} else {
			res.Created++
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

func copyReason(pr *problem.Problem) string {
	switch pr.Code {
	case ErrPhoneTaken.Code:
		return ReasonPhoneTaken
	case ErrHouseholdNotFound.Code:
		return "Không tìm thấy nông hộ nguồn"
	}
	if pr.Detail != "" && pr.Status == http.StatusForbidden {
		return pr.Detail
	}
	return rowReason(pr)
}

// visibleHousehold đọc nông hộ trong phạm vi người gọi; quản lý HTX chỉ hộ của HTX mình.
func (s *Service) visibleHousehold(ctx context.Context, p authz.Principal, sc authz.Scope, ref uuid.UUID) (domain.Household, error) {
	var h *domain.Household
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		h, e = store.HouseholdByRef(ctx, tx, ref)
		return e
	}); err != nil {
		return domain.Household{}, err
	}
	if h == nil || !sc.Allows(h.CommuneCode) {
		return domain.Household{}, ErrHouseholdNotFound
	}
	if isCoopManagerOnly(p) && h.CooperativeRef != p.OrgRef {
		return domain.Household{}, errManagerOtherCoop
	}
	return *h, nil
}

// HouseholdPhone trả SĐT chủ hộ lúc Quản lý HTX bấm gọi nhanh (HTX US-HH-03): đọc từ
// kho định danh với mục đích extension.contact (kho ghi sổ truy cập, R-42), không lưu,
// không ghi log (R-14); handler đặt Cache-Control: no-store. Hộ ngoài HTX của quản lý
// → 403 "Bạn không có quyền truy cập nông hộ này."; chưa có SĐT hợp lệ → 404.
func (s *Service) HouseholdPhone(ctx context.Context, ref uuid.UUID) (string, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return "", err
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return "", err
	}
	if _, err := s.visibleHousehold(ctx, p, sc, ref); err != nil {
		return "", err
	}
	if s.Vault == nil {
		return "", ErrDependency.WithDetail("chưa nối kho định danh")
	}
	phone, err := s.Vault.Phone(ctx, ref)
	switch {
	case errors.Is(err, port.ErrIdentityNotFound):
		return "", ErrNoContact
	case err != nil:
		return "", vaultError(err)
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", ErrNoContact
	}
	return phone, nil
}

// householdByPhone tìm nông hộ theo SĐT qua chỉ mục mù của kho định danh (HTX US-HH-01
// AC-2, mục đích registry.admin — kho ghi sổ tra cứu). Không thấy → Nil, nil.
func (s *Service) householdByPhone(ctx context.Context, phone string) (uuid.UUID, error) {
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	ref, err := s.Vault.LookupPhone(ctx, phone)
	switch {
	case errors.Is(err, port.ErrIdentityNotFound):
		return uuid.Nil, nil
	case err != nil:
		var pr *problem.Problem
		if errors.As(err, &pr) && pr.Status == http.StatusUnprocessableEntity {
			return uuid.Nil, problem.Validation.WithField("phone", "phải là SĐT Việt Nam")
		}
		return uuid.Nil, vaultError(err)
	}
	return ref, nil
}
