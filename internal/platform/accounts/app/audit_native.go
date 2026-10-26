package app

import (
	"context"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NativeAuditSource exposes account_event metadata only. Reading it remains
// internal; the Audit service authorizes and records disclosure for HTTP users.
func (s *Service) NativeAuditSource() port.AuditSource {
	return port.AuditSource{Key: store.NativeAuditSourceKey, Module: "accounts", Fields: map[string]port.AuditValueKind{"enabled": port.AuditBoolean, "roles": port.AuditCodes, "org_ref": port.AuditRef, "lock_cause": port.AuditCodes}, Read: s.readNativeAudit}
}
func (s *Service) readNativeAudit(ctx context.Context, tid uuid.UUID, q port.AuditReadQuery) ([]port.AuditRecord, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.TenantID != tid || tid == uuid.Nil || p.ActorRef == uuid.Nil {
		return nil, problem.Forbidden
	}
	if contextual, err := tenant.FromContext(ctx); err == nil && contextual != tid {
		return nil, problem.Forbidden
	} else if err != nil && !errors.Is(err, tenant.ErrMissing) {
		return nil, err
	}
	if s.DB == nil {
		return nil, ErrDependency
	}
	if q.Limit < 1 || q.Limit > 201 || q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > MaxAuditWindow || q.AsOf.IsZero() {
		return nil, auditBad("from")
	}
	var out []port.AuditRecord
	err := tenant.WithTenantTxOptions(ctx, db.ForModule(s.DB, db.Module("pf/accounts")), tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = store.AuditAccountEvents(ctx, tx, q)
		return err
	})
	return out, err
}
