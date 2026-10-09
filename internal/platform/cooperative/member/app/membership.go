package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// errNoHousehold là lỗi nội bộ "không có nông hộ" trước khi đổi sang lỗi của port bên gọi.
var errNoHousehold = errors.New("cooperative: không có nông hộ")

func (s *Service) membership(ctx context.Context, ref uuid.UUID) (domain.Household, error) {
	var h *domain.Household
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		h, e = store.HouseholdByRef(ctx, tx, ref)
		return e
	})
	if err != nil {
		return domain.Household{}, err
	}
	if h == nil {
		return domain.Household{}, errNoHousehold
	}
	return *h, nil
}

// HouseholdMembership trả HTX hiện tại của nông hộ (cooperativeport.Households.Membership — mặt tiền
// cooperative/app nối). Tenant lấy từ context. Không có → port.ErrHouseholdNotFound.
func (s *Service) HouseholdMembership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	h, err := s.membership(ctx, ref)
	if errors.Is(err, errNoHousehold) {
		return port.HouseholdMembership{}, port.ErrHouseholdNotFound
	}
	if err != nil {
		return port.HouseholdMembership{}, err
	}
	return port.HouseholdMembership{Ref: h.Ref, CooperativeRef: h.CooperativeRef, CommuneCode: h.CommuneCode, Active: h.Active,
		OwnerTenant: h.TenantID}, nil
}

// PartyHouseholds là bộ điều hợp partyport.Households cho bên tiêu thụ tầng lõi (pf/accounts — không import
// cooperativeport); gốc lắp ráp nối (W3.3).
type PartyHouseholds struct{ Svc *Service }

var _ partyport.Households = PartyHouseholds{}

// Membership thực hiện partyport.Households. Không có → partyport.ErrHouseholdNotFound.
func (a PartyHouseholds) Membership(ctx context.Context, ref uuid.UUID) (partyport.HouseholdMembership, error) {
	h, err := a.Svc.membership(ctx, ref)
	if errors.Is(err, errNoHousehold) {
		return partyport.HouseholdMembership{}, partyport.ErrHouseholdNotFound
	}
	if err != nil {
		return partyport.HouseholdMembership{}, err
	}
	return partyport.HouseholdMembership{Ref: h.Ref, CooperativeRef: h.CooperativeRef, CommuneCode: h.CommuneCode, Active: h.Active}, nil
}
