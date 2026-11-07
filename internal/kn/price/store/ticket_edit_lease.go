package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// EditLease is transient ownership, read only after the caller's ticket scope is checked.
type EditLease struct {
	Holder  uuid.UUID
	Expires time.Time
}

// All lease operations run under LockPointTickets, shared with every ticket edit.
func LoadEditLease(ctx context.Context, tx pgx.Tx, ticket uuid.UUID, now time.Time) (*EditLease, error) {
	var l EditLease
	err := tx.QueryRow(ctx, `SELECT l.holder_ref, max(h.expires_at)
 FROM pf_price.ticket_edit_lease l JOIN pf_price.ticket_edit_handle h USING (tenant_id,ticket_id)
 WHERE l.ticket_id=$1 AND h.expires_at>$2 GROUP BY l.holder_ref`, ticket, now.UTC()).Scan(&l.Holder, &l.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}
func ClaimEditLease(ctx context.Context, tx pgx.Tx, tenant, ticket, actor, handle uuid.UUID, now, until time.Time) error {
	if err := ClearExpiredEditHandles(ctx, tx, ticket, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pf_price.ticket_edit_lease(tenant_id,ticket_id,holder_ref) VALUES($1,$2,$3)
 ON CONFLICT(tenant_id,ticket_id) DO UPDATE SET holder_ref=EXCLUDED.holder_ref`, tenant, ticket, actor); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.ticket_edit_handle(tenant_id,ticket_id,handle_id,expires_at) VALUES($1,$2,$3,$4)
 ON CONFLICT(tenant_id,ticket_id,handle_id) DO UPDATE SET expires_at=EXCLUDED.expires_at`, tenant, ticket, handle, until.UTC())
	return err
}
func HasEditHandle(ctx context.Context, tx pgx.Tx, ticket, actor, handle uuid.UUID, now time.Time) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_price.ticket_edit_handle h JOIN pf_price.ticket_edit_lease l USING(tenant_id,ticket_id)
 WHERE h.ticket_id=$1 AND l.holder_ref=$2 AND h.handle_id=$3 AND h.expires_at>$4)`, ticket, actor, handle, now.UTC()).Scan(&ok)
	return ok, err
}
func RenewEditHandle(ctx context.Context, tx pgx.Tx, ticket, handle uuid.UUID, until time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_price.ticket_edit_handle SET expires_at=$3 WHERE ticket_id=$1 AND handle_id=$2`, ticket, handle, until.UTC())
	return err
}

// ClearEditHandle removes only this tab's transient handle; all ticket business history remains append-only.
func ClearEditHandle(ctx context.Context, tx pgx.Tx, ticket, actor, handle uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM pf_price.ticket_edit_handle h USING pf_price.ticket_edit_lease l
 WHERE h.tenant_id=l.tenant_id AND h.ticket_id=l.ticket_id AND h.ticket_id=$1 AND l.holder_ref=$2 AND h.handle_id=$3`, ticket, actor, handle)
	return err
}

// ClearExpiredEditHandles removes expired transient coordination handles, never business ledger rows.
func ClearExpiredEditHandles(ctx context.Context, tx pgx.Tx, ticket uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `DELETE FROM pf_price.ticket_edit_handle WHERE ticket_id=$1 AND expires_at<=$2`, ticket, now.UTC())
	return err
}
