package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
)

// lookupHousehold keeps the native manager's commune lookup consistent with the
// server-resolved scope. It can find an unassigned household for joining, or let
// the caller's ownership check reject a different cooperative in the same
// commune. It does not authorize that cooperative's data or change write scope.
// Selected sessions and KN contexts retain their existing database projection.
func lookupHousehold(ctx context.Context, tx pgx.Tx, p authz.Principal, sc authz.Scope, ref uuid.UUID) (*domain.Household, error) {
	if p.KN != nil || p.SelectedSession != nil || !isCoopManagerOnly(p) {
		return store.HouseholdByRef(ctx, tx, ref)
	}
	if sc.All {
		return nil, ErrOutOfScope
	}
	if p.OrgRef == uuid.Nil || len(sc.Units) == 0 {
		return nil, ErrHouseholdNotFound
	}
	var previous string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.xa_ids', true), '')").Scan(&previous); err != nil {
		return nil, err
	}
	vars := shareddb.ContextVars{XaIDs: sc.Units}
	const set = "SELECT set_config('app.xa_ids', $1, true)"
	if _, err := tx.Exec(ctx, set, vars.Settings()[shareddb.VarXaIDs]); err != nil {
		return nil, err
	}
	h, err := store.HouseholdByRef(ctx, tx, ref)
	if err != nil {
		return nil, err // the enclosing transaction rolls back, including SET LOCAL
	}
	if _, err := tx.Exec(ctx, set, previous); err != nil {
		return nil, err
	}
	return h, nil
}
