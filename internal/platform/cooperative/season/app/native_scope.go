package app

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Native staff have an OrgRef but no selected application session. Their
// server-resolved commune scope must reach RLS as well as the app's filters.
// Managers/farmers keep their own node scope; KN/selected projections remain
// authoritative. A target tenant different from either authenticated tenant
// is never eligible for this additional native scope.
func nativeScopeStaff(p authz.Principal) bool {
	if p.TenantID == uuid.Nil || p.ActorRef == uuid.Nil || p.OrgRef == uuid.Nil || p.KN != nil || p.SelectedSession != nil ||
		p.Has(authz.RolePlatformAdmin) || p.Has(port.RoleCoopManager) || p.Has(port.RoleFarmer) ||
		p.Has(partyport.RolePartnerSync) || p.Has("api_client") || p.Has("kn_user") {
		return false
	}
	return slices.ContainsFunc(knRoles, p.Has)
}
func nativeScopeCaller(ctx context.Context, tid uuid.UUID) bool {
	p, ok := authz.FromContext(ctx)
	if !ok || p.TenantID != tid || !nativeScopeStaff(p) {
		return false
	}
	if current, err := tenant.FromContext(ctx); err == nil && current != tid {
		return false
	}
	return true
}

func (s *Service) nativeScopePreflight(ctx context.Context) error {
	p, ok := authz.FromContext(ctx)
	if !ok || !nativeScopeStaff(p) {
		return nil
	}
	if s.dependencyTxOpen(ctx) {
		return ErrDependency.WithDetail("phạm vi cán bộ phải được giải trước giao dịch mùa vụ")
	}
	return nil
}

// New port lookups cannot run in a caller's joined/reusable transaction.
// Opaque Beginner wrappers which hide an existing tx are unsupported.
func (s *Service) dependencyTxOpen(ctx context.Context) bool {
	_, joined := shareddb.JoinedTx(ctx)
	_, direct := s.DB.(pgx.Tx)
	return joined || direct
}

// ScopeResolver may open a different module's pool. Resolve it BEFORE BeginTx,
// then set only LOCAL xa_ids inside our transaction. Never clear node_id for
// All or derive a scope from client fields, realm roles alone, or org ancestors.
func (s *Service) nativeScopeTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	var xaIDs string
	bind := false
	if nativeScopeCaller(ctx, tid) {
		if err := s.nativeScopePreflight(ctx); err != nil {
			return err
		}
		if s.Scope == nil {
			return ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
		}
		sc, err := s.Scope.Scope(ctx)
		if err != nil {
			return err
		}
		if !sc.All {
			xaIDs = (shareddb.ContextVars{XaIDs: sc.Units}).Settings()[shareddb.VarXaIDs]
			bind = true
		}
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		if bind {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.xa_ids', $1, true)", xaIDs); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}
