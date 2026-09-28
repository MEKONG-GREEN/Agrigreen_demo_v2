package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ port.Households = (*Registry)(nil)

// Membership thực hiện port.Households: HTX hiện tại của nông hộ. Tenant lấy từ context.
func (r *Registry) Membership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	var out *port.HouseholdMembership
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		h, e := store.HouseholdByRef(ctx, tx, ref)
		if e != nil || h == nil {
			return e
		}
		out = &port.HouseholdMembership{Ref: h.Ref, CooperativeRef: h.CooperativeRef, CommuneCode: h.CommuneCode, Active: h.Active}
		return nil
	})
	if err != nil {
		return port.HouseholdMembership{}, err
	}
	if out == nil {
		return port.HouseholdMembership{}, port.ErrHouseholdNotFound
	}
	return *out, nil
}
